import { partialCreationFailure } from "../api/errorMessage";
import type { ProjectAccess } from "../api/types";
import { ErrorNotice } from "../ErrorNotice";
import { partialSummary, resultSummary } from "./itemSummary";
import { ItemBody, ProjectLinkLine } from "./panelParts";
import type { CreationState } from "./panelShared";
import type { ProjectLink } from "./projectLink";

/**
 * 作成の実行と結果表示。
 *
 * 解釈が済んでいるときだけ出す。何を作るかは開発者が結果を見て決める
 * （中核思想 3）。
 */
export function CreationSection({
  annotationId,
  state,
  creationBlocked,
  reasons,
  projectAccess,
  creationUnavailable,
  projectLink,
  onCreate,
}: {
  annotationId: string;
  state?: CreationState;
  /**
   * いま作成を始められない理由。始められるなら null。
   *
   * **`BoardPage` が `exclusion.ts` の表から引いて渡す。** ここで
   * `saving` / `importing` から組み直すと、同じ判定がヘッダーと 2 箇所に
   * なる（#146）。
   */
  creationBlocked: string | null;
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
  // 作成できない理由。押せるなら null（ADR 0039）。
  //
  // **不備が先。** 保存が終わっても、1 件も選ばれていなければ押せないままなので、
  // 一時的なほうを先に出すと待った人が同じところで止まる（解釈のボタンと同じ順）。
  //
  // **`blockingReasons` には混ぜない。** あちらは下書きだけを見て「作るものが
  // 揃っているか」に答える純関数で、進行中かどうかを知らない。混ぜると UI の
  // 一時的な状態を引数に取ることになる。
  //
  // **未設定の説明と違って、注釈ごとにボタンの下へ置く**（ADR 0030 の「パネルに
  // 1 つ」と揃えない）。あちらは全注釈で同じことを恒常的に言うが、こちらは
  // 出ている時間が保存の 1 往復ぶんしかない。パネルに上げると、作成ボタンが
  // 1 つも無い注釈しか無いときにも出る。
  const blocked = reasons.length > 0 ? reasons.join(" ") : creationBlocked;

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
        disabled={running || blocked !== null}
        aria-describedby={blocked !== null ? blockedId : undefined}
      >
        {running ? "作成中…" : "GitHub に作成する"}
      </button>

      {/*
        押せない理由は本文として出す。disabled なボタンはフォーカスも当たらない
        ので、title ではキーボードと読み上げの利用者に理由が届かない。
      */}
      {blocked !== null && (
        <p className="hint" id={blockedId}>
          {blocked}
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
