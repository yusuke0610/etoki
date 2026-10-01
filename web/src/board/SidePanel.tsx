import { type KeyboardEvent, type ReactNode, useEffect, useRef, useState } from "react";

/** 右のパネルに並べるもの。 */
export type SidePanelTab = "annotations" | "diagram" | "members";

/**
 * 畳んだ帯でタブの名前に添える件数（#202）。**0 件のものは出さない。**
 *
 * 畳んでいるあいだも状態を隠さない（ADR 0064）。添えるのは手を打つ必要が
 * あるもの（未作成・変更あり）だけで、作成済みまで並べると帯が伸びる。
 */
export type RailBadge = {
  label: string;
  /** 色は注釈の 3 状態のバッジ（`badge-*`）と同じにする。 */
  state: "uncreated" | "changed";
  count: number;
};

type TabSpec = {
  id: SidePanelTab;
  label: string;
  content: ReactNode;
  railBadges?: RailBadge[];
};

type Props = {
  /** 出すタブ。権限で使えないもの（viewer の図のドラフト）は渡さない。 */
  tabs: TabSpec[];
  active: SidePanelTab;
  onSelect: (tab: SidePanelTab) => void;
  /** 畳んでいるか。覚えるのは親（端末ごと、`panelState.ts`）。 */
  collapsed: boolean;
  onCollapsedChange: (collapsed: boolean) => void;
};

/** パネルの枠の id。畳む口と帯のボタンが `aria-controls` で指す。 */
const PANEL_ID = "side-panel";

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
 *
 * **畳める**（#202）。畳んだら右端に縦の帯を残し、タブを並べる。**畳んでも
 * 中身は外さない。** 枠に `hidden` を付けるだけにする。外すと、図への指示の
 * 書きかけや招待の途中が、畳んだだけで消える（タブを切り替えたときと同じ理由）。
 */
export function SidePanel({
  tabs,
  active,
  onSelect,
  collapsed,
  onCollapsedChange,
}: Props) {
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
  const railButtons = useRef(new Map<SidePanelTab, HTMLButtonElement>());

  // 開閉のあとに焦点を移す先。**移さないと、押したボタンが消えるので焦点が
  // body に落ちる。** 畳んだら帯の「いまのタブ」へ、帯から開いたらそのタブへ。
  // 描き終えてから移すので effect で行う（押した時点では移す先がまだ無い）。
  const pendingFocus = useRef<"rail" | "tab" | null>(null);
  useEffect(() => {
    const target =
      pendingFocus.current === "rail"
        ? railButtons.current.get(active)
        : pendingFocus.current === "tab"
          ? buttons.current.get(active)
          : undefined;
    pendingFocus.current = null;
    target?.focus();
  }, [collapsed, active]);

  const collapse = () => {
    pendingFocus.current = "rail";
    onCollapsedChange(true);
  };

  const expand = (tab: SidePanelTab) => {
    pendingFocus.current = "tab";
    onSelect(tab);
    onCollapsedChange(false);
  };

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
    <>
      {collapsed && (
        <nav className="side-panel-rail" aria-label="パネル">
          {tabs.map((tab) => {
            const badges = (tab.railBadges ?? []).filter((b) => b.count > 0);
            // 縦書きの見た目の区切りに頼らず、件数まで名前に入れる。
            const name = [
              tab.label,
              ...badges.map((b) => `${b.label} ${b.count} 件`),
            ].join("、");
            return (
              <button
                key={tab.id}
                ref={(el) => {
                  if (el) railButtons.current.set(tab.id, el);
                  else railButtons.current.delete(tab.id);
                }}
                type="button"
                className="side-panel-rail-tab"
                aria-label={name}
                aria-expanded={false}
                aria-controls={PANEL_ID}
                // 開いたときに戻る先が見て分かるように、選んでいたタブを示す。
                aria-current={tab.id === active ? "true" : undefined}
                onClick={() => expand(tab.id)}
              >
                <span>{tab.label}</span>
                {badges.map((b) => (
                  <span key={b.state} className={`badge badge-${b.state}`}>
                    {b.label} <span className="upright">{b.count}</span>
                  </span>
                ))}
              </button>
            );
          })}
        </nav>
      )}

      <div className="side-panel" id={PANEL_ID} hidden={collapsed}>
        <div className="side-panel-head">
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
          <button
            type="button"
            className="quiet side-panel-close"
            aria-label="パネルを閉じる"
            aria-expanded={true}
            aria-controls={PANEL_ID}
            onClick={collapse}
          >
            <span aria-hidden="true">»</span>
          </button>
        </div>

        {tabs.map((tab) => (
          <div
            key={tab.id}
            role="tabpanel"
            id={`side-panel-${tab.id}`}
            aria-labelledby={`side-tab-${tab.id}`}
            className="side-panel-body"
            hidden={tab.id !== active}
          >
            {visited.has(tab.id) && tab.content}
          </div>
        ))}
      </div>
    </>
  );
}
