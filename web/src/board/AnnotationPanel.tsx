import { useState } from "react";

import type {
  AnnotationStatus,
  DetachedAnnotation,
  DiagramKind,
  Granularity,
  Interpretation,
  ProjectAccess,
  SyncState,
} from "../api/types";
import type { SelectableFrame } from "../excalidraw/annotation";
import { GRANULARITY_LABEL, annotationLabels, frameLabel } from "./annotationLabel";
import { DetachedSection } from "./DetachedSection";
import { DIAGRAM_KIND_LABELS, diagramKinds } from "./diagramLabels";
import { InterpretationSection } from "./InterpretationSection";
import type { InterpretationState } from "./interpretationHistory";
import { ItemBody, ProjectLinkLine } from "./panelParts";
import {
  INTERPRETATION_UNAVAILABLE_ID,
  type CreationState,
  type RunHistoryState,
} from "./panelShared";
import type { ProjectLink } from "./projectLink";
import { RunHistory } from "./RunHistory";

const STATE_LABEL: Record<SyncState, string> = {
  uncreated: "未作成",
  created: "作成済み",
  changed: "変更あり",
};

type Props = {
  annotations: AnnotationStatus[];
  /**
   * シーンから消えたのに GitHub 側にものが残っている注釈（#111）。
   *
   * **`annotations` と混ぜて渡さない。** 3 状態も名前も無いので、注釈の
   * カードと同じ形では出せない。
   */
  detached: DetachedAnnotation[];
  /** 選択中の frame のうち、まだ注釈になっていないもの。 */
  markableFrames: SelectableFrame[];
  /** 選択中の frame のうち、すでに注釈になっているもの。 */
  unmarkableFrames: SelectableFrame[];
  /**
   * キャンバスにいま在る frame の ID。まだ分からなければ null。
   *
   * 状態は保存済みシーンが基準なので、未保存で消したフレームの注釈が一覧に
   * 残る。そのカードは押しても飛び先が無い（ADR 0022）。**分からないうちは
   * 無いことにしない。** 空配列と同じ扱いにすると、マウント直後の一瞬だけ
   * 全部のカードが「キャンバスにありません」になる。
   */
  canvasFrameIds: string[] | null;
  /**
   * キャンバスで選択中の frame の ID。対応するカードを強調するために使う。
   */
  selectedFrameIds: string[];
  /** カードを押したとき、キャンバスをそのフレームへ寄せて選択する。 */
  onFocusFrame: (frameId: string) => void;
  onMark: (frameId: string, granularity: Granularity) => void;
  onUnmark: (frameId: string) => void;
  onChangeGranularity: (frameId: string, granularity: Granularity) => void;
  /** 図の種別を差し替える。`undefined` は「指定なし」に戻す。 */
  onChangeKind: (frameId: string, kind: DiagramKind | undefined) => void;
  /** 未保存の変更があるとき、状態表示は古い可能性がある。 */
  stale: boolean;
  /** 注釈 ID をキーにした解釈の状態。未実行の注釈は入っていない。 */
  interpretations: Record<string, InterpretationState>;
  onInterpret: (annotationId: string) => void;
  /**
   * 見る解釈を選び直す。
   *
   * 解釈は引き直すたびに揺れるので、前のほうが良いことがある。選び直せないと、
   * 引き直しは「戻せない操作」になる。
   */
  onSelectInterpretation: (annotationId: string, runId: number) => void;
  /**
   * 注釈 ID をキーにした実行履歴。**まだ押していない注釈は入っていない。**
   *
   * 開いただけで全注釈ぶん引かない（中核思想 3）。
   */
  runHistories: Record<string, RunHistoryState>;
  onLoadRuns: (annotationId: string) => void;
  /** 注釈 ID をキーにした作成の状態。未実行の注釈は入っていない。 */
  creations: Record<string, CreationState>;
  /**
   * 保存中は下書きの編集も止める。保存が解釈ごと捨てるため。
   *
   * **`creationBlocked` とは別物。** あちらは「押せない理由」で、こちらは
   * 「入力を凍らせるかどうか」。作成を止める条件は保存だけではないので、
   * 一方をもう一方から導かない。
   */
  saving: boolean;
  /**
   * いま作成を始められない理由。押せるなら null。
   *
   * **文言はここで組まない。** 何が走っているとどう言うかは
   * `web/src/board/exclusion.ts` の表が持ち、`BoardPage` が引いて渡す
   * （#146）。パネルが `saving` / `importing` から組み直していたころは、
   * 同じ判定がヘッダーとここの 2 箇所にあった。
   */
  creationBlocked: string | null;
  /**
   * `interpretationId` は下書きの元になった解釈。作ったものをその解釈に
   * 結びつけて持つために渡す（ADR 0052）。
   */
  onCreate: (
    annotationId: string,
    interpretationId: number,
    interpretation: Interpretation,
  ) => void;
  /**
   * 編集できるか。viewer は false（ADR 0017）。
   *
   * 解釈も含めて出さない。解釈は LLM を叩く外部呼び出しであり、閲覧者に
   * 許すのは「閲覧」ではない。
   */
  canEdit: boolean;
  /**
   * 作成先の Project に書けるかどうかの、いまの状態。
   *
   * `denied` でもボタンを黙って消さず、理由を出す。ブレストには参加できて
   * 作成だけができない、というのがこの機能で普通に起きる状態なので、
   * 「なぜできないか」が見えていないと使えない（中核思想 3）。
   */
  projectAccess: ProjectAccess;
  /**
   * LLM が未設定なら理由。使えるなら null（ADR 0030）。
   *
   * `projectAccess` とは別物。あちらはこのボードの Project に書けるか、
   * こちらは etoki に LLM が設定されているか。**混ぜない。**
   */
  interpretationUnavailable: string | null;
  /** GitHub が未設定なら理由。使えるなら null（ADR 0030）。 */
  creationUnavailable: string | null;
  /**
   * 作成先へのリンク。組めなければ null（ADR 0025）。
   *
   * draft issue 個別の URL は組めないので、飛び先は注釈ごとではなく
   * ボードごとに 1 つ。**行ごとにリンクを置かない。** 置くと、行ごとに
   * 違う場所へ飛ぶように読めてしまう。
   */
  projectLink: ProjectLink | null;
};

