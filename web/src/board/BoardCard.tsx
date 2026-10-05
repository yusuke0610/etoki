import type { AnnotationCounts, BoardListEntry, SyncState } from "../api/types";
import { STATE_LABEL } from "./annotationLabel";
import { ROLE_LABELS } from "./roles";
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
 * ボード一覧のカード（#200）。**カード全体を 1 つのボタンにする。** 押すと
 * ボードを開く。
 *
 * **ボタンの名前はボード名だけにする。** ロール・更新時刻・状態は説明として
 * 結ぶ。全部を名前にすると、名前で引く読み上げと E2E の両方で長すぎる
 * （注釈のカードと同じ形）。
 */
export function BoardCard({ board, now, onOpen }: Props) {
  const ids = {
    name: `board-card-name-${board.id}`,
    role: `board-card-role-${board.id}`,
    updated: `board-card-updated-${board.id}`,
    states: `board-card-states-${board.id}`,
    note: `board-card-note-${board.id}`,
  };
  const unselected = board.projectId === "";
  const described = [
    ids.role,
    ids.updated,
    ids.states,
    ...(unselected ? [ids.note] : []),
  ];

  return (
    <li>
      <button
        type="button"
        // 作成先が未選択のカードは枠を破線にする。開くと作成先の選択から
        // 始まるので、ほかのカードと同じ見た目だと押した先で驚く。
        className={`board-card${unselected ? " unselected" : ""}`}
        aria-labelledby={ids.name}
        aria-describedby={described.join(" ")}
        onClick={() => onOpen(board.id)}
      >
        <span className="board-card-head">
          <span id={ids.name} className="board-card-name">
            {board.name}
          </span>
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
        {unselected && (
          // 作成先を選べるのはオーナーだけ（ADR 0017）。押した先で何が起きるかを
          // 開く前に言う。
          <span id={ids.note} className="hint board-card-note">
            {board.role === "owner"
              ? "開くと作成先を選べます"
              : "作成先はオーナーが選びます"}
          </span>
        )}
      </button>
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
    return <span className="hint">注釈の状態を読めません</span>;
  }

  const shown = STATE_ORDER.filter((state) => counts[state] > 0);
  if (shown.length === 0) {
    return <span className="badge badge-none">注釈なし</span>;
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
