import { useState } from "react";

import { partialCreationFailure, type Failure } from "../api/errorMessage";
import type {
  AnnotationStatus,
  CreatedRun,
  Granularity,
  Interpretation,
  InterpretedItem,
  ItemKind,
  ProjectAccess,
  SyncItem,
  SyncRun,
  SyncState,
} from "../api/types";
import { ErrorNotice } from "../ErrorNotice";
import type { SelectableFrame } from "../excalidraw/annotation";
import { GRANULARITY_LABEL, annotationLabels, frameLabel } from "./annotationLabel";
import { groupByEpic } from "./interpretation";
import {
  interpretationOrderLabel,
  selectedInterpretation,
  type InterpretationRun,
  type InterpretationState,
} from "./interpretationHistory";
import {
  blockingReasons,
  buildInterpretation,
  createDraft,
  leftBehindItemIds,
  orphanedLocalIds,
  setBody,
  setKind,
  setTitle,
  toggleItem,
} from "./interpretationDraft";
import type { ProjectLink } from "./projectLink";

/** 注釈 1 つぶんの作成の進み具合。 */
export type CreationState =
  | { status: "running" }
  | { status: "done"; run: CreatedRun }
  | { status: "error"; failure: Failure };

/**
 * 注釈 1 つぶんの実行履歴の読み込み具合。
 *
 * 未実行（キーが無い）と「引いたが 0 件」は別物。前者はまだ押していない、
 * 後者は「一度も作っていない」ことが分かっている状態。
 */
export type RunHistoryState =
  | { status: "loading" }
  | { status: "done"; runs: SyncRun[] }
  | { status: "error"; failure: Failure };

/**
 * 「LLM が未設定」の説明文の id。
 *
 * パネルに 1 つしか出さないので固定でよい。各注釈の「解釈する」がここを指す。
 */
const INTERPRETATION_UNAVAILABLE_ID = "interpretation-unavailable";

const STATE_LABEL: Record<SyncState, string> = {
  uncreated: "未作成",
  created: "作成済み",
  changed: "変更あり",
};

type Props = {
  annotations: AnnotationStatus[];
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
  /** 保存中は作成させない。保存が作成の結果を捨てるため。 */
  saving: boolean;
  onCreate: (annotationId: string, interpretation: Interpretation) => void;
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

export function AnnotationPanel({
  annotations,
  markableFrames,
  unmarkableFrames,
  canvasFrameIds,
  selectedFrameIds,
  onFocusFrame,
  onMark,
  onUnmark,
  onChangeGranularity,
  stale,
  interpretations,
  onInterpret,
  onSelectInterpretation,
  runHistories,
  onLoadRuns,
  creations,
  saving,
  onCreate,
  canEdit,
  projectAccess,
  interpretationUnavailable,
  creationUnavailable,
  projectLink,
}: Props) {
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
                      projectAccess={projectAccess}
                      interpretationUnavailable={interpretationUnavailable}
                      creationUnavailable={creationUnavailable}
                      previous={a.items ?? []}
                      projectLink={projectLink}
                      onInterpret={() => onInterpret(a.id)}
                      onSelectInterpretation={(runId) =>
                        onSelectInterpretation(a.id, runId)
                      }
                      onCreate={(interpretation) => onCreate(a.id, interpretation)}
                    />
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>
    </aside>
  );
}

type InterpretationSectionProps = {
  /** 説明文の id を注釈ごとに分けるために持つ。一覧に複数並ぶため。 */
  annotationId: string;
  /** 注釈の粒度。作成前に手直しできる範囲がこれで変わる。 */
  granularity: Granularity;
  state?: InterpretationState;
  creation?: CreationState;
  /** 未保存の変更があるあいだは解釈させない（ADR 0018）。 */
  stale: boolean;
  saving: boolean;
  projectAccess: ProjectAccess;
  /** LLM が未設定なら理由。使えるなら null（ADR 0030）。 */
  interpretationUnavailable: string | null;
  /** GitHub が未設定なら理由。使えるなら null（ADR 0030）。 */
  creationUnavailable: string | null;
  /** この注釈が GitHub に在らしめているもの（ADR 0026）。 */
  previous: SyncItem[];
  /** 作成したものを確かめにいく先。組めなければ null（ADR 0025）。 */
  projectLink: ProjectLink | null;
  onInterpret: () => void;
  onSelectInterpretation: (runId: number) => void;
  onCreate: (interpretation: Interpretation) => void;
};

