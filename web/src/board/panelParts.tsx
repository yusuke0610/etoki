import type { SyncItem } from "../api/types";
import type { ProjectLink } from "./projectLink";

/**
 * 注釈パネルの中で、複数の節から同じ形で使う部品。
 *
 * **見え方を揃えたいものだけを置く。** draft issue の本文と Project への
 * リンクは、これから作るものと作ったものの両方に出る（ADR 0023 / 0025）。
 */

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
export function ProjectLinkLine({ link }: { link: ProjectLink | null }) {
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
export function ItemBody({ body }: { body: string }) {
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

/**
 * GitHub に届いたか分からない書き込み（ADR 0056）。
 *
 * **「失敗した」とは書かない。** GitHub が受理したあとで応答だけを失った場合も、
 * 受理せずに返した場合も、etoki からは区別できない。分からないことを分からない
 * と出して、確かめるのは開発者に任せる（中核思想 3）。
 *
 * **押し直しの導線は置かない。** 受理されていた場合、もう一度送ると消せない
 * draft issue が重複する。出口は GitHub を見ること 1 つだけなので、リンクを
 * 添える。
 */
export function UnconfirmedItems({
  items,
  link,
}: {
  items: SyncItem[];
  link: ProjectLink | null;
}) {
  if (items.length === 0) return null;

  return (
    <div className="unconfirmed-items">
      <p className="hint">
        {items.length} 件は、GitHub に届いたか確認できていません。作られている
        かもしれません。<strong>もう一度作る前に GitHub を見てください。</strong>
      </p>
      <ul className="plain-list">
        {items.map((it, i) => (
          // 未確定の作成は itemId を持たないので、鍵には使えない。並びは
          // サーバーが返した順で、再描画で入れ替わらない。
          <li key={it.itemId === "" ? `${it.localId}-${i}` : it.itemId}>
            <span className="kind">{it.kind}</span> {it.title}
            <span className="badge badge-unconfirmed">確認できていません</span>
            <ItemBody body={it.body} />
          </li>
        ))}
      </ul>
      <ProjectLinkLine link={link} />
    </div>
  );
}
