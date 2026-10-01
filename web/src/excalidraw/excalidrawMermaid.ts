/**
 * Excalidraw が貼り付けを mermaid として変換に回すかどうか（ADR 0067）。
 *
 * **Excalidraw 0.18.1 の同名関数の写し。** 元は export されていない。
 * キャンバスに貼られた文字列がこれに当たると、Excalidraw は etoki の守り
 * （受け付ける種類・`secure`・大きさの上限・画像を置かない。ADR 0040 / 0061）を
 * 通さずに mermaid で描く。`onPaste` で先に止めるために同じ判定を持つ。
 *
 * **広げても狭めてもいけない。** 狭めると漏れた分が守りなしで描かれ、広げると
 * 普通のテキストの貼り付けまで止まる。元と揃っていることは
 * `excalidrawMermaid.test.ts` が配布物を読んで確かめている。
 */
export const EXCALIDRAW_MERMAID_KINDS = [
  "flowchart",
  "graph",
  "sequenceDiagram",
  "classDiagram",
  "stateDiagram",
  "stateDiagram-v2",
  "erDiagram",
  "journey",
  "gantt",
  "pie",
  "quadrantChart",
  "requirementDiagram",
  "gitGraph",
  "C4Context",
  "mindmap",
  "timeline",
  "zenuml",
  "sankey",
  "xychart",
  "block",
] as const;

const MERMAID_DEFINITION = new RegExp(
  `^(?:%%{.*?}%%[\\s\\n]*)?\\b(?:${EXCALIDRAW_MERMAID_KINDS.map((x) => `\\s*${x}(-beta)?`).join("|")})\\b`,
);

export function isMaybeMermaidDefinition(text: string): boolean {
  return MERMAID_DEFINITION.test(text.trim());
}
