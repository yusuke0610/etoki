import type { ReactNode } from "react";

/** 図のドラフトのタブで、どちらの口を出しているか。 */
export type DiagramMode = "generate" | "paste";

type Props = {
  mode: DiagramMode;
  onModeChange: (mode: DiagramMode) => void;
  /** LLM に作らせる口（`DiagramChatPanel`）。 */
  generate: ReactNode;
  /** 既存の mermaid を貼る口（`MermaidPastePanel`）。 */
  paste: ReactNode;
};

/**
 * 右のパネルの「図のドラフト」タブ。LLM に作らせる口と、既存の設計（mermaid）を
 * 貼る口（ADR 0062）を同じ場所に置く。
 *
 * **どちらもキャンバスに置く図を用意するもので、置き場所の規則も同じ**（既存の
 * 絵の右外、ADR 0041 / 0062）。タブを分けると、似たものが 2 つ並んで右のパネルの
 * タブが窮屈になる。
 *
 * **出すのは片方だけ。** 縦に並べるとパネルが長くなり、押した「置く」がどちらの
 * ものかが紛れる。**隠した側も描いたまま残す。** 切り替えるたびに外すと、
 * 書きかけの指示や置けなかった理由の表示が消える（右のパネルのタブと同じ理由、
 * `SidePanel`）。
 */
export function DiagramTab({ mode, onModeChange, generate, paste }: Props) {
  return (
    <div className="diagram-tab">
      <div className="diagram-tab-switch" role="group" aria-label="図の用意のしかた">
        <button
          type="button"
          aria-pressed={mode === "generate"}
          onClick={() => onModeChange("generate")}
        >
          LLM で作る
        </button>
        <button
          type="button"
          aria-pressed={mode === "paste"}
          onClick={() => onModeChange("paste")}
        >
          mermaid を貼る
        </button>
      </div>
      <div hidden={mode !== "generate"}>{generate}</div>
      <div hidden={mode !== "paste"}>{paste}</div>
    </div>
  );
}
