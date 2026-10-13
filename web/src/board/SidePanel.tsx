import { type KeyboardEvent, type ReactNode, useEffect, useRef, useState } from "react";

/** 右のパネルに並べるもの。 */
export type SidePanelTab = "annotations" | "diagram";

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

export type TabSpec = {
  id: SidePanelTab;
  label: string;
  content: ReactNode;
  railBadges?: RailBadge[];
};

/**
 * 畳んだ帯（と、スマホの上の帯）でタブを開くボタンの名前と、添える件数。
 * **件数まで名前に入れる。** 縦書きの帯では見た目の区切りに頼れず、上の帯でも
 * 同じ名前で引けるようにしておく（#199）。
 */
export function panelOpener(tab: TabSpec): { name: string; badges: RailBadge[] } {
  const badges = (tab.railBadges ?? []).filter((b) => b.count > 0);
  const name = [tab.label, ...badges.map((b) => `${b.label} ${b.count} 件`)].join("、");
  return { name, badges };
}

type Props = {
  /** 出すタブ。権限で使えないもの（viewer の図のドラフト）は渡さない。 */
  tabs: TabSpec[];
  active: SidePanelTab;
  onSelect: (tab: SidePanelTab) => void;
  /** 畳んでいるか。覚えるのは親（端末ごと、`panelState.ts`）。 */
  collapsed: boolean;
  onCollapsedChange: (collapsed: boolean) => void;
  /**
   * 畳んだときに右端の帯を出すか。**スマホの置き方では出さない**（#199）。
   * 開く口は上の帯（`BoardBar`）にあり、画面の右端をキャンバスから取らない。
   */
  rail: boolean;
  /**
   * パネルの外から開いた回数（スマホの上の帯）。変わったら、開いたタブへ
   * 焦点を移す。**回数で持つ。** 開いているタブを開き直しても移すため
   * （`AnnotationDetail` の `openRequest` と同じ形）。
   */
  openRequest: number;
};

/** パネルの枠の id。畳む口と帯のボタンが `aria-controls` で指す。 */
export const SIDE_PANEL_ID = "side-panel";

/**
 * 「絵解いた」（注釈）と「etoki AI」（図のドラフト）を 1 か所のタブにまとめた右の
 * パネル（ADR 0065）。
 *
 * 以前はメンバーが上から帯、図のドラフトが左から縦、注釈が右と、開く場所が
 * ばらばらで、開くたびにキャンバスの形が変わった。**開く場所を 1 つにする。**
 * メンバー（共有）はボードそのものの管理なので、メニューからダイアログで開く
 * （#248、ADR 0078）。
 *
 * **1 度開いたタブは描いたまま隠す。** 隠すたびに外すと、手直し中の下書きや
 * 入力中の文がタブを切り替えただけで消える。Excalidraw の `Sidebar` に載せ
 * なかったのはこのため（閉じると中身ごと外れる）。**初めて開くまでは描かない。**
 * 描いたまま隠すだけにすると、開いた時点で何かを引く中身を置いた日に、ボードを
 * 開いただけで取りに行くようになり、押したときだけ引く約束
 * （`.claude/rules/async-ui.md`）が崩れる（メンバーを置いていた頃はそうだった）。
 *
 * 形は WAI-ARIA の tabs。左右の矢印で隣のタブへ移り、そのまま開く。
 *
 * **畳める**（#202）。畳んだら右端に縦の帯を残し、タブを並べる。**畳んでも
 * 中身は外さない。** 枠に `hidden` を付けるだけにする。外すと、図への指示の
 * 書きかけが、畳んだだけで消える（タブを切り替えたときと同じ理由）。
 *
 * **置き方で形が変わる**（#199、`layout.ts`）。キャンバスの横に並べる・重ねる・
 * スマホでは全面に開く。形を決めるのは CSS（`.board[data-layout]`）で、ここが
 * 知るのは右端の帯を出すかどうかだけ。
 */
export function SidePanel({
  tabs,
  active,
  onSelect,
  collapsed,
  onCollapsedChange,
  rail,
  openRequest,
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

  // 外から開いたら、開いたタブへ焦点を移す。**移さないと、押したボタンが
  // パネルの下に隠れるので焦点が取り残される。** 描き終えてから移すのは上と同じ。
  const handledOpen = useRef(openRequest);
  useEffect(() => {
    if (handledOpen.current === openRequest) return;
    handledOpen.current = openRequest;
    buttons.current.get(active)?.focus();
  }, [openRequest, active]);

  // 畳んだあとの焦点は、帯があれば帯の「いまのタブ」へ。帯が無ければ親が
  // 開いた口へ戻す（`BoardBar`）。
  const collapse = () => {
    pendingFocus.current = rail ? "rail" : null;
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
      {collapsed && rail && (
        <nav className="side-panel-rail" aria-label="パネル">
          {tabs.map((tab) => {
            const { name, badges } = panelOpener(tab);
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
                aria-controls={SIDE_PANEL_ID}
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

      <div className="side-panel" id={SIDE_PANEL_ID} hidden={collapsed}>
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
            aria-controls={SIDE_PANEL_ID}
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
            // 中にフォーカスできるものが無いタブ（何も描いていない注釈の一覧など）でも、
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
    </>
  );
}
