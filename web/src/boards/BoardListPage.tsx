import { useMemo, useRef, useState } from "react";

import type { Failure } from "../api/errorMessage";
import type { BoardListEntry, SessionStatus } from "../api/types";
import { Wordmark } from "../app/Wordmark";
import { UserMenu } from "../auth/UserMenu";
import { ConnectionsDialog } from "../connections/ConnectionsDialog";
import type { TemplateChoice } from "../excalidraw/template";
import { BoardCard } from "./BoardCard";
import { boardSections } from "./grouping";
import { NewBoardDialog } from "./NewBoardDialog";

/**
 * 読み込んだ一覧と、読み込んだ時刻。
 *
 * **時刻は一覧と一緒に持つ。** カードの更新時刻（「2 時間前」）は読み込んだ時点を
 * 基準に書く。描くたびに時計を読むと、同じ一覧が描き直しのたびに違う文になる。
 */
export type BoardList = {
  entries: BoardListEntry[];
  fetchedAt: Date;
};

type Props = {
  /** ログインしている相手。認証を設定していない構成では無い。 */
  user: SessionStatus["user"];
  onLogout: () => void;
  /** まだ読み込んでいなければ null。0 件（空の配列）とは分ける。 */
  boards: BoardList | null;
  onOpen: (id: string) => void;
  /**
   * ボードを作るときと、作成先が未選択のボードを開くときに、作成先を選ぶか。
   * GitHub が未設定と分かっている構成だけ false（ADR 0090）。**まだ確かめて
   * いなければ true**（ADR 0030 の「引けていないうちは止めない」と同じ向きで、
   * いつもの流れに倒す）。
   */
  choosesTarget: boolean;
  /**
   * MCP の接続を扱えない理由（ADR 0076）。扱えるなら、またはまだ確かめて
   * いなければ null（ADR 0030）。
   */
  connectionsUnavailable: string | null;
  /** 新しいボードのダイアログ（`NewBoardDialog`）。開閉と入力は App が `useNewBoardFlow` で持つ。 */
  dialog: {
    open: boolean;
    name: string;
    template: TemplateChoice;
    onOpen: () => void;
    onNameChange: (name: string) => void;
    onTemplateChange: (template: TemplateChoice) => void;
    /**
     * 名前を確定して作成先の選択に進む。ここではまだ作らない。作成先を選ばない
     * 構成（`choosesTarget` が false）では、ここで作る。
     */
    onNext: () => void;
    onCancel: () => void;
    /** 作成先を選ばずに作っているあいだ。 */
    submitting: boolean;
    /** 作成先を選ばずに作れなかった理由。ダイアログの中に出す。 */
    failure: Failure | null;
  };
};

/**
 * ボードの一覧。**ボードとは別の画面にしてある**（ADR 0064）。
 *
 * 同じ画面に置いていたころは、描いているあいだも他のボードと、それが GitHub の
 * どこに属するかが視界に残っていた。ブレスト中に構造を意識させないために
 * （中核思想 1）、ボードを開いたらこの画面ごと外す。
 *
 * **ボードは作成先ごとの節に、カードの格子で並べる**（#200）。カードには更新
 * 時刻と注釈の 3 状態の件数を出す。開く前に、どのボードに手を打つものがあるかが
 * 分かる（中核思想 3）。
 *
 * **状態は App が持つ。** ここは並べて、押されたことを返すだけ。
 */
export function BoardListPage({
  user,
  onLogout,
  boards,
  onOpen,
  choosesTarget,
  connectionsUnavailable,
  dialog,
}: Props) {
  const newBoard = useRef<HTMLButtonElement>(null);
  // 「MCP の接続」のダイアログ。**開閉はここで持つ。** 新しいボードと違って
  // 作成先の選択から戻って開き直す流れが無く、App が持つ理由が無い。
  const [connectionsOpen, setConnectionsOpen] = useState(false);
  // 閉じたら焦点を戻す先。押した「MCP の接続」はしまったメニューの中にあって
  // 見えないので、メニューを開くボタンへ戻す。
  const userMenu = useRef<HTMLDivElement>(null);
  const sections = useMemo(
    () => (boards === null ? [] : boardSections(boards.entries)),
    [boards],
  );

  return (
    <div className="board-list-page">
      {/*
        上の帯の高さは認証の有無で変えない（#200）。右に置くものが無い構成でも、
        見出しの位置が構成ごとに動かないようにする。
      */}
      <header className="list-header">
        {/* ロゴが名前を持つので、見出しの名前は「etoki」のまま（ADR 0084）。 */}
        <h1 className="brand">
          <Wordmark height={26} />
        </h1>
        {user && (
          <div ref={userMenu} className="list-header-user">
            <UserMenu
              user={user}
              onLogout={onLogout}
              onOpenConnections={() => setConnectionsOpen(true)}
              connectionsUnavailable={connectionsUnavailable}
            />
          </div>
        )}
      </header>

      <div className="list-body">
        <div className="list-title">
          <h2>ボード</h2>
          <div className="list-title-action">
            {/*
              **どの構成でも止めない**（ADR 0090）。GitHub が未設定なら作成先を
              選ばずに作る。止めていたころは、LLM を設定しなくてもブレストだけ
              先にできるという約束（ADR 0008）が、GitHub を設定するまで果たされ
              なかった。
            */}
            <button
              ref={newBoard}
              type="button"
              className="primary"
              onClick={dialog.onOpen}
            >
              新しいボード
            </button>
          </div>
        </div>

        {/*
          読み込む前は何も出さない。空の文を出すと、ボードを持っている人にも
          一瞬「まだボードがありません」が見える。
        */}
        {boards !== null && boards.entries.length === 0 && (
          <p className="hint">
            まだボードがありません。「新しいボード」から作成してください。
          </p>
        )}
        {sections.length > 0 && boards !== null && (
          // 一覧は作成先でまとめる（ADR 0019）。作成先はボードの属性なので、
          // 開くまで分からないままだと取り違えたまま作成に進める。
          <div className="board-list">
            {sections.map((section, i) => {
              const headingId = `board-section-${i}`;
              return (
                <section
                  key={section.key}
                  className="board-section"
                  aria-labelledby={headingId}
                >
                  <h3 id={headingId}>{section.heading}</h3>
                  <ul className="board-grid">
                    {section.boards.map((board) => (
                      <BoardCard
                        key={board.id}
                        board={board}
                        now={boards.fetchedAt}
                        choosesTarget={choosesTarget}
                        onOpen={onOpen}
                      />
                    ))}
                  </ul>
                </section>
              );
            })}
          </div>
        )}
      </div>

      <NewBoardDialog
        open={dialog.open}
        name={dialog.name}
        onNameChange={dialog.onNameChange}
        template={dialog.template}
        onTemplateChange={dialog.onTemplateChange}
        choosesTarget={choosesTarget}
        submitting={dialog.submitting}
        failure={dialog.failure}
        onNext={dialog.onNext}
        onCancel={dialog.onCancel}
        // 閉じたら「新しいボード」へ焦点を戻す。作成先の選択から戻って開き
        // 直したときは、開く前の焦点がどこにも無いので、ブラウザに任せると
        // 画面の先頭へ落ちる。
        onClosed={() => newBoard.current?.focus()}
      />
      <ConnectionsDialog
        open={connectionsOpen}
        onClose={() => {
          setConnectionsOpen(false);
          userMenu.current?.querySelector("button")?.focus();
        }}
      />
    </div>
  );
}
