import "@excalidraw/excalidraw/index.css";

import { useCallback, useEffect, useState } from "react";

import { boardsApi } from "./api/boards";
import type { BoardDetail, BoardSummary } from "./api/types";
import { BoardPage } from "./board/BoardPage";
import { RepositoryPicker } from "./board/RepositoryPicker";

export function App() {
  const [boards, setBoards] = useState<BoardSummary[]>([]);
  const [current, setCurrent] = useState<BoardDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState("");
  // 作成先を選び直している最中かどうか。未選択のボードでは常に選ばせる。
  const [picking, setPicking] = useState(false);

  const reload = useCallback(async () => {
    try {
      setBoards(await boardsApi.list());
    } catch (e) {
      setError(`ボード一覧を取得できませんでした: ${String(e)}`);
    }
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  const open = useCallback(async (id: string) => {
    try {
      setPicking(false);
      setCurrent(await boardsApi.get(id));
    } catch (e) {
      setError(`ボードを開けませんでした: ${String(e)}`);
    }
  }, []);

  const create = useCallback(async () => {
    if (!name.trim()) return;
    try {
      const board = await boardsApi.create(name.trim());
      setName("");
      await reload();
      setPicking(false);
      setCurrent(board);
    } catch (e) {
      setError(`ボードを作成できませんでした: ${String(e)}`);
    }
  }, [name, reload]);

  /** 作成先が決まったら、その姿でボードを差し替えてブレストに進む。 */
  const targetSelected = useCallback((board: BoardDetail) => {
    setPicking(false);
    setCurrent(board);
  }, []);

  return (
    <div className="app">
      <nav className="sidebar">
        <h1 className="brand">etoki</h1>

        <form
          className="create-board"
          onSubmit={(e) => {
            e.preventDefault();
            void create();
          }}
        >
          <input
            aria-label="ボード名"
            placeholder="新しいボード名"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <button type="submit" disabled={!name.trim()}>
            作成
          </button>
        </form>

        <ul className="board-list">
          {boards.map((b) => (
            <li key={b.id}>
              <button
                type="button"
                className={b.id === current?.id ? "active" : ""}
                onClick={() => void open(b.id)}
              >
                {b.name}
              </button>
            </li>
          ))}
        </ul>
      </nav>

      <main className="main">
        {error && (
          <div className="error" role="alert">
            {error}
            <button type="button" onClick={() => setError(null)}>
              閉じる
            </button>
          </div>
        )}

        {/*
          「ボードに入る → 対象リポジトリ選択 → ブレスト開始」の分岐はここに置く。
          BoardPage の中ではなく手前で切ることで、作成先が決まるまでキャンバスを
          出さないという要求がそのまま形になる。
        */}
        {current === null ? (
          <p className="hint">左からボードを選ぶか、新しく作成してください。</p>
        ) : picking || current.projectId === "" ? (
          <RepositoryPicker
            key={current.id}
            board={current}
            onSelected={targetSelected}
            // 未選択のうちは引き返す先が無い。選び直しのときだけ戻れる。
            onCancel={picking ? () => setPicking(false) : undefined}
          />
        ) : (
          // ボードを切り替えたら Excalidraw ごと作り直す。initialData は
          // マウント時にしか読まれないため、key を変えないと前のシーンが残る。
          <BoardPage
            key={current.id}
            board={current}
            onError={setError}
            onChangeTarget={() => setPicking(true)}
          />
        )}
      </main>
    </div>
  );
}
