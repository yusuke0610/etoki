import type { BoardSummary, SessionStatus } from "../api/types";
import type { TemplateChoice } from "../excalidraw/template";
import { BoardTree } from "./BoardTree";
import { TemplatePicker } from "./TemplatePicker";

type Props = {
  /** ログインしている相手。認証を設定していない構成では無い。 */
  user: SessionStatus["user"];
  onLogout: () => void;
  boards: BoardSummary[];
  onOpen: (id: string) => void;
  /** 新しく作るボードの名前。確定するまで App が持つ。 */
  name: string;
  onNameChange: (name: string) => void;
  template: TemplateChoice;
  onTemplateChange: (template: TemplateChoice) => void;
  /** 名前を確定して作成先の選択に進む。ここではまだ作らない。 */
  onCreate: () => void;
};

/**
 * ボードの一覧。**ボードとは別の画面にしてある**（ADR 0064）。
 *
 * 同じ画面に置いていたころは、描いているあいだも他のボードと、それが GitHub の
 * どこに属するかが視界に残っていた。ブレスト中に構造を意識させないために
 * （中核思想 1）、ボードを開いたらこの画面ごと外す。
 *
 * **状態は App が持つ。** ここは並べて、押されたことを返すだけ。作成の途中の
 * 名前やひな形も App に置くのは、作成先の選択（別の画面）に進んでから引き
 * 返しても消さないため。
 */
export function BoardListPage({
  user,
  onLogout,
  boards,
  onOpen,
  name,
  onNameChange,
  template,
  onTemplateChange,
  onCreate,
}: Props) {
  return (
    <div className="board-list-page">
      <header className="list-header">
        <h1 className="brand">etoki</h1>

        {user && (
          <div className="account">
            <span className="account-name">{user.displayName}</span>
            <button type="button" onClick={onLogout}>
              ログアウト
            </button>
          </div>
        )}
      </header>

      <div className="list-body">
        <section className="list-section" aria-labelledby="create-board-heading">
          <h2 id="create-board-heading">新しいボード</h2>
          <form
            className="create-board"
            onSubmit={(e) => {
              e.preventDefault();
              onCreate();
            }}
          >
            <input
              aria-label="ボード名"
              placeholder="新しいボード名"
              value={name}
              onChange={(e) => onNameChange(e.target.value)}
            />
            {/*
              何から始めるかをここで選ばせる（#52）。**画面を 1 枚増やさない。**
              増やすと、空白で始めたい人にも通り抜けるだけの手順が要る。
            */}
            <TemplatePicker value={template} onChange={onTemplateChange} />
            <button type="submit" className="primary" disabled={!name.trim()}>
              次へ
            </button>
          </form>
        </section>

        <section className="list-section" aria-labelledby="boards-heading">
          <h2 id="boards-heading">ボード</h2>
          {boards.length === 0 ? (
            <p className="hint">
              まだボードがありません。上で名前を付けて作成してください。
            </p>
          ) : (
            // 一覧はリポジトリと Project でまとめる（ADR 0019）。作成先はボードの
            // 属性なので、開くまで分からないままだと取り違えたまま作成に進める。
            <BoardTree boards={boards} onOpen={onOpen} />
          )}
        </section>
      </div>
    </div>
  );
}
