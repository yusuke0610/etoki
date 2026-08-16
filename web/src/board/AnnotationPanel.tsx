import type {
  AnnotationStatus,
  CreatedRun,
  Granularity,
  Interpretation,
  ProjectAccess,
  SyncState,
} from "../api/types";
import { groupByEpic } from "./interpretation";

/** 注釈 1 つぶんの作成の進み具合。 */
export type CreationState =
  | { status: "running" }
  | { status: "done"; run: CreatedRun }
  | { status: "error"; message: string };

/** 注釈 1 つぶんの解釈の進み具合。 */
export type InterpretationState =
  | { status: "running" }
  | { status: "done"; result: Interpretation }
  | { status: "error"; message: string };

const STATE_LABEL: Record<SyncState, string> = {
  uncreated: "未作成",
  created: "作成済み",
  changed: "変更あり",
};

const GRANULARITY_LABEL: Record<Granularity, string> = {
  "": "指定なし",
  epic: "epic",
  issue: "issue",
};

type Props = {
  annotations: AnnotationStatus[];
  /** 選択中の frame のうち、まだ注釈になっていないもの。 */
  markableFrameIds: string[];
  /** 選択中の frame のうち、すでに注釈になっているもの。 */
  unmarkableFrameIds: string[];
  onMark: (frameId: string, granularity: Granularity) => void;
  onUnmark: (frameId: string) => void;
  onChangeGranularity: (frameId: string, granularity: Granularity) => void;
  /** 未保存の変更があるとき、状態表示は古い可能性がある。 */
  stale: boolean;
  /** 注釈 ID をキーにした解釈の状態。未実行の注釈は入っていない。 */
  interpretations: Record<string, InterpretationState>;
  onInterpret: (annotationId: string) => void;
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
};

export function AnnotationPanel({
  annotations,
  markableFrameIds,
  unmarkableFrameIds,
  onMark,
  onUnmark,
  onChangeGranularity,
  stale,
  interpretations,
  onInterpret,
  creations,
  saving,
  onCreate,
  canEdit,
  projectAccess,
}: Props) {
  return (
    <aside className="panel">
      <h2>注釈</h2>

      {!canEdit && (
        <p className="hint" role="status">
          {"読むだけの権限で開いています。編集・解釈・作成はできません。"}
        </p>
      )}

      <section className="panel-section">
        <h3>選択中のフレーム</h3>
        {!canEdit ? (
          <p className="hint">注釈を付け外しできるのは編集できる人だけです。</p>
        ) : markableFrameIds.length === 0 && unmarkableFrameIds.length === 0 ? (
          <p className="hint">
            フレームツール（F）で囲んでから、そのフレームを選択してください。
          </p>
        ) : (
          <ul className="plain-list">
            {markableFrameIds.map((id) => (
              <li key={id}>
                <button type="button" onClick={() => onMark(id, "")}>
                  このフレームを注釈にする
                </button>
              </li>
            ))}
            {unmarkableFrameIds.map((id) => (
              <li key={id}>
                <button type="button" onClick={() => onUnmark(id)}>
                  注釈の指定を外す
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
            {annotations.map((a) => (
              <li key={a.id} className="annotation">
                <div className="annotation-head">
                  <span className="annotation-name">{a.name || "（名前なし）"}</span>
                  <span className={`badge badge-${a.state}`}>{STATE_LABEL[a.state]}</span>
                </div>

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
                    <summary>前回作成した {a.items.length} 件</summary>
                    <ul className="plain-list">
                      {a.items.map((it) => (
                        <li key={it.itemId}>
                          <span className="kind">{it.kind}</span> {it.title}
                        </li>
                      ))}
                    </ul>
                  </details>
                )}

                {canEdit && (
                  <InterpretationSection
                    annotationId={a.id}
                    state={interpretations[a.id]}
                    creation={creations[a.id]}
                    stale={stale}
                    saving={saving}
                    projectAccess={projectAccess}
                    onInterpret={() => onInterpret(a.id)}
                    onCreate={(interpretation) => onCreate(a.id, interpretation)}
                  />
                )}
              </li>
            ))}
          </ul>
        )}
      </section>
    </aside>
  );
}

type InterpretationSectionProps = {
  /** 説明文の id を注釈ごとに分けるために持つ。一覧に複数並ぶため。 */
  annotationId: string;
  state?: InterpretationState;
  creation?: CreationState;
  /** 未保存の変更があるあいだは解釈させない（ADR 0018）。 */
  stale: boolean;
  saving: boolean;
  projectAccess: ProjectAccess;
  onInterpret: () => void;
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
  state,
  creation,
  stale,
  saving,
  projectAccess,
  onInterpret,
  onCreate,
}: InterpretationSectionProps) {
  const running = state?.status === "running";
  // 押せない理由は title に隠さず本文として出す。disabled なボタンはフォーカスも
  // 当たらないので、title ではキーボードと読み上げの利用者に理由が届かない。
  const blockedId = `interpret-blocked-${annotationId}`;

  return (
    <div className="interpretation">
      {/*
        未保存のあいだは押させない。テキストは保存済みシーンから、画像は画面
        から取るので、揃っていないと 1 回の解釈の入力が食い違う（ADR 0018）。
      */}
      <button
        type="button"
        onClick={onInterpret}
        disabled={running || stale}
        aria-describedby={stale ? blockedId : undefined}
      >
        {running ? "解釈中…" : "解釈する"}
      </button>

      {stale && (
        <p className="hint" id={blockedId}>
          保存してから解釈できます。テキストは保存済みのシーンから、
          画像は画面から取るためです。
        </p>
      )}

      {state?.status === "error" && <p className="error">{state.message}</p>}

      {state?.status === "done" && (
        <>
          <InterpretationResult result={state.result} />
          <CreationSection
            state={creation}
            saving={saving}
            projectAccess={projectAccess}
            onCreate={() => onCreate(state.result)}
          />
        </>
      )}
    </div>
  );
}

