/**
 * 開いているボードと URL を対応づける（ADR 0056、#149）。
 *
 * **URL を組み立てる規則はここだけに置く。** 散らすと、読む側と書く側で
 * 別の形を持ってしまい、自分で書いた URL を自分で開けなくなる
 * （`board/projectLink.ts` が GitHub の URL を 1 つに閉じてあるのと同じ形）。
 *
 * **形はクエリ（`/?board={id}`）にしてある。** パス（`/boards/{id}`）にすると、
 * 未知のパスに `index.html` を返す fallback が要り、打ち間違えた URL がすべて
 * 200 + HTML で返る（ADR 0032）。クエリなら配信するパスは `/` のままで済む。
 */

/** URL に載るもの。 */
export type BoardLocation = {
  /** 開いているボードの ID。null は「開いていない」。 */
  boardId: string | null;
  /**
   * 作成先を選び直している最中かどうか。
   *
   * **ボードを開いていないときは常に false。** 選び直しは開いているボードに
   * 対する操作なので、単独では状態として意味を持たない。
   */
  picking: boolean;
};

/** 開いていない状態。 */
export const NO_BOARD: BoardLocation = { boardId: null, picking: false };

/** クエリのキー。読む側と書く側で同じものを使う。 */
const BOARD_PARAM = "board";
const PICKING_PARAM = "picking";

/** `picking` が立っているとみなす値。 */
const PICKING_ON = "1";

/**
 * `window.location.search` を読む。
 *
 * **知らない param は無視する。** 他所から付いてきた計測用の param などで
 * ボードが開けなくなる理由が無い。
 */
export function parseBoardLocation(search: string): BoardLocation {
  const params = new URLSearchParams(search);
  const boardId = params.get(BOARD_PARAM);
  if (boardId === null || boardId === "") return NO_BOARD;

  return { boardId, picking: params.get(PICKING_PARAM) === PICKING_ON };
}

/**
 * 画面の状態から URL を組む。
 *
 * 返すのは同一オリジンの相対パス。**そのままログイン後の戻り先としても送れる**
 * 形にしてある（サーバーは自オリジンの相対パスだけを受け付ける、ADR 0056）。
 */
export function boardLocationUrl({ boardId, picking }: BoardLocation): string {
  if (boardId === null || boardId === "") return "/";

  const params = new URLSearchParams({ [BOARD_PARAM]: boardId });
  if (picking) params.set(PICKING_PARAM, PICKING_ON);
  return `/?${params.toString()}`;
}
