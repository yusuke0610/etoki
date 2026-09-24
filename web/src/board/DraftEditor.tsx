import { useState } from "react";

import type {
  Granularity,
  Interpretation,
  InterpretedItem,
  ItemKind,
  ProjectAccess,
  SyncItem,
} from "../api/types";
import { CreationSection } from "./CreationSection";
import { groupByEpic } from "./interpretation";
import {
  blockingReasons,
  buildInterpretation,
  createDraft,
  leftBehindItemIds,
  markCreated,
  orphanedLocalIds,
  setBody,
  setKind,
  setTitle,
  setUpdatesPrevious,
  toggleItem,
} from "./interpretationDraft";
import type { CreationState } from "./panelShared";
import type { ProjectLink } from "./projectLink";

/**
 * 解釈結果を見せ、作るものを選ばせ、手直しさせる。
 *
 * summary は GitHub には作らない。LLM がこの囲みをどう読んだかを開発者が
 * 確かめるための材料（ADR 0006）なので、編集もさせない。
 *
 * 作成は取り消せない（ADR 0009）。押す前に中身が読めているだけでなく、
 * **LLM が決めたとおりに作るしかない状態にしない**（中核思想 3、ADR 0024）。
 *
 * **下書きをここで持つ。`BoardPage` に上げない。** 保存も解釈のやり直しも
 * `InterpretationState` を done から外すので、この枝ごと unmount されて編集は
 * 捨てられる。上げると `save` と `interpret` の両方に破棄を書き足すことになり、
 * 片方を忘れると保存したあとに古い編集が残る。
 */