/**
 * 解釈の実行と結果表示。
 *
 * 結果を見せるだけで、ここから GitHub には何も作らない。何を作るかは
 * 開発者が別途トリガーする。
 */
function InterpretationSection({
  annotationId,
  granularity,
  state,
  creation,
  stale,
  saving,
  projectAccess,
  interpretationUnavailable,
  creationUnavailable,
  previous,
  projectLink,
  onInterpret,
  onSelectInterpretation,
  onCreate,
}: InterpretationSectionProps) {
  const running = state?.running ?? false;
  const runs = state?.runs ?? [];
  // いま見ている解釈。1 件も返っていなければ undefined。
  const selected = selectedInterpretation(state);
  // 押せない理由は title に隠さず本文として出す。disabled なボタンはフォーカスも
  // 当たらないので、title ではキーボードと読み上げの利用者に理由が届かない。
  const blockedId = `interpret-blocked-${annotationId}`;
  // **設定の不足が先。** 保存しても状況は変わらないので、「保存してから」を
  // 先に出すと、保存した人がもう一度同じところで止まる（ADR 0030）。
  //
  // 未設定の理由はパネルの上に 1 つだけ出ているので、ここでは指すだけにする。
  // 注釈の数だけ同じ文を並べない。
  const unavailable = interpretationUnavailable !== null;
  const describedBy = unavailable
    ? INTERPRETATION_UNAVAILABLE_ID
    : stale
      ? blockedId
      : undefined;

  return (
    <div className="interpretation">
      {/*
        未保存のあいだは押させない。テキストは保存済みシーンから、画像は画面
        から取るので、揃っていないと 1 回の解釈の入力が食い違う（ADR 0018）。
      */}
      <button
        type="button"
        onClick={onInterpret}
        disabled={running || unavailable || stale}
        aria-describedby={describedBy}
      >
        {running ? "解釈中…" : "解釈する"}
      </button>

      {!unavailable && stale && (
        <p className="hint" id={blockedId}>
          保存してから解釈できます。テキストは保存済みのシーンから、
          画像は画面から取るためです。
        </p>
      )}

      {/*
        失敗しても過去の結果は消さない。引き直しに失敗しただけで前の結果まで
        消えると、やり直せば済むはずの失敗が取り返しのつかないものになる。
      */}
      {state?.failure && <ErrorNotice failure={state.failure} />}

      {/*
        2 件以上あるときだけ出す。1 件しか無いのに選択肢を並べると、選ぶ
        余地があるように見えて読むものが増える。
      */}
      {runs.length > 1 && (
        <InterpretationHistory
          annotationId={annotationId}
          runs={runs}
          selectedId={selected?.id ?? null}
          onSelect={onSelectInterpretation}
        />
      )}

      {selected && (
        <InterpretationDraft
          // 選び直したら手直しは引き継がない。別の解釈に対する編集が
          // 混ざると、何を作るのかが読めなくなる（解釈し直したときと同じ）。
          key={selected.id}
          annotationId={annotationId}
          granularity={granularity}
          result={selected.result}
          creation={creation}
          saving={saving}
          projectAccess={projectAccess}
          creationUnavailable={creationUnavailable}
          previous={previous}
          projectLink={projectLink}
          onCreate={onCreate}
        />
      )}
    </div>
  );
}

/**
 * その注釈の実行履歴（ADR 0007）。
 *
 * **押されるまで引かない。** 開いただけで全注釈ぶん引くと、注釈の数だけ
 * 問い合わせが増える（中核思想 3、作成先の名前の取り直しと同じ形）。
 *
 * **畳み込み（「GitHub にある N 件」）とは別物。** あちらは「いま在るもの」、
 * こちらは「いつ何回に分けて作ったか」。同じものを 2 通りに見せているのでは
 * なく、答えている問いが違う（ADR 0026）。
 */
