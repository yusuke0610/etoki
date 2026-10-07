import type { AnnotationStatus, Granularity, ItemKind, SyncState } from "../../api/types";
import { DIAGRAM_KIND_LABELS } from "../diagram/diagramLabels";

/**
 * 注釈の 3 状態の見出し。右のパネルのカード、中央の面（解釈の結果）、ボード
 * 一覧のカードの件数（#200）に出す。**1 か所に置く。** 2 か所に書くと文言が割れる。
 */
export const STATE_LABEL: Record<SyncState, string> = {
  uncreated: "未作成",
  created: "作成済み",
  changed: "変更あり",
};

/**
 * 粒度の見出し。
 *
 * パネルとキャンバスに重ねる枠で同じ語を出すためにここに置く。片方だけ
 * 言い換えると、同じ注釈が画面の場所によって違う粒度に見える。
 */
export const GRANULARITY_LABEL: Record<Granularity, string> = {
  "": "指定なし",
  epic: "epic",
  issue: "issue",
};

/**
 * 解釈で作るものの種別の見出し。
 *
 * 粒度（注釈に付けるメタデータ）とは別の型なので表も分ける。語が同じでも、
 * 片方に値を足したときにもう片方へ混ざらないようにするため。
 */
export const ITEM_KIND_LABEL: Record<ItemKind, string> = {
  epic: "epic",
  issue: "issue",
};

/**
 * 種別を選択肢に並べる順。**表そのものの並び。**
 *
 * 手書きの `<option>` にしない。`Record` は網羅を `tsc` が見るが、選択肢の
 * 並びは見ないので、契約に種別を足したときに黙って抜ける（`diagramKinds` と
 * 同じ形）。
 */
export function itemKinds(): ItemKind[] {
  return Object.keys(ITEM_KIND_LABEL) as ItemKind[];
}

/**
 * 注釈の見出し。名前が無ければ一覧上の位置で採番する。
 *
 * Excalidraw の frame は既定で名前を持たず、キャンバス側もそれを `Frame` と
 * しか描かない（採番しない）。名前を頼りにすると、複数の注釈がすべて同じ
 * 見出しで並ぶ（ADR 0022）。
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

/**
 * 注釈のカードに出す要約 1 行（#201）。`粒度 epic · 種別 未指定 · GitHub に 2 件`。
 *
 * 粒度と種別はどちらも、選んでいなければ「未指定」と書く。粒度の選択肢の
 * 「指定なし」と語を変えるのは、ここが選ぶ場所ではなく、まだ選んでいないことを
 * 知らせる場所だから。**GitHub の件数は 1 件以上のときだけ添える。** 0 件を
 * 並べると、未作成のカードがどれも同じ長さの行で埋まる。
 */
export function annotationSummary(a: AnnotationStatus): string {
  const granularity = a.granularity === "" ? "未指定" : GRANULARITY_LABEL[a.granularity];
  const kind = a.kind === undefined ? "未指定" : DIAGRAM_KIND_LABELS[a.kind];
  const parts = [`粒度 ${granularity}`, `種別 ${kind}`];
  const created = a.items?.length ?? 0;
  if (created > 0) parts.push(`GitHub に ${created} 件`);
  return parts.join(" · ");
}
