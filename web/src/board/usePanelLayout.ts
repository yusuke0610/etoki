import { useCallback, useMemo, useState } from "react";

import type { BoardLayout } from "./layout";
import { readPanelCollapsed, writePanelCollapsed } from "./panelState";
import type { SidePanelTab } from "./SidePanel";
import { useBoardLayout } from "./useBoardLayout";

export type PanelLayout = {
  /** 置き方（`layout.ts`、#199）。並べる・重ねる・スマホ。 */
  layout: BoardLayout;
  /** 置き方を測る枠に付ける ref（`useBoardLayout`）。 */
  measureLayout: (el: HTMLElement | null) => void;
  phone: boolean;
  /** 右のパネルでどのタブを開いているか（`SidePanel`）。既定は注釈。 */
  panelTab: SidePanelTab;
  setPanelTab: (tab: SidePanelTab) => void;
  /** 広い画面で右のパネルを畳んでいるか（#202）。端末ごとに覚える。 */
  panelCollapsed: boolean;
  changePanelCollapsed: (collapsed: boolean) => void;
  /** スマホでパネルを全面に開いているか。覚えない。 */
  phonePanelOpen: boolean;
  setPhonePanelOpen: (open: boolean) => void;
  /** 上の帯からパネルを開いた回数（`SidePanel` の `openRequest`）。 */
  panelOpenRequest: number;
  /** スマホで、そのタブを全面に開く。 */
  openPhonePanel: (tab: SidePanelTab) => void;
  /** スマホで、キャンバスを見せるために全面のパネルを閉じる。 */
  closePhonePanel: () => void;
  /**
   * パネルのそのタブを見せる。広い画面では開いてそのタブにし、スマホでは全面に
   * 開く（上の帯から開くのと同じ口、#199）。
   */
  showTab: (tab: SidePanelTab) => void;
};

/**
 * 右のパネルの置き方と開閉（#352 で `BoardPage` から切り出し）。
 *
 * **機能は幅で変えない**（ADR 0075）。ここが持つのは置き方と、置き方ごとの開閉
 * だけ。広い画面の開閉とスマホの開閉は別々に持つので、スマホで開いても広い画面の
 * 開閉は変わらない。
 */
export function usePanelLayout(): PanelLayout {
  const [panelTab, setPanelTab] = useState<SidePanelTab>("annotations");
  // 端末ごとに覚えた値で始める。
  const [panelCollapsed, setPanelCollapsed] = useState(() => readPanelCollapsed());
  const changePanelCollapsed = useCallback((collapsed: boolean) => {
    setPanelCollapsed(collapsed);
    writePanelCollapsed(collapsed);
  }, []);
  const [layout, measureLayout] = useBoardLayout();
  const phone = layout === "phone";
  // **覚えない。** いつも畳んだ状態で始める（#202）。
  const [phonePanelOpen, setPhonePanelOpen] = useState(false);
  const [panelOpenRequest, setPanelOpenRequest] = useState(0);
  // スマホの置き方に入ったら畳む。描いている最中に置く（前の値と違うとき
  // だけ置く形なので React が許している）。effect にすると、入った直後の 1 回は
  // 前の置き方で開いていたパネルが全面に出る。
  const [layoutSeen, setLayoutSeen] = useState(layout);
  if (layoutSeen !== layout) {
    setLayoutSeen(layout);
    if (layout === "phone") setPhonePanelOpen(false);
  }
  const openPhonePanel = useCallback((tab: SidePanelTab) => {
    setPanelTab(tab);
    setPhonePanelOpen(true);
    setPanelOpenRequest((n) => n + 1);
  }, []);
  // 置き方を見ずに閉じてよい。スマホでなければこの値は読まれず、スマホに入る
  // ときに畳み直す。
  const closePhonePanel = useCallback(() => setPhonePanelOpen(false), []);
  const showTab = useCallback(
    (tab: SidePanelTab) => {
      if (phone) {
        openPhonePanel(tab);
        return;
      }
      setPanelTab(tab);
      changePanelCollapsed(false);
    },
    [changePanelCollapsed, openPhonePanel, phone],
  );

  return useMemo(
    () => ({
      layout,
      measureLayout,
      phone,
      panelTab,
      setPanelTab,
      panelCollapsed,
      changePanelCollapsed,
      phonePanelOpen,
      setPhonePanelOpen,
      panelOpenRequest,
      openPhonePanel,
      closePhonePanel,
      showTab,
    }),
    [
      layout,
      measureLayout,
      phone,
      panelTab,
      panelCollapsed,
      changePanelCollapsed,
      phonePanelOpen,
      panelOpenRequest,
      openPhonePanel,
      closePhonePanel,
      showTab,
    ],
  );
}