export function DraftEditor({
  annotationId,
  granularity,
  result,
  created,
  creation,
  saving,
  creationBlocked,
  projectAccess,
  creationUnavailable,
  previous,
  projectLink,
  onCreate,
}: {
  annotationId: string;
  granularity: Granularity;
  result: Interpretation;
  /** この解釈から作ったもの。作成の 1 回ごとに 1 要素（ADR 0052）。 */
  created: SyncItem[][];
  creation?: CreationState;
  /** 保存中は入力も止める。保存が解釈ごと捨てるため。 */
  saving: boolean;
  /** いま作成を始められない理由。押せるなら null（表は `exclusion.ts`）。 */
  creationBlocked: string | null;
  projectAccess: ProjectAccess;
  /** GitHub が未設定なら理由。使えるなら null（ADR 0030）。 */
  creationUnavailable: string | null;
  /** この注釈が GitHub に在らしめているもの。取り残しの算出に使う。 */
  previous: SyncItem[];
  projectLink: ProjectLink | null;
  onCreate: (interpretation: Interpretation) => void;
}) {
  // 作ったものは作り直した下書きにも反映する。解釈を選び直して戻ってきた
  // ときに、作成済みの項目が全部選ばれた状態に戻ると押し直しで重複する
  // （ADR 0052）。
  const [draft, setDraft] = useState(() =>
    created.reduce(markCreated, createDraft(result)),
  );
  // 下書きに反映した作成の回数。**増えたぶんだけを反映する。** 前に作った
  // 項目を選び直していたのに、今回の作成に載らなかったものまで外すと、選んだ
  // 操作が黙って消える（`markCreated`）。
  const [appliedCreations, setAppliedCreations] = useState(created.length);
  if (created.length > appliedCreations) {
    // 描画中に揃える。effect にすると、作成が済んだのに選択が残った 1 フレームで
    // ボタンが押せてしまう。
    setAppliedCreations(created.length);
    setDraft((d) => created.slice(appliedCreations).reduce(markCreated, d));
  }

  // 編集後の kind で組み直す。構造を変えたことがその場で見えるようにする。
  const groups = groupByEpic(draft.items.map((d) => d.item));
  // groupByEpic は下書きの項目そのものを並べ替えて返すので、引けない localId は
  // 無い。それでも既定を持つのは、無いものを「選ばれている」と倒さないため。
  const selected = new Map(draft.items.map((d) => [d.item.localId, d.selected]));
  // LLM の答えに従うかどうか。既定は従う（ADR 0026）。
  const updatesPrevious = new Map(
    draft.items.map((d) => [d.item.localId, d.updatesPrevious]),
  );
  // この解釈から作った項目。
  const createdItems = new Set(
    draft.items.filter((d) => d.createdItemId).map((d) => d.item.localId),
  );
  const orphans = orphanedLocalIds(draft);
  const reasons = blockingReasons(draft, granularity);
  // 今回の作成で GitHub 側に置き去りになるもの（ADR 0026）。
  const leftBehind = leftBehindItemIds(draft, previous);

  // 作成中と保存中は入力も止める。ボタンだけ止めても、押せないあいだに
  // 編集できるのでは何を作っているのかが定まらない。
  const frozen = creation?.status === "running" || saving;

  // 粒度に issue を指定した注釈では epic を 1 件も作れない（サーバーの
  // Validate が弾く）。選ばせる理由が無いので種別は変えさせない。
  const editableKind = granularity !== "issue";

  const fields = (item: InterpretedItem) => (
    <DraftItemFields
      item={item}
      selected={selected.get(item.localId) ?? false}
      createdItem={createdItems.has(item.localId)}
      updatesPrevious={updatesPrevious.get(item.localId) ?? false}
      orphan={orphans.has(item.localId)}
      frozen={frozen}
      editableKind={editableKind}
      onToggle={() => setDraft((d) => toggleItem(d, item.localId))}
      onKind={(kind) => setDraft((d) => setKind(d, item.localId, kind))}
      onTitle={(title) => setDraft((d) => setTitle(d, item.localId, title))}
      onBody={(body) => setDraft((d) => setBody(d, item.localId, body))}
      onUpdatesPrevious={(updates) =>
        setDraft((d) => setUpdatesPrevious(d, item.localId, updates))
      }
    />
  );

  return (
    <>
      <div className="interpretation-result">
        <p className="summary">{draft.summary}</p>

        {groups.length === 0 ? (
          <p className="hint">作成される項目はありません。</p>
        ) : (
          <ul className="plain-list">
            {groups.map((g, i) => (
              <li key={g.epic?.localId ?? `orphans-${i}`}>
                {g.epic ? (
                  fields(g.epic)
                ) : (
                  <span className="hint">epic に属さない issue</span>
                )}

                {g.issues.length > 0 && (
                  <ul className="plain-list">
                    {g.issues.map((it) => (
                      <li key={it.localId}>{fields(it)}</li>
                    ))}
                  </ul>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>

      <LeftBehind items={previous.filter((it) => leftBehind.has(it.itemId))} />

      <CreationSection
        annotationId={annotationId}
        state={creation}
        creationBlocked={creationBlocked}
        reasons={reasons}
        projectAccess={projectAccess}
        creationUnavailable={creationUnavailable}
        projectLink={projectLink}
        onCreate={() => onCreate(buildInterpretation(draft))}
      />
    </>
  );
}

/**
 * 解釈結果 1 件ぶんの、作るかどうかと中身。
 *
 * ラベルは `localId` で分ける。一覧に同じ役割の入力が何組も並ぶので、
 * タイトルで分けると編集の途中でラベルが変わってしまう。
 */
function DraftItemFields({
  item,
  selected,
  createdItem,
  updatesPrevious,
  orphan,
  frozen,
  editableKind,
  onToggle,
  onKind,
  onTitle,
  onBody,
  onUpdatesPrevious,
}: {
  item: InterpretedItem;
  selected: boolean;
  /**
   * この解釈から作った項目かどうか（ADR 0052）。
   *
   * 作った項目は新規には戻せない。選び直すと、作った draft issue の書き換えに
   * なる。
   */
  createdItem: boolean;
  /**
   * LLM が対応づけた更新先に、実際に書き込むかどうか。
   *
   * `item.previousItemId` を持たない項目では常に false。切り替えも出さない。
   * 指す先が無いので、選ばせるものが無い。
   */
  updatesPrevious: boolean;
  /**
   * 親を失ったまま作られる issue かどうか。
   *
   * 選ばれていない項目は最初から含まれない（`orphanedLocalIds`）。ここで
   * `selected` と重ねて判定しない。同じことを 2 箇所で決めることになる。
   */
  orphan: boolean;
  frozen: boolean;
  editableKind: boolean;
  onToggle: () => void;
  onKind: (kind: ItemKind) => void;
  onTitle: (title: string) => void;
  onBody: (body: string) => void;
  onUpdatesPrevious: (updatesPrevious: boolean) => void;
}) {
  return (
    <div className={`draft-item${selected ? "" : " unselected"}`}>
      <div className="draft-head">
        <input
          type="checkbox"
          checked={selected}
          disabled={frozen}
          onChange={onToggle}
          aria-label={`${item.localId} を作成する`}
        />

        {editableKind ? (
          <select
            className="draft-kind"
            value={item.kind}
            disabled={frozen}
            onChange={(e) => onKind(e.target.value as ItemKind)}
            aria-label={`${item.localId} の種別`}
          >
            <option value="epic">epic</option>
            <option value="issue">issue</option>
          </select>
        ) : (
          <span className="kind">{item.kind}</span>
        )}

        <input
          className="draft-title"
          value={item.title}
          disabled={frozen}
          onChange={(e) => onTitle(e.target.value)}
          aria-label={`${item.localId} のタイトル`}
        />

        {/*
          作るのか書き換えるのかは、押す前に見えている必要がある（ADR 0026）。
          どちらも取り消せないが、取り返しのつかなさが違う。書き換えは前の内容を
          消す。

          **印だけでなく、覆せる形で出す。** 対応づけを解釈させるのは LLM でも、
          決めるのは開発者（ADR 0026）。指す先が GitHub から消えていると、
          更新のままでは作成が必ず失敗する。
        */}
        {createdItem ? (
          <span className="badge badge-created">作成した</span>
        ) : (
          item.previousItemId &&
          updatesPrevious && <span className="badge badge-updated">更新</span>
        )}
      </div>

      {/*
        作った項目は、選び直すと書き換えになることを先に言う。チェックだけ
        外れていると、作り損ねたのか作ったのかが読めない。
      */}
      {createdItem && (
        <p className="hint">
          {selected
            ? "作成した draft issue を書き換えます。"
            : "作成しました。選び直すと、作成した draft issue を書き換えます。"}
        </p>
      )}

      {/*
        **切り替えは見出しの行に置かない。** パネルは狭く、種別とタイトルが
        すでに並んでいる。同じ行に足すとタイトルが読めなくなり、押す前に中身が
        見えているという前提（ADR 0024）が崩れる。

        LLM が言ったこととの差も添える。既定のままなら出さない。「残ります」は
        ここでは言わない。取り残しは作成ボタンの手前にまとめて出しており
        （ADR 0026）、同じことを 2 箇所で数えることになる。
      */}
      {/*
        作った項目には出さない。作った ID の書き換えにしか送れないので
        （`markCreated`）、選ばせるものが無い。
      */}
      {item.previousItemId && !createdItem && (
        <div className="draft-previous">
          <select
            value={updatesPrevious ? "update" : "create"}
            disabled={frozen}
            onChange={(e) => onUpdatesPrevious(e.target.value === "update")}
            aria-label={`${item.localId} を更新するか新しく作るか`}
          >
            <option value="update">更新する</option>
            <option value="create">新しく作る</option>
          </select>
          {!updatesPrevious && (
            <span className="hint">解釈では既存の draft issue の更新でした。</span>
          )}
        </div>
      )}

      {/*
        親が消えたことを黙って起こさない（ADR 0024）。作られるものが変わって
        いるので、押す前に見えている必要がある。
      */}
      {orphan && <p className="hint">epic に属さない issue として作られます。</p>}

      <DraftItemBody
        localId={item.localId}
        body={item.body}
        frozen={frozen}
        onBody={onBody}
      />
    </div>
  );
}

/**
 * これから作る draft issue の本文。既定は畳んでおく。
 *
 * `ItemBody` と見え方を揃える。畳んであること、生テキストのまま出すこと、
 * 空なら空と分かること。**整形しない。** GitHub に送るのはこのテキスト
 * そのもので、整形すると「確認したもの」と「作られるもの」がずれる。
 */
function DraftItemBody({
  localId,
  body,
  frozen,
  onBody,
}: {
  localId: string;
  body: string;
  frozen: boolean;
  onBody: (body: string) => void;
}) {
  return (
    <details className="item-body">
      {/* 空のときの文言は `ItemBody` と揃える。同じものを見ているのに、
          読むときと直すときで呼び方が変わると別物に見える（ADR 0023）。 */}
      <summary>{body === "" ? "本文なし" : "本文"}</summary>
      <textarea
        value={body}
        rows={6}
        disabled={frozen}
        onChange={(e) => onBody(e.target.value)}
        aria-label={`${localId} の本文`}
      />
    </details>
  );
}

/**
 * 今回の作成で GitHub 側に置き去りになるもの（ADR 0026）。
 *
 * **消す判断はしない。** draft issue は削除できないので、etoki にできるのは
 * 「残ります」と見せるところまで。黙って落とすと、開発者は自分が何を置き去りに
 * したのかを確かめられない（中核思想 3）。
 *
 * 0 件なら何も出さない。常に枠を出すと、取り残しが無いことと 0 件であることの
 * 区別に注意を割かせる。
 */
function LeftBehind({ items }: { items: SyncItem[] }) {
  if (items.length === 0) return null;

  return (
    <div className="left-behind">
      <p className="hint">
        {`前回作った ${items.length} 件は、今回の作成では書き換わりません。`}
        {"GitHub 側にそのまま残ります。"}
      </p>
      <ul className="plain-list">
        {items.map((it) => (
          <li key={it.itemId}>
            <span className="kind">{it.kind}</span> {it.title}
          </li>
        ))}
      </ul>
    </div>
  );
}
