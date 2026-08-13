import "@excalidraw/excalidraw/index.css";

import { useCallback, useEffect, useState } from "react";

import { ApiError, authApi, boardsApi } from "./api/boards";
import type { BoardDetail, BoardSummary, BoardTarget, SessionStatus } from "./api/types";
import { LoginPage } from "./auth/LoginPage";
import { BoardPage } from "./board/BoardPage";
import { BoardTree } from "./board/BoardTree";
import { RepositoryPicker } from "./board/RepositoryPicker";

export function App() {
  const [boards, setBoards] = useState<BoardSummary[]>([]);
  const [current, setCurrent] = useState<BoardDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState("");
  // 作成先を選び直している最中かどうか。未選択のボードでは常に選ばせる。
  const [picking, setPicking] = useState(false);
  // 作成しようとしているボードの名前。null なら作成中ではない。
  //
  // **作成先はボードを作る前に選ばせる**（ADR 0017）。書ける Project を持たない
  // 人はここで先へ進めず、それが「作成にはリポジトリへのアクセス権が要る」
  // ことの表れになる。
  const [creating, setCreating] = useState<string | null>(null);
  // ログイン状態。null は問い合わせ中。
  const [session, setSession] = useState<SessionStatus | null>(null);

  useEffect(() => {
    void (async () => {
      try {
        setSession(await authApi.session());
      } catch (e) {
        // 状態が分からないなら、ログインを求めない側に倒す。求める側に倒すと、
        // 認証を設定していない構成が API の一時的な失敗で使えなくなる。
        setError(`ログイン状態を取得できませんでした: ${String(e)}`);
        setSession({ authRequired: false, authenticated: false });
      }
    })();
  }, []);

  const reload = useCallback(async () => {
    try {
      setBoards(await boardsApi.list());
    } catch (e) {
      // 使っている最中の失効はここで初めて分かる。エラーだけ出すと、画面は
      // ログイン済みのまま何も操作できず、リロードするまで戻れない。
      // 状態を読み直せばログイン画面に落ちる。
      if (e instanceof ApiError && e.status === 401) {
        setSession(await authApi.session());
        return;
      }
      setError(`ボード一覧を取得できませんでした: ${String(e)}`);
    }
  }, []);

  // ログインが要る構成では、済むまで読みにいかない。先に叩くと 401 が
  // エラー表示に出て、ログイン画面の上に無関係な失敗が重なる。
  const signedIn = session !== null && (!session.authRequired || session.authenticated);

  useEffect(() => {
    if (!signedIn) return;
    void reload();
  }, [reload, signedIn]);

  const logout = useCallback(async () => {
    try {
      await authApi.logout();
      // 状態は作り直さず読み直す。手元で組み立てるとサーバーの見方とずれうる。
      setSession(await authApi.session());
      setCurrent(null);
      setBoards([]);
    } catch (e) {
      setError(`ログアウトできませんでした: ${String(e)}`);
    }
  }, []);

  const open = useCallback(async (id: string) => {
    try {
      setPicking(false);
      setCreating(null);
      setCurrent(await boardsApi.get(id));
    } catch (e) {
      setError(`ボードを開けませんでした: ${String(e)}`);
    }
  }, []);

  /** 名前を確定して、作成先の選択に進む。ここではまだ作らない。 */
  const startCreating = useCallback(() => {
    if (!name.trim()) return;
    setCurrent(null);
    setPicking(false);
    setCreating(name.trim());
  }, [name]);

  /** 作成先が決まったのでボードを作る。失敗は picker が表示する。 */
  const createWithTarget = useCallback(
    async (target: BoardTarget) => {
      if (creating === null) return;

      const board = await boardsApi.create(creating, target);
      setName("");
      setCreating(null);
      await reload();
      setCurrent(board);
    },
    [creating, reload],
  );

  /** 既存ボードの作成先を選び直す。最初の作成より前だけ通る（ADR 0014）。 */
  const changeTarget = useCallback(
    async (target: BoardTarget) => {
      if (current === null) return;

      const board = await boardsApi.setTarget(current.id, target);
      setPicking(false);
      setCurrent(board);
    },
    [current],
  );

  // 問い合わせ中は何も出さない。ログイン画面を一瞬見せてから消すと、
  // 認証を設定していない構成でもちらつく。
  if (session === null) {
    return <div className="app" />;
  }

  if (session.authRequired && !session.authenticated) {
    return (
      <div className="app">
        <main className="main">
          <LoginPage />
        </main>
      </div>
    );
  }

  return (
    <div className="app">
      <nav className="sidebar">
        <h1 className="brand">etoki</h1>

        {session.user && (
          <div className="account">
            <span className="account-name">{session.user.displayName}</span>
            <button type="button" onClick={() => void logout()}>
              ログアウト
            </button>
          </div>
        )}

        <form
          className="create-board"
          onSubmit={(e) => {
            e.preventDefault();
            startCreating();
          }}
        >
          <input
            aria-label="ボード名"
            placeholder="新しいボード名"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <button type="submit" disabled={!name.trim()}>
            次へ
          </button>
        </form>

        {/*
          一覧はリポジトリと Project でまとめる（ADR 0019）。作成先はボードの
          属性なので、開くまで分からないままだと取り違えたまま作成に進める。
        */}
        <BoardTree
          boards={boards}
          currentId={current?.id ?? null}
          onOpen={(id) => void open(id)}
        />
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
        {creating !== null ? (
          <RepositoryPicker
            key="creating"
            title={creating}
            onSelected={createWithTarget}
            onCancel={() => setCreating(null)}
          />
        ) : current === null ? (
          <p className="hint">左からボードを選ぶか、新しく作成してください。</p>
        ) : picking || current.projectId === "" ? (
          <RepositoryPicker
            key={current.id}
            title={current.name}
            onSelected={changeTarget}
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