type PendingKind = {
  /** 保存を待つあいだに表示する、最後に選んだ値。 */
  value: DiagramKind | undefined;
};

export function AnnotationPanel({
  annotations,
  detached,
  markableFrames,
  unmarkableFrames,
  canvasFrameIds,
  selectedFrameIds,
  onFocusFrame,
  onMark,
  onUnmark,
  onChangeGranularity,
  onChangeKind,
  stale,
  interpretations,
  onInterpret,
  onSelectInterpretation,
  runHistories,
  onLoadRuns,
  creations,
  saving,
  creationBlocked,
  onCreate,
  canEdit,
  projectAccess,
  interpretationUnavailable,
  creationUnavailable,
  projectLink,
}: Props) {
  // 注釈の状態は保存済みシーンから来る。種別を変えた直後はキャンバスだけが
  // 新しく、次の保存まで a.kind は古いので、そのあいだはここで選択値を持つ。
  // **最後に選んだ値が a.kind に追いつくまで保持する。** 保存中に選び直すと
  // 前の選択の保存が後から追いつくことがあり、そこで「保存済みの値が変わった
  // から追いついた」と判定すると、追いついたのが古い選択のほうでも pending を
  // 消してしまい、選択欄がキャンバスと違う値に戻る。
  //
  // **追いついた・注釈が消えた pending は掃除しない。** 掃除は「レンダー中に
  // 前回の props と比べて setState する」か「effect で setState する」のどちらかに
  // なるが、前者は ref を読み書きする形になり、後者は effect 内の直接の setState
  // になるので、どちらもこのリポジトリの eslint-plugin-react-hooks が禁じる形に
  // なる。**このパネルはボードを切り替えると `key={board.id}` ごと作り直される**
  // （`App`）ので、残る量は開いているボードで選び直した種別の数に留まり、
  // 実害の無い範囲。
  const [pendingKinds, setPendingKinds] = useState<Record<string, PendingKind>>({});

  // 見出しは 2 つの欄で共有する。同じ注釈が片方は名前、もう片方は番号で
  // 出ると、同じものが 2 つあるように見える。
  const labels = annotationLabels(annotations);

  return (
    <aside className="panel">
      <h2>注釈</h2>

      {!canEdit && (
        <p className="hint" role="status">
          {"読むだけの権限で開いています。編集・解釈・作成はできません。"}
        </p>
      )}

      {/*
        設定の不足は注釈ごとではなくパネルに 1 度だけ出す。注釈の数だけ同じ文が
        並ぶと、読むべき状態が埋もれる。各カードのボタンはこの文を
        aria-describedby で指す（ADR 0030）。

        viewer には出さない。どのみち解釈できないことは上の 1 行が言っており、
        設定の話を重ねても打てる手は増えない（ADR 0017）。
      */}
      {canEdit && interpretationUnavailable !== null && (
        <p className="hint" role="status" id={INTERPRETATION_UNAVAILABLE_ID}>
          {interpretationUnavailable}
        </p>
      )}

      <section className="panel-section">
        <h3>選択中のフレーム</h3>
        {!canEdit ? (
          <p className="hint">注釈を付け外しできるのは編集できる人だけです。</p>
        ) : markableFrames.length === 0 && unmarkableFrames.length === 0 ? (
          <p className="hint">
            フレームツール（F）で囲んでから、そのフレームを選択してください。
          </p>
        ) : (
          <ul className="plain-list">
            {/*
              どのフレームに対する操作なのかを項目ごとに出す。複数を選んだとき、
              ボタンの文言だけでは項目が区別できない（ADR 0022）。
            */}
            {markableFrames.map((frame) => (
              <li key={frame.id}>
                <button type="button" onClick={() => onMark(frame.id, "")}>
                  {frameLabel(frame.name)}
                  <span className="kind">を注釈にする</span>
                </button>
              </li>
            ))}
            {unmarkableFrames.map((frame) => (
              <li key={frame.id}>
                <button type="button" onClick={() => onUnmark(frame.id)}>
                  {labels.get(frame.id) ?? frameLabel(frame.name)}
                  <span className="kind">の注釈を外す</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="panel-section">
        <h3>
          状態
          {stale && <span className="stale"> （未保存の変更あり）</span>}
        </h3>

        {annotations.length === 0 ? (
          <p className="hint">保存済みの注釈はありません。</p>
        ) : (
          <ul className="annotation-list">
            {annotations.map((a) => {
              const onCanvas = canvasFrameIds === null || canvasFrameIds.includes(a.id);
              const selected = selectedFrameIds.includes(a.id);
              const missingId = `annotation-missing-${a.id}`;
              const pendingKind = pendingKinds[a.id];
              const kind =
                pendingKind && pendingKind.value !== a.kind ? pendingKind.value : a.kind;

              return (
                <li
                  key={a.id}
                  className={`annotation${selected ? " selected" : ""}`}
                  // キャンバスで選択したフレームがどのカードなのかを、色だけに
                  // 頼らず読み上げにも届く形で示す。
                  aria-current={selected ? "true" : undefined}
                >
                  <div className="annotation-head">
                    {/*
                      見出しを押すとキャンバスがそのフレームへ寄る。名前に頼らず
                      対応を確かめられる唯一の手段なので、名前の有無に関わらず
                      押せるようにしてある（ADR 0022）。
                    */}
                    <button
                      type="button"
                      className="annotation-name"
                      onClick={() => onFocusFrame(a.id)}
                      disabled={!onCanvas}
                      aria-describedby={onCanvas ? undefined : missingId}
                    >
                      {labels.get(a.id)}
                    </button>
                    <span className={`badge badge-${a.state}`}>
                      {STATE_LABEL[a.state]}
                    </span>
                  </div>

                  {/*
                    状態は保存済みシーンが基準なので、未保存で消したフレームの
                    注釈がここに残る。押せない理由は title に隠さず本文で出す。
                  */}
                  {!onCanvas && (
                    <p className="hint" id={missingId}>
                      このフレームはキャンバスにありません。保存すると一覧からも消えます。
                    </p>
                  )}

                  <label className="granularity">
                    粒度
                    <select
                      value={a.granularity}
                      disabled={!canEdit}
                      onChange={(e) =>
                        onChangeGranularity(a.id, e.target.value as Granularity)
                      }
                    >
                      {(Object.keys(GRANULARITY_LABEL) as Granularity[]).map((g) => (
                        <option key={g} value={g}>
                          {GRANULARITY_LABEL[g]}
                        </option>
                      ))}
                    </select>
                  </label>

                  {/*
                    何の図として読ませるかを選ばせる。**ひな形は絵を置くだけ**
                    （ADR 0047）で、どこを囲むかも何の図かも人が決めるので、
                    種別が載る先はここしかない。

                    粒度と同じ形（`<select>` + 表を引く）にしてあるのは、
                    同じメタデータに載る 2 つが画面で別物に見えないため。
                  */}
                  <label className="granularity">
                    種別
                    <select
                      value={kind ?? ""}
                      disabled={!canEdit}
                      onChange={(e) => {
                        const nextKind = (e.target.value || undefined) as
                          | DiagramKind
                          | undefined;
                        setPendingKinds((current) => ({
                          ...current,
                          [a.id]: { value: nextKind },
                        }));
                        onChangeKind(a.id, nextKind);
                      }}
                    >
                      {/*
                        「指定なし」は種別の語彙（DiagramKind）に無い値なので、
                        ここだけ空文字で表す。選ばれたら customData からキーごと
                        落ちる（setAnnotationKind）。
                      */}
                      <option value="">指定なし</option>
                      {diagramKinds().map((k) => (
                        <option key={k} value={k}>
                          {DIAGRAM_KIND_LABELS[k]}
                        </option>
                      ))}
                    </select>
                  </label>

                  {a.items && a.items.length > 0 && (
                    <details>
                      <summary>GitHub にある {a.items.length} 件</summary>
                      <ul className="plain-list">
                        {a.items.map((it) => (
                          <li key={it.itemId}>
                            <span className="kind">{it.kind}</span> {it.title}
                            <ItemBody body={it.body} />
                          </li>
                        ))}
                      </ul>
                      <ProjectLinkLine link={projectLink} />
                    </details>
                  )}

                  {/*
                    前回実行が途中で失敗したことは、履歴を開かなくても見える
                    ところに出す（ADR 0043）。**状態（3 状態）は変えない。**
                    作れたぶんは記録するので created のままであり、そこに件数
                    以外の手掛かりが無いのが問題だった。

                    **理由はここには出さない。** 手掛かりの本文は履歴が持つ。
                  */}
                  {a.lastRunOutcome === "incomplete" && (
                    <p className="hint">
                      前回の実行は途中で失敗しました。作れたところまでは GitHub
                      側に残っています。
                    </p>
                  )}

                  {/*
                    履歴は一度でも実行した注釈にだけ出す。**未実行の注釈にも
                    出すと、常に空の枠が並ぶ。** lastSyncedAt があることと
                    run が 1 件以上あることは同じ（最新 run から来る）。
                  */}
                  {a.lastSyncedAt !== undefined && (
                    <details className="run-history">
                      <summary>実行の履歴</summary>
                      <RunHistory
                        state={runHistories[a.id]}
                        onLoad={() => onLoadRuns(a.id)}
                      />
                    </details>
                  )}

                  {canEdit && (
                    <InterpretationSection
                      annotationId={a.id}
                      granularity={a.granularity}
                      state={interpretations[a.id]}
                      creation={creations[a.id]}
                      stale={stale}
                      saving={saving}
                      creationBlocked={creationBlocked}
                      projectAccess={projectAccess}
                      interpretationUnavailable={interpretationUnavailable}
                      creationUnavailable={creationUnavailable}
                      previous={a.items ?? []}
                      projectLink={projectLink}
                      onInterpret={() => onInterpret(a.id)}
                      onSelectInterpretation={(runId) =>
                        onSelectInterpretation(a.id, runId)
                      }
                      onCreate={(interpretationId, interpretation) =>
                        onCreate(a.id, interpretationId, interpretation)
                      }
                    />
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>

      <DetachedSection
        annotations={detached}
        runHistories={runHistories}
        onLoadRuns={onLoadRuns}
        projectLink={projectLink}
      />
    </aside>
  );
}
