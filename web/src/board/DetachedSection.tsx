import type { DetachedAnnotation } from "../api/types";
import { ItemBody, ProjectLinkLine } from "./panelParts";
import type { RunHistoryState } from "./panelShared";
import type { ProjectLink } from "./projectLink";
import { RunHistory, formatRunTimestamp } from "./RunHistory";

/**
 * シーンから消えたのに GitHub 側にものが残っている注釈（#111）。
 *
 * **注釈のカードと同じ形にはしない。** 名前も 3 状態も無く、解釈も作成も
 * できない。混ぜると「押せない注釈」が状態の一覧に並ぶことになる。
 *
 * **etoki からは消しも作り直しもしない**（中核思想 3）。できるのは、GitHub に
 * 残っているものへ辿れるようにするところまで。frame を引き直すと要素の ID が
 * 変わるので、以後は別の注釈として扱われる。**それも書いて渡す。** 書かないと、
 * 引き直せば戻ると読める。
 *
 * 1 件も無ければ節ごと出さない。ふつうは空なので、常に空の枠が並ぶと、
 * 本当に何か残っているときに気づけない。
 */
export function DetachedSection({
  annotations,
  runHistories,
  onLoadRuns,
  projectLink,
}: {
  annotations: DetachedAnnotation[];
  runHistories: Record<string, RunHistoryState>;
  onLoadRuns: (annotationId: string) => void;
  projectLink: ProjectLink | null;
}) {
  if (annotations.length === 0) return null;

  return (
    <section className="panel-section">
      <h3>キャンバスに無い注釈</h3>
      <p className="hint">
        囲みは消えていますが、そこから作った draft issue は GitHub に残っています。
        囲みを引き直しても、これらとは繋がりません。
      </p>

      <ul className="annotation-list">
        {annotations.map((a) => (
          <li key={a.id} className="annotation">
            {/*
              **名前は出せない。** シーンから消えているので取りようが無い。
              何の囲みだったかは、下に並ぶ「作ったもの」から読む。
            */}
            <p className="hint">
              最後の実行:{" "}
              {a.lastSyncedAt === undefined ? "不明" : formatRunTimestamp(a.lastSyncedAt)}
            </p>

            <details open>
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

            {/* 履歴の口はシーンに注釈が残っているかを見ない（ADR 0007）。 */}
            <details className="run-history">
              <summary>実行の履歴</summary>
              <RunHistory state={runHistories[a.id]} onLoad={() => onLoadRuns(a.id)} />
            </details>
          </li>
        ))}
      </ul>
    </section>
  );
}
