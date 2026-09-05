import { BLANK_TEMPLATE, type TemplateChoice } from "../excalidraw/template";
import { DIAGRAM_KIND_LABELS, diagramKinds } from "./diagramLabels";

type Props = {
  value: TemplateChoice;
  onChange: (choice: TemplateChoice) => void;
};

/**
 * 新しいボードを何から始めるかを選ばせる。
 *
 * **既定は「空白」**（中核思想 3）。テンプレートは勝手に適用しない。空白を
 * 選択肢の先頭に置いてあるのは、これまでどおりの始め方が最初に目に入る
 * ようにするため。
 *
 * **見出しは種類の表示名をそのまま使う**（`DIAGRAM_KIND_LABELS`）。図の
 * ドラフト生成のパネルと同じ語で並ぶ。片方だけ言い換えると、同じ 5 種が
 * 画面の場所によって違うものに見える。
 *
 * **何のひな形かの説明をここで書き足さない。** 説明が要るほど分かりにくい
 * なら、それは名前のほうを直す話になる。
 */
export function TemplatePicker({ value, onChange }: Props) {
  return (
    <label className="template-picker">
      ひな形
      <select value={value} onChange={(e) => onChange(e.target.value as TemplateChoice)}>
        <option value={BLANK_TEMPLATE}>空白</option>
        {diagramKinds().map((kind) => (
          <option key={kind} value={kind}>
            {DIAGRAM_KIND_LABELS[kind]}
          </option>
        ))}
      </select>
    </label>
  );
}