/**
 * 作成の実行と結果表示。
 *
 * 解釈が済んでいるときだけ出す。何を作るかは開発者が結果を見て決める
 * （中核思想 3）。
 */
function CreationSection({
  state,
  saving,
  projectAccess,
  onCreate,
}: {
  state?: CreationState;
  saving: boolean;
  projectAccess: ProjectAccess;
  onCreate: () => void;
}) {
  const running = state?.status === "running";

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
        disabled={running || saving}
        title={saving ? "保存が終わるまで作成できません" : undefined}
      >
        {running ? "作成中…" : "GitHub に作成する"}
      </button>

      {state?.status === "error" && <p className="error">{state.message}</p>}

      {state?.status === "done" && (
        <div className="creation-result">
          {/* 途中で失敗しても作れたぶんは残る。何も作られていないと
              誤解して再実行すると、GitHub 側に重複が増える。 */}
          {state.run.incomplete ? (
            <p className="error">
              途中で失敗しました（{state.run.items.length} 件は作成済み）:{" "}
              {state.run.error}
            </p>
          ) : (
            <p className="hint">{state.run.items.length} 件を作成しました。</p>
          )}
          <ul className="plain-list">
            {state.run.items.map((it) => (
              <li key={it.itemId}>
                <span className="kind">{it.kind}</span> {it.title}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

/**
 * 解釈結果。summary を最初に見せ、item ごとの本文も読める形で出す。
 *
 * summary は GitHub には作らない。LLM がこの囲みをどう読んだかを開発者が
 * 確かめるための材料（ADR 0006）。
 *
 * 本文は summary とは別に要る。summary は解釈全体の要約であって、これから
 * 作られる draft issue の中身ではない。作成は取り消せない（ADR 0009）ので、
 * 押す前に中身が読めていなければならない。
 */
function InterpretationResult({ result }: { result: Interpretation }) {
  const groups = groupByEpic(result.items);

  return (
    <div className="interpretation-result">
      <p className="summary">{result.summary}</p>

      {groups.length === 0 ? (
        <p className="hint">作成される項目はありません。</p>
      ) : (
        <ul className="plain-list">
          {groups.map((g, i) => (
            <li key={g.epic?.localId ?? `orphans-${i}`}>
              {g.epic ? (
                <>
                  <span className="kind">epic</span> {g.epic.title}
                  <ItemBody body={g.epic.body} />
                </>
              ) : (
                <span className="hint">epic に属さない issue</span>
              )}

              {g.issues.length > 0 && (
                <ul className="plain-list">
                  {g.issues.map((it) => (
                    <li key={it.localId}>
                      <span className="kind">issue</span> {it.title}
                      <ItemBody body={it.body} />
                    </li>
                  ))}
                </ul>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/**
 * draft issue の本文。既定は畳んでおく。
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