function RunHistory({
  state,
  onLoad,
}: {
  /** まだ押していなければ undefined。 */
  state?: RunHistoryState;
  onLoad: () => void;
}) {
  if (state === undefined) {
    return (
      <button type="button" onClick={onLoad}>
        履歴を読み込む
      </button>
    );
  }

  if (state.status === "loading") {
    return <p className="hint">読み込み中…</p>;
  }

  if (state.status === "error") {
    return <ErrorNotice failure={state.failure} />;
  }

  if (state.runs.length === 0) {
    return <p className="hint">実行の記録はありません。</p>;
  }

  return (
    <>
      <ul className="plain-list">
        {state.runs.map((run) => (
          <li key={run.id}>
            <span className="hint">{formatRunTimestamp(run.createdAt)}</span>
            {/*
              その 1 回で何をしたかを出す。**畳んだ結果ではない**ので、
              触らなかった item はここには現れない（ADR 0026）。
            */}
            <ul className="plain-list">
              {run.items.length === 0 ? (
                <li className="hint">作られたものはありません。</li>
              ) : (
                run.items.map((it) => (
                  <li key={it.itemId}>
                    <span className="kind">{it.kind}</span> {it.title}
                    {it.action === "updated" && (
                      <span className="badge badge-updated">更新</span>
                    )}
                  </li>
                ))
              )}
            </ul>
          </li>
        ))}
      </ul>
      <button type="button" onClick={onLoad}>
        履歴を読み込み直す
      </button>
    </>
  );
}

/** run の実行時刻。日をまたぐので日付まで出す（解釈の履歴とは違う）。 */
function formatRunTimestamp(at: string): string {
  return new Date(at).toLocaleString("ja-JP");
}

/**
 * 引いた解釈を並べて、どれを見るか選ばせる。
 *
 * **サーバーには何も置かない。** 解釈は GitHub にも DB にも何も作らないので、
 * 残す意味があるのは画面を開いているあいだだけ。保存すると前提のシーンが
 * 変わるので、そこで丸ごと捨てる（`BoardPage` の `save`）。
 *
 * 実行時刻と粒度を添えるのは、見比べる材料がその 2 つだから。同じ粒度で
 * 引き直したのか、指定を変えて引いたのかが読めないと、選ぶ理由が無い。
 */
function InterpretationHistory({
  annotationId,
  runs,
  selectedId,
  onSelect,
}: {
  annotationId: string;
  /** 新しい順。 */
  runs: InterpretationRun[];
  selectedId: number | null;
  onSelect: (runId: number) => void;
}) {
  // 一覧に複数の注釈が並ぶので、id は注釈ごとに分ける。
  const selectId = `interpretation-history-${annotationId}`;

  return (
    <div className="interpretation-history">
      <label htmlFor={selectId}>解釈結果</label>
      <select
        id={selectId}
        value={selectedId ?? ""}
        onChange={(e) => onSelect(Number(e.target.value))}
      >
        {runs.map((run, i) => (
          <option key={run.id} value={run.id}>
            {`${interpretationOrderLabel(i)}・${formatRunTime(run.at)}・粒度 ${
              GRANULARITY_LABEL[run.granularity]
            }`}
          </option>
        ))}
      </select>
    </div>
  );
}

/**
 * 解釈を引いた時刻。
 *
 * 日付は出さない。解釈は保存で捨てるので、画面に並ぶのは同じセッションの
 * ものだけになる。
 */
function formatRunTime(at: string): string {
  return new Date(at).toLocaleTimeString("ja-JP");
}

/**
 * 作成の実行と結果表示。
 *
 * 解釈が済んでいるときだけ出す。何を作るかは開発者が結果を見て決める
 * （中核思想 3）。
 */
