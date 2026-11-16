import type { AnnotationCounts, BoardListEntry, SyncState } from "../api/types";
import { STATE_LABEL } from "../board/annotations/annotationLabel";
import { ROLE_LABELS } from "../board/members/roles";
import { updatedLabel } from "./updatedLabel";

/** 件数を並べる順（#200）。注釈の 3 状態を、進む順に並べる。 */
const STATE_ORDER: SyncState[] = ["uncreated", "created", "changed"];

type Props = {
  board: BoardListEntry;
  /** 更新時刻を書く基準。一覧を読み込んだ時刻（`BoardList.fetchedAt`）。 */
  now: Date;
  onOpen: (id: string) => void;
};

/**
 * ボード一覧のカード（#200）。**カード全体を押すと** ボードを開く。
 *
 * **ボタンの中身はボード名だけにする。** ロール・更新時刻・状態はボタンの外に
 * 置き、説明として結ぶ。カード全体を押せるのは、ボタンがカードを覆う
 * （`boards.css` の `.board-card-open::after`）ため。中身ごとボタンにして名前を
 * ボード名だけに絞ると、見えている文字が名前に含まれず、見えている文字を
 * 読み上げて押す音声操作で当たらない（WCAG 2.5.3、#311）。全部を名前にすると、
 * 名前で引く読み上げと E2E の両方で長すぎる（注釈のカードと同じ形）。
 */
export function BoardCard({ board, now, onOpen }: Props) {
  const ids = {
    role: `board-card-role-${board.id}`,
    updated: `board-card-updated-${board.id}`,
    states: `board-card-states-${board.id}`,
  };
  // **作成先が未選択でも、開いた先はキャンバス**（ADR 0100）。作成先は描いた
  // あとでボードから選ぶので、カードで先に知らせることは無い。未選択であること
  // 自体は節の見出し（「作成先が未選択」）が言っている。
  const described = [ids.role, ids.updated, ids.states];

  return (
    <li className="board-card">
      <span className="board-card-head">
        <button
          type="button"
          className="board-card-open"
          aria-describedby={described.join(" ")}
          onClick={() => onOpen(board.id)}
        >
          {board.name}
        </button>
        {/*
          自分が何をできるのかは、開く前から見えている（ADR 0017）。共有された
          ボードが並ぶと「開けるが書けない」が普通に起きる。**オーナーのボードにも
          出す**（#200 で決めた）。出し分けると、出ていないことの意味を覚えて
          おく必要がある。
        */}
        <span id={ids.role} className="badge badge-role">
          {ROLE_LABELS[board.role]}
        </span>
      </span>
      <time id={ids.updated} className="board-card-updated" dateTime={board.updatedAt}>
        {updatedLabel(board.updatedAt, now)}
      </time>
      <span id={ids.states} className="board-card-states">
        <StateBadges counts={board.annotationCounts} />
      </span>
    </li>
  );
}

/**
 * 注釈の 3 状態の件数。0 件のものは出さない（#200）。
 *
 * **色だけで伝えない。** バッジには状態の名前を文字で添える（#62）。
 */
function StateBadges({ counts }: { counts: AnnotationCounts | null }) {
  // シーンを読めなかったボード。一覧全体は失敗させず、このボードだけそう書く
  // （#207）。「注釈なし」にすると、読めなかったことが 0 件に化ける。
  if (counts === null) {
    return <span className="hint">絵の状態を読めません</span>;
  }

  const shown = STATE_ORDER.filter((state) => counts[state] > 0);
  if (shown.length === 0) {
    return <span className="badge badge-none">絵なし</span>;
  }
  return (
    <>
      {shown.map((state) => (
        <span key={state} className={`badge badge-${state}`}>
          {STATE_LABEL[state]} {counts[state]}
        </span>
      ))}
    </>
  );
}
