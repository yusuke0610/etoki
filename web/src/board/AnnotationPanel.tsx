import type { AnnotationStatus, Granularity, SyncState } from "../api/boards";

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
};

export function AnnotationPanel({
  annotations,
  markableFrameIds,
  unmarkableFrameIds,
  onMark,
  onUnmark,
  onChangeGranularity,
  stale,
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
              </li>
            ))}
          </ul>
        )}
      </section>
    </aside>
  );
}
