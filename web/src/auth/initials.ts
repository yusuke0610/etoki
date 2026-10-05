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
    .map((word) => {
      // **語ごとに大文字にしてから 1 文字取る。** 大文字にすると 2 文字以上に
      // なる文字がある（`ß` → `SS`）。並べてから大文字にすると、2 文字の上限を
      // 越える。
      const first = Array.from(word)[0] ?? "";
      return Array.from(first.toUpperCase())[0] ?? "";
    })
    .join("");
}
