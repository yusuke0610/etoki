import type { AnnotationStatus } from "../api/types";

/**
 * 注釈の見出し。名前が無ければ一覧上の位置で採番する。
 *
 * Excalidraw の frame は既定で名前を持たず、キャンバス側もそれを `Frame` と
 * しか描かない（採番しない）。名前を頼りにすると、複数の注釈がすべて同じ
 * 見出しで並ぶ（ADR 0021）。
 *
 * **この番号はキャンバスのラベルとは一致しない。** 一覧の中で項目同士を
 * 指し分けるためだけのもので、どのフレームかを確かめる手段はカードを押して
 * キャンバスを寄せることのほう。
 */
export function annotationLabel(name: string, index: number): string {
  return name.trim() === "" ? `注釈 ${index + 1}` : name;
}

/**
 * 注釈 ID から見出しを引ける対応。
 *
 * 「選択中のフレーム」欄と「状態」欄で同じ注釈に同じ見出しを出すために作る。
 * 片方だけ名前、もう片方だけ番号になると、同じものが 2 つに見える。
 */
export function annotationLabels(annotations: AnnotationStatus[]): Map<string, string> {
  return new Map(annotations.map((a, i) => [a.id, annotationLabel(a.name, i)]));
}

/**
 * まだ注釈になっていない frame の見出し。
 *
 * 一覧に並んでいないので番号を持たない。ここで別に採番すると、意味の違う
 * 番号が同じ画面に 2 種類出る。
 */
export function frameLabel(name: string): string {
  return name.trim() === "" ? "名前のないフレーム" : name;
}
