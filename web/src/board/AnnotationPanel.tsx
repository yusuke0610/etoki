import type {
  AnnotationStatus,
  Granularity,
  Interpretation,
  SyncState,
} from "../api/boards";
import { groupByEpic } from "./interpretation";

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
}: Props) {
  return (
    <aside className="panel">
      <h2>注釈</h2>

      <section className="panel-section">
        <h3>選択中のフレーム</h3>
        {markableFrameIds.length === 0 && unmarkableFrameIds.length === 0 ? (
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

                <InterpretationSection
                  state={interpretations[a.id]}
                  stale={stale}
                  onInterpret={() => onInterpret(a.id)}
                />
              </li>
            ))}
          </ul>
        )}
      </section>
    </aside>
  );
}

type InterpretationSectionProps = {
  state?: InterpretationState;
  /** 未保存の変更があると、解釈は保存済みシーンに対して行われる。 */
  stale: boolean;
  onInterpret: () => void;
};

/**
 * 解釈の実行と結果表示。
 *
 * 結果を見せるだけで、ここから GitHub には何も作らない。何を作るかは
 * 開発者が別途トリガーする。
 */
function InterpretationSection({
  state,
  stale,
  onInterpret,
}: InterpretationSectionProps) {
  const running = state?.status === "running";

  return (
    <div className="interpretation">
      <button type="button" onClick={onInterpret} disabled={running}>
        {running ? "解釈中…" : "解釈する"}
      </button>

      {stale && (
        <p className="hint">
          未保存の変更は解釈に含まれません。保存してから実行してください。
        </p>
      )}

      {state?.status === "error" && <p className="error">{state.message}</p>}

      {state?.status === "done" && <InterpretationResult result={state.result} />}
    </div>
  );
}

/**
 * 解釈結果。summary を最初に見せる。
 *
 * summary は GitHub には作らない。LLM がこの囲みをどう読んだかを開発者が
 * 確かめるための材料。
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
                </>
              ) : (
                <span className="hint">epic に属さない issue</span>
              )}

              {g.issues.length > 0 && (
                <ul className="plain-list">
                  {g.issues.map((it) => (
                    <li key={it.localId}>
                      <span className="kind">issue</span> {it.title}
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
