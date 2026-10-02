import { type KeyboardEvent, type ReactNode, useRef, useState } from "react";

/** 右のパネルに並べるもの。 */
export type SidePanelTab = "annotations" | "diagram" | "members";

type TabSpec = {
  id: SidePanelTab;
  label: string;
  content: ReactNode;
};

type Props = {
  /** 出すタブ。権限で使えないもの（viewer の図のドラフト）は渡さない。 */
  tabs: TabSpec[];
  active: SidePanelTab;
  onSelect: (tab: SidePanelTab) => void;
};

/**
 * 注釈・図のドラフト・メンバーを 1 か所のタブにまとめた右のパネル（ADR 0065）。
 *
 * 以前はメンバーが上から帯、図のドラフトが左から縦、注釈が右と、開く場所が
 * ばらばらで、開くたびにキャンバスの形が変わった。**開く場所を 1 つにする。**
 *
 * **1 度開いたタブは描いたまま隠す。** 隠すたびに外すと、手直し中の下書きや
 * 入力中の文がタブを切り替えただけで消える。Excalidraw の `Sidebar` に載せ
 * なかったのはこのため（閉じると中身ごと外れる）。**初めて開くまでは描かない。**
 * 描いたまま隠すだけにすると、ボードを開いた時点でメンバーの一覧を取りに行く
 * ようになり、押したときだけ引く約束（`.claude/rules/async-ui.md`）が崩れる。
 *
 * 形は WAI-ARIA の tabs。左右の矢印で隣のタブへ移り、そのまま開く。
 */
export function SidePanel({ tabs, active, onSelect }: Props) {
  // 1 度でも開いたタブ。持つのは描くかどうかだけで、どれを開いているかは
  // 親が持つ（メニューや E2E から開かせる口を親に残すため）。
  const [visited, setVisited] = useState<ReadonlySet<SidePanelTab>>(
    () => new Set([active]),
  );
  if (!visited.has(active)) {
    // 描いている最中に積む。effect で積むと、開いた直後の 1 回は中身の無い
    // パネルが出る。React はこの形（前の値と違うときだけ置く）を許している。
    setVisited(new Set([...visited, active]));
  }

  const buttons = useRef(new Map<SidePanelTab, HTMLButtonElement>());

  const move = (e: KeyboardEvent<HTMLButtonElement>, from: SidePanelTab) => {
    const index = tabs.findIndex((t) => t.id === from);
    const step = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
    const edge = e.key === "Home" ? 0 : e.key === "End" ? tabs.length - 1 : null;
    if (step === 0 && edge === null) return;

    e.preventDefault();
    const next = tabs[edge ?? (index + step + tabs.length) % tabs.length];
    if (!next) return;
    onSelect(next.id);
    buttons.current.get(next.id)?.focus();
  };

  return (
    <div className="side-panel">
      <div className="side-panel-tabs" role="tablist" aria-label="パネル">
        {tabs.map((tab) => {
          const selected = tab.id === active;
          return (
            <button
              key={tab.id}
              ref={(el) => {
                if (el) buttons.current.set(tab.id, el);
                else buttons.current.delete(tab.id);
              }}
              type="button"
              role="tab"
              id={`side-tab-${tab.id}`}
              aria-selected={selected}
              aria-controls={`side-panel-${tab.id}`}
              // 選ばれているタブだけを Tab キーで止める。残りへは矢印で移る。
              tabIndex={selected ? 0 : -1}
              onClick={() => onSelect(tab.id)}
              onKeyDown={(e) => move(e, tab.id)}
            >
              {tab.label}
            </button>
          );
        })}
      </div>

      {tabs.map((tab) => (
        <div
          key={tab.id}
          role="tabpanel"
          id={`side-panel-${tab.id}`}
          aria-labelledby={`side-tab-${tab.id}`}
          className="side-panel-body"
          hidden={tab.id !== active}
          // 中にフォーカスできるものが無いタブ（共有が未設定のメンバーなど）でも、
          // タブから Tab キーでパネルへ移れるようにする。WAI-ARIA の tabs
          // パターンの推奨。外すと、キーボードだけの人はそのパネルの理由に
          // 届かない。
          // eslint-disable-next-line jsx-a11y/no-noninteractive-tabindex
          tabIndex={0}
        >
          {visited.has(tab.id) && tab.content}
        </div>
      ))}
    </div>
  );
}
