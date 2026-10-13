import { type ReactNode, useEffect, useRef } from "react";

import { panelOpener, SIDE_PANEL_ID, type SidePanelTab, type TabSpec } from "./SidePanel";

/** 上の帯でパネルを開くボタンの id。注釈の詳細が閉じたときの焦点の戻り先に使う。 */
export function boardBarButtonId(tab: SidePanelTab): string {
  return `board-bar-${tab}`;
}

type Props = {
  /** 右のパネルと同じタブ。名前と件数だけを読む。 */
  tabs: TabSpec[];
  /** パネルで選んでいるタブ。 */
  active: SidePanelTab;
  /** パネルを全面に開いているか。 */
  panelOpen: boolean;
  /** そのタブでパネルを全面に開く。 */
  onOpen: (tab: SidePanelTab) => void;
  /** 下の帯の中身（ロール・作成先・大きさ、`BoardContext`）。 */
  context: ReactNode;
};

/**
 * スマホの置き方で、キャンバスの上に置く etoki の帯の 2 段目（#199）。パネルを
 * 開くボタンと、下の帯の中身を横に流す。1 段目（ボード名・未保存・「絵解き」）は
 * `BoardStatus` で、置くのは `BoardPage`。
 *
 * **Excalidraw の外に置く。** モバイル用 UI では `Footer` が描かれず、
 * `renderTopRightUI` はツールバーと同じ行に詰められる。拡張点に載せたままだと、
 * 状態を常に見せる約束（ADR 0064 / 0065）が崩れる。
 *
 * パネルを開くボタンには、畳んだ帯（`SidePanel`）と同じく手を打つ必要がある
 * 件数を添える。**パネルを閉じたら、開いたボタンへ焦点を戻す。** 戻さないと、
 * 焦点は隠したパネルの中に取り残される。
 */
export function BoardBar({ tabs, active, panelOpen, onOpen, context }: Props) {
  const buttons = useRef(new Map<SidePanelTab, HTMLButtonElement>());
  // 開いたボタン。開いたあとでタブを切り替えても、戻るのは押したボタン。
  const opener = useRef<SidePanelTab | null>(null);

  const wasOpen = useRef(panelOpen);
  useEffect(() => {
    if (wasOpen.current && !panelOpen && opener.current !== null) {
      buttons.current.get(opener.current)?.focus();
    }
    wasOpen.current = panelOpen;
  }, [panelOpen]);

  return (
    <div className="board-bar board-bar-row">
      <nav className="board-bar-panels" aria-label="パネル">
        {tabs.map((tab) => {
          const { name, badges } = panelOpener(tab);
          return (
            <button
              key={tab.id}
              ref={(el) => {
                if (el) buttons.current.set(tab.id, el);
                else buttons.current.delete(tab.id);
              }}
              id={boardBarButtonId(tab.id)}
              type="button"
              aria-label={name}
              aria-expanded={panelOpen && tab.id === active}
              aria-controls={SIDE_PANEL_ID}
              onClick={() => {
                opener.current = tab.id;
                onOpen(tab.id);
              }}
            >
              <span>{tab.label}</span>
              {badges.map((b) => (
                <span key={b.state} className={`badge badge-${b.state}`}>
                  {b.label} {b.count}
                </span>
              ))}
            </button>
          );
        })}
      </nav>
      {context}
    </div>
  );
}