function CreationSection({
  annotationId,
  state,
  saving,
  reasons,
  projectAccess,
  creationUnavailable,
  projectLink,
  onCreate,
}: {
  annotationId: string;
  state?: CreationState;
  saving: boolean;
  /** このまま作らせない理由。空なら押させる。 */
  reasons: string[];
  projectAccess: ProjectAccess;
  /** GitHub が未設定なら理由。使えるなら null（ADR 0030）。 */
  creationUnavailable: string | null;
  /** 作成したものを確かめにいく先。組めなければ null（ADR 0025）。 */
  projectLink: ProjectLink | null;
  onCreate: () => void;
}) {
  const running = state?.status === "running";
  const blockedId = `create-blocked-${annotationId}`;

  // **GitHub が未設定なら、権限より先にこちら。** 未設定の構成では
  // projectAccess は unknown にしかならないので、下の denied では拾えない。
  // 解釈まではこのまま続けられることも書く（ADR 0008 / 0030）。
  if (creationUnavailable !== null) {
    return (
      <div className="creation">
        <p className="hint">
          {creationUnavailable}
          {"ブレストと解釈はこのまま続けられます。"}
        </p>
      </div>
    );
  }

  // 書けないと分かっているなら、押させずに理由を出す。押せば GitHub が 403 を
  // 返すので結果は同じだが、理由が読めるのは先に出したときだけ（ADR 0017）。
  if (projectAccess === "denied") {
    return (
      <div className="creation">
        <p className="hint">
          {"この Project に書き込む権限がありません。"}
          {"ブレストと解釈はこのまま続けられます。"}
        </p>
      </div>
    );
  }

  return (
    <div className="creation">
      <button
        type="button"
        onClick={onCreate}
        disabled={running || saving || reasons.length > 0}
        title={saving ? "保存が終わるまで作成できません" : undefined}
        aria-describedby={reasons.length > 0 ? blockedId : undefined}
      >
        {running ? "作成中…" : "GitHub に作成する"}
      </button>

      {/*
        押せない理由は本文として出す。disabled なボタンはフォーカスも当たらない
        ので、title ではキーボードと読み上げの利用者に理由が届かない。
      */}
      {reasons.length > 0 && (
        <p className="hint" id={blockedId}>
          {reasons.join(" ")}
        </p>
      )}

      {state?.status === "error" && <ErrorNotice failure={state.failure} />}

      {state?.status === "done" && (
        <div className="creation-result">
          {/* 途中で失敗しても作れたぶんは残る。何も作られていないと
              誤解して再実行すると、GitHub 側に重複が増える。 */}
          {state.run.incomplete ? (
            // 部分失敗の本文には code を足さない。1 件ずつ理由が違いうるので
            // 1 つの code に落ちない。畳んで見せる扱いだけ揃える。文言は
            // errorMessage.ts、数えるのはこちら。
            <ErrorNotice
              failure={partialCreationFailure(
                partialSummary(state.run.items),
                state.run.error,
              )}
            />
          ) : (
            <p className="hint">{resultSummary(state.run.items)}。</p>
          )}
          <ul className="plain-list">
            {state.run.items.map((it) => (
              <li key={it.itemId}>
                <span className="kind">{it.kind}</span> {it.title}
                {/*
                  作ったのか書き換えたのかを残す。GitHub 側に何が増えたのかは
                  この内訳でしか数えられない（ADR 0026）。
                */}
                {it.action === "updated" && (
                  <span className="badge badge-updated">更新</span>
                )}
                <ItemBody body={it.body} />
              </li>
            ))}
          </ul>
          <ProjectLinkLine link={projectLink} />
        </div>
      )}
    </div>
  );
}

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
function InterpretationDraft({
  annotationId,
  granularity,
  result,
  creation,
  saving,
  projectAccess,
  creationUnavailable,
  previous,
  projectLink,
  onCreate,
}: {
  annotationId: string;
  granularity: Granularity;
  result: Interpretation;
  creation?: CreationState;
  saving: boolean;
  projectAccess: ProjectAccess;
  /** GitHub が未設定なら理由。使えるなら null（ADR 0030）。 */
  creationUnavailable: string | null;
  /** この注釈が GitHub に在らしめているもの。取り残しの算出に使う。 */
  previous: SyncItem[];
  projectLink: ProjectLink | null;
  onCreate: (interpretation: Interpretation) => void;
}) {
  const [draft, setDraft] = useState(() => createDraft(result));

  // 編集後の kind で組み直す。構造を変えたことがその場で見えるようにする。
  const groups = groupByEpic(draft.items.map((d) => d.item));
  // groupByEpic は下書きの項目そのものを並べ替えて返すので、引けない localId は
  // 無い。それでも既定を持つのは、無いものを「選ばれている」と倒さないため。
  const selected = new Map(draft.items.map((d) => [d.item.localId, d.selected]));
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
      orphan={orphans.has(item.localId)}
      frozen={frozen}
      editableKind={editableKind}
      onToggle={() => setDraft((d) => toggleItem(d, item.localId))}
      onKind={(kind) => setDraft((d) => setKind(d, item.localId, kind))}
      onTitle={(title) => setDraft((d) => setTitle(d, item.localId, title))}
      onBody={(body) => setDraft((d) => setBody(d, item.localId, body))}
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
        saving={saving}
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
  orphan,
  frozen,
  editableKind,
  onToggle,
  onKind,
  onTitle,
  onBody,
}: {
  item: InterpretedItem;
  selected: boolean;
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
        */}
        {item.previousItemId && <span className="badge badge-updated">更新</span>}
      </div>

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
 * 作成結果の内訳を 1 行にする（ADR 0026）。
 *
 * 件数だけでは、GitHub 側に何が増えたのかが分からない。更新は増えないので、
 * 「5 件を作成しました」と出しておいて実際に増えたのが 2 件、ということが起きる。
 */
