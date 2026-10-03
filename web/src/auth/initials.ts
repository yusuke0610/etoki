/**
 * 利用者のメニューの丸に出す頭文字（#200）。
 *
 * 名前を空白で分けた語の先頭 1 文字を、最大 2 文字まで大文字で並べる
 * （`Octo Cat` → `OC`、`山田太郎` → `山`）。**文字は書記素ではなくコードポイントで
 * 数える。** サロゲートペアの漢字や絵文字を半分に割らないため。
 *
 * 名前が空なら空文字を返す。丸は飾りなので、呼ぶ側が別の文字（login など）で
 * 埋めればよい。
 */
export function initialsOf(name: string): string {
  return name
    .split(/\s+/)
    .filter((word) => word !== "")
    .slice(0, 2)
    .map((word) => Array.from(word)[0] ?? "")
    .join("")
    .toUpperCase();
}
