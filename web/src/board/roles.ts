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