function resultSummary(items: SyncItem[]): string {
  const { created, updated } = countByAction(items);

  if (updated === 0) return `${created} 件を作成しました`;
  if (created === 0) return `${updated} 件を更新しました`;

  return `${created} 件を作成し、${updated} 件を更新しました`;
}

/**
 * 途中で失敗した run の内訳（ADR 0009 / 0026）。
 *
 * **完了したときとは言い回しを変える。** ここで伝えたいのは「もう GitHub 側に
 * 在る」ことで、何も作られていないと誤解させると再実行で重複が増える。
 */
function partialSummary(items: SyncItem[]): string {
  const { created, updated } = countByAction(items);

  if (updated === 0) return `${created} 件は作成済み`;
  if (created === 0) return `${updated} 件は更新済み`;

  return `${created} 件は作成済み、${updated} 件は更新済み`;
}

function countByAction(items: SyncItem[]): { created: number; updated: number } {
  const updated = items.filter((it) => it.action === "updated").length;
  return { created: items.length - updated, updated };
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

/**
 * 作成した draft issue を確かめにいくリンク 1 行（ADR 0025）。
 *
 * **リストごとに 1 本で、行ごとには置かない。** draft issue には個別の URL が
 * 無く、飛び先はどの行でも同じ Project になる。行ごとに並べると、行ごとに
 * 違う場所へ飛ぶように読めてしまう。
 *
 * Project そのものに着地しないときは、そう書く。リポジトリの Projects まで
 * しか辿れないのに「Project を開く」と言うと、リンクの約束が崩れる。
 */
function ProjectLinkLine({ link }: { link: ProjectLink | null }) {
  // 作成先が未選択のボードでは飛び先が無い。何も出さない。
  if (!link) return null;

  return (
    <p className="hint">
      <a href={link.href} target="_blank" rel="noreferrer">
        {link.exact
          ? "GitHub でこの Project を開く"
          : "GitHub でリポジトリの Projects を開く"}
      </a>
    </p>
  );
}

/**
 * draft issue の本文。既定は畳んでおく。
 *
 * これから作るものと、前回作ったものの両方で使う。同じものを見ているので
 * 見え方を変えない。
 *
 * **markdown として整形しない。** GitHub に送るのはこの生テキストそのもの
 * なので、整形して見せると「確認したもの」と「作られるもの」がずれる。
 */
function ItemBody({ body }: { body: string }) {
  // 契約上は必須の string なので undefined にはならない。空文字だけを見る。
  if (body === "") {
    return <p className="hint">本文なし</p>;
  }

  return (
    <details className="item-body">
      <summary>本文</summary>
      <pre>{body}</pre>
    </details>
  );
}
