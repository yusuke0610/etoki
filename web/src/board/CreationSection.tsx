import { partialCreationFailure } from "../api/errorMessage";
import type { ProjectAccess } from "../api/types";
import { ErrorNotice } from "../ErrorNotice";
import { partialSummary, resultSummary } from "./itemSummary";
import type { CreateControl } from "./DetailBand";
import { ItemBody, ItemLink, ProjectLinkLine, UnconfirmedItems } from "./panelParts";
import type { CreationState } from "./panelShared";
import type { ProjectLink } from "./projectLink";

/**
 * 帯の「GitHub に作成する」を組み立てる（`DetailBand`）。
 *
 * 解釈が済んでいるときだけ呼ぶ。何を作るかは開発者が結果を見て決める
 * （中核思想 3）。
 */
export function createControlOf({
  state,
  creationBlocked,
  reasons,
  projectAccess,
  creationUnavailable,
  notice,
  onCreate,
}: {
  state?: CreationState;
  /**
   * いま作成を始められない理由。始められるなら null。
   *
   * **`BoardPage` が `exclusion.ts` の表から引いて渡す。** ここで
   * `saving` / `importing` から組み直すと、同じ判定が右上・メニューと 2 箇所に
   * なる（#146）。
   */
  creationBlocked: string | null;
  /** このまま作らせない理由。空なら押させる。 */
  reasons: string[];
  projectAccess: ProjectAccess;
  /** GitHub が未設定なら理由。使えるなら null（ADR 0030）。 */
  creationUnavailable: string | null;
  /** 押せるときに出す、取り消せないことと作る先の文。 */
  notice: string;
  onCreate: () => void;
}): CreateControl {
  // **GitHub が未設定なら、権限より先にこちら。** 未設定の構成では
  // projectAccess は unknown にしかならないので、下の denied では拾えない。
  // 解釈まではこのまま続けられることも書く（ADR 0008 / 0030）。
  if (creationUnavailable !== null) {
    return {
      kind: "unavailable",
      text: `${creationUnavailable}ブレストと解釈はこのまま続けられます。`,
    };
  }

  // 書けないと分かっているなら、押させずに理由を出す。押せば GitHub が 403 を
  // 返すので結果は同じだが、理由が読めるのは先に出したときだけ（ADR 0017）。
  if (projectAccess === "denied") {
    return {
      kind: "unavailable",
      text: "この Project に書き込む権限がありません。ブレストと解釈はこのまま続けられます。",
    };
  }

  return {
    kind: "ready",
    running: state?.status === "running",
    // 作成できない理由。押せるなら null（ADR 0039）。
    //
    // **不備が先。** 保存が終わっても、1 件も選ばれていなければ押せないままなので、
    // 一時的なほうを先に出すと待った人が同じところで止まる（解釈のボタンと同じ順）。
    //
    // **`blockingReasons` には混ぜない。** あちらは下書きだけを見て「作るものが
    // 揃っているか」に答える純関数で、進行中かどうかを知らない。混ぜると UI の
    // 一時的な状態を引数に取ることになる。
    blocked: reasons.length > 0 ? reasons.join(" ") : creationBlocked,
    notice,
    onCreate,
  };
}

/**
 * 作成の結果。帯ではなく本文に出す（`DraftEditor`）。作ったものの一覧は長く
 * なりうるので、下端に固定した帯には入れない。
 */
export function CreationResult({
  state,
  projectLink,
}: {
  state?: CreationState;
  /** 作成したものを確かめにいく先。組めなければ null（ADR 0025）。 */
  projectLink: ProjectLink | null;
}) {
  return (
    <>
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
            {state.run.items
              .filter((it) => it.confirmed)
              .map((it) => (
                <li key={it.itemId}>
                  <span className="kind">{it.kind}</span> {it.title}
                  {/*
                    作ったのか書き換えたのかを残す。GitHub 側に何が増えたのかは
                    この内訳でしか数えられない（ADR 0026）。
                  */}
                  {it.action === "updated" && (
                    <span className="badge badge-updated">更新</span>
                  )}
                  <ItemLink link={projectLink} item={it} />
                  <ItemBody body={it.body} />
                </li>
              ))}
          </ul>
          <ProjectLinkLine link={projectLink} />
          {/*
            届いたか分からないものは、確かに作れたものと同じリストに並べない
            （ADR 0056）。並べると「作れた」と読まれる。
          */}
          <UnconfirmedItems
            items={state.run.items.filter((it) => !it.confirmed)}
            link={projectLink}
          />
        </div>
      )}
    </>
  );
}
