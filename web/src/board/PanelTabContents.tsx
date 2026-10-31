import type { ComponentProps } from "react";

import { ErrorBoundary } from "../app/ErrorBoundary";
import { AnnotationPanel } from "./annotations/AnnotationPanel";
import { DiagramChatPanel } from "./diagram/DiagramChatPanel";
import { DiagramTab, type DiagramMode } from "./diagram/DiagramTab";
import { MermaidPastePanel } from "./diagram/MermaidPastePanel";

/*
 * 右のパネルのタブの中身（`BoardPage` の `panelTabs`）。
 *
 * **パネルは 1 枚ずつ境界で包む**（ADR 0027）。落ちたのが 1 枚でも外側で
 * 受けると、キャンバスごと外れて未保存のブレストが消える。
 */

/** 「絵解いた」タブ。注釈の一覧。 */
export function AnnotationsTabContent(props: ComponentProps<typeof AnnotationPanel>) {
  return (
    <ErrorBoundary name="「絵解いた」パネル" recovery="remount">
      <AnnotationPanel {...props} />
    </ErrorBoundary>
  );
}

type DiagramTabContentProps = {
  mode: DiagramMode;
  onModeChange: (mode: DiagramMode) => void;
  chat: ComponentProps<typeof DiagramChatPanel>;
  paste: ComponentProps<typeof MermaidPastePanel>;
};

/**
 * 「etoki AI」タブ。LLM に図を作らせる口と、mermaid を貼る口（#248）。
 * 切り替えて一方だけを出し、隠した側も描いたまま残す（`DiagramTab`）。
 */
export function DiagramTabContent({
  mode,
  onModeChange,
  chat,
  paste,
}: DiagramTabContentProps) {
  return (
    <DiagramTab
      mode={mode}
      onModeChange={onModeChange}
      generate={
        <ErrorBoundary name="図のドラフト" recovery="remount">
          <DiagramChatPanel {...chat} />
        </ErrorBoundary>
      }
      paste={
        <ErrorBoundary name="mermaid の貼り付け" recovery="remount">
          <MermaidPastePanel {...paste} />
        </ErrorBoundary>
      }
    />
  );
}
