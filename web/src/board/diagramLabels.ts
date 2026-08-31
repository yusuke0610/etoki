import type { DiagramKind } from "../api/types";

/**
 * 図の種類の表示名。値そのものを画面に出しても、何のひな形かは伝わらない。
 *
 * **`Record` にすることが担保。** `DiagramKind` は `api/openapi.yaml` からの
 * 生成物なので、契約に種類を足して名前を書き忘れると `tsc` が落ちる
 * （`ROLE_LABELS` と同じ形）。
 *
 * **どの mermaid 記法で書かれるかはここに書かない。** 記法を選ぶのはサーバー
 * （ADR 0041）で、画面が知っているとその判断が 2 箇所になる。
 */
export const DIAGRAM_KIND_LABELS: Record<DiagramKind, string> = {
  todo: "やること",
  mindmap: "マインドマップ",
  sequence: "シーケンス図",
  er: "ER 図",
  architecture: "システム構成図",
};

/**
 * 選択肢に並べる順。**表そのものの並び。**
 *
 * 別の配列で持たない。`Record` は網羅を `tsc` が見るが、配列は見ないので、
 * 契約に種類を足したときに選択肢から黙って抜ける。読む側が
 * `DIAGRAM_KIND_LABELS` を引く形にしておけば、抜けようがない
 * （`AnnotationPanel` が `GRANULARITY_LABEL` を引くのと同じ形）。
 */
export function diagramKinds(): DiagramKind[] {
  return Object.keys(DIAGRAM_KIND_LABELS) as DiagramKind[];
}
