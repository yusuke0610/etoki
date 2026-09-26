import type { BoardRole } from "../api/types";

/**
 * ロールの表示名。値そのものを画面に出すと、意味が伝わらない。
 *
 * ヘッダとメンバー一覧の両方が出すので 1 箇所に置く。写すと、同じロールが
 * 画面によって違う名前で出る。
 */
export const ROLE_LABELS: Record<BoardRole, string> = {
  owner: "オーナー",
  editor: "編集できる",
  viewer: "読むだけ",
};

/**
 * 選択肢に並べる順。**表そのものの並び。**
 *
 * 別の配列や手書きの `<option>` で持たない。`Record` は網羅を `tsc` が見るが、
 * 配列は見ないので、契約にロールを足したときに選択肢から黙って抜ける
 * （`diagramKinds` と同じ形）。
 */
export function roleOptions(): BoardRole[] {
  return Object.keys(ROLE_LABELS) as BoardRole[];
}

/**
 * ロールごとに、ボードを編集できるか（シーンの保存・解釈・作成・改名）。
 *
 * **ロールの上下の順序はここに持たない。** 順序を知っているのはサーバーの
 * `port.BoardRole.AtLeast` だけで、フロントに同じ表を置くと判定が 2 箇所に
 * なる（`internal/CLAUDE.md`）。ここにあるのは押せるボタンを出し分けるための
 * 答えだけで、判定そのものはサーバーが 403 で返す。`Record` なので、ロールが
 * 増えたら答えを書き忘れた時点で `tsc` が落ちる。
 */
const CAN_EDIT: Record<BoardRole, boolean> = {
  owner: true,
  editor: true,
  // viewer は読むだけ。解釈も許さない（ADR 0017）。
  viewer: false,
};

/** ボードを編集できるロールかどうか。 */
export function canEditBoard(role: BoardRole): boolean {
  return CAN_EDIT[role];
}

/** owner だけに許す操作（作成先の変更・招待・削除）を出してよいか。 */
export function isOwner(role: BoardRole): boolean {
  return role === "owner";
}
