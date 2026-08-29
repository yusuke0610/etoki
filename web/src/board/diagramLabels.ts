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
 * 選択肢に並べる順。
 *
 * `Object.keys` に任せない。並びが型の定義順に依存すると、契約の順序を
 * 変えたときに画面の並びが黙って変わる。
 */
export const DIAGRAM_KINDS: DiagramKind[] = [
  "todo",
  "mindmap",
  "sequence",
  "er",
  "architecture",
];
