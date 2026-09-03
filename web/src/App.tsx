import "@excalidraw/excalidraw/index.css";

import { useCallback, useEffect, useRef, useState } from "react";

import { ApiError, authApi, boardsApi, capabilitiesApi } from "./api/boards";
import { describeFailure, type Failure } from "./api/errorMessage";
import type {
  BoardDetail,
  BoardSummary,
  BoardTarget,
  Capabilities,
  SessionStatus,
} from "./api/types";
import { LoginPage } from "./auth/LoginPage";
import { ErrorNotice } from "./ErrorNotice";
import { BoardPage } from "./board/BoardPage";
import { BoardTree } from "./board/BoardTree";
import { RepositoryPicker } from "./board/RepositoryPicker";

export function App() {
  const [boards, setBoards] = useState<BoardSummary[]>([]);
  const [current, setCurrent] = useState<BoardDetail | null>(null);
  const [error, setError] = useState<Failure | null>(null);
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
  // いま使える機能。**null は「まだ確かめていない」。**
  //
  // LLM や GitHub を設定しなくても etoki は起動する（ADR 0008）。設定していない
  // 機能は押した後に 503 で返るだけなので、押す前に見せるために引く。
  const [capabilities, setCapabilities] = useState<Capabilities | null>(null);
  // 開いているボードに未保存の変更があるか。BoardPage から上がってくる。
  //
  // キャンバスから離れる導線はこちら側にあるので、判断の材料をここに置く。
  // **state ではなく ref で持つ。** 描くたびに再描画する必要が無いのに加えて、
  // state だと通信の待ちを挟んだ判定が、待ち始めた時点の値を見てしまう。
  const unsaved = useRef(false);

  useEffect(() => {
    void (async () => {
      try {
        setSession(await authApi.session());
      } catch (e) {
        // 状態が分からないなら、ログインを求めない側に倒す。求める側に倒すと、
        // 認証を設定していない構成が API の一時的な失敗で使えなくなる。
        setError(describeFailure("ログイン状態を取得できませんでした", e));
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
      if (e instanceof ApiError && e.code === "login_required") {
        // 読み直しにも失敗したら、初回と同じ側に倒す。ここで投げると、
        // 呼び出し側は void reload() なので誰も受けず、画面はログイン済みの
        // ままボード一覧だけが空という、戻れない状態で止まる。
        try {
          setSession(await authApi.session());
        } catch (sessionError) {
          setError(describeFailure("ログイン状態を取得できませんでした", sessionError));
          setSession({ authRequired: false, authenticated: false });
        }
        return;
      }
      setError(describeFailure("ボード一覧を取得できませんでした", e));
    }
  }, []);

  // ログインが要る構成では、済むまで読みにいかない。先に叩くと 401 が
  // エラー表示に出て、ログイン画面の上に無関係な失敗が重なる。
  const signedIn = session !== null && (!session.authRequired || session.authenticated);

  useEffect(() => {
    if (!signedIn) return;
    // 一覧は開いた時点で要る。読みにいくのは await の後で state を置く非同期
    // 関数なので描画の連鎖は起きないが、規則が見ているのは effect から
    // setState を含む関数を呼ぶこと自体なので、ここは外す。
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void reload();

    void (async () => {
      try {
        setCapabilities(await capabilitiesApi.get());
      } catch {
        // 引けなくても止めない。null のままなら押させる側に倒れ、これまで
        // どおり押した後に 503 の理由が出る。**確かめられなかったことを
        // 「使えない」として見せない**（中核思想 3）。
      }
    })();
  }, [reload, signedIn]);

  /**
   * 未保存かどうかを覚える。BoardPage から呼ばれる。
   *
   * 同じ関数を渡し続ける必要がある。作り直すと、BoardPage 側の登録し直しが
   * 走って「外れたので下ろす」が誤って動く。
   */
  const handleDirtyChange = useCallback((dirty: boolean) => {
    unsaved.current = dirty;
  }, []);

  /**
   * 未保存の変更を捨ててよいかを確かめる。捨てないなら false。
   *
   * **確認とキャンバスを外すことのあいだに待ちを挟まない。** 挟むと、待って
   * いるあいだに描き足したぶんを確認なしで捨てる。JS は 1 本なので、間に
   * await が無ければ描き込む隙も無い。
   *
   * 保存はしない。自動で保存すると、捨てるつもりの試し描きまで残る
   * （中核思想 3）。
   */
  const confirmDiscard = useCallback(() => {
    if (!unsaved.current) return true;
    return window.confirm(
      "未保存の変更があります。このまま移動すると、描いた内容は失われます。",
    );
  }, []);

  const logout = useCallback(async () => {
    // ログアウトもキャンバスを外す。押した理由が何であれ、消えるものは同じ。
    if (!confirmDiscard()) return;

    // 捨ててよいと言われたので、通信を待たずにここで外す。待ってから外すと、
    // 待っているあいだの描き足しを確認なしで捨てることになる。
    setCurrent(null);
    setBoards([]);

    try {
      await authApi.logout();
      // 状態は作り直さず読み直す。手元で組み立てるとサーバーの見方とずれうる。
      setSession(await authApi.session());
    } catch (e) {
      setError(describeFailure("ログアウトできませんでした", e));
      // 失敗したらログインしたまま。一覧を空のままにすると、何も操作できない
      // 画面が残る。
      await reload();
    }
  }, [confirmDiscard, reload]);

  const open = useCallback(
    async (id: string) => {
      // **取ってから訊く。** 訊いてから取ると、取っているあいだに描き足せて
      // しまい、そのぶんを確認なしで捨てる。取得が失敗したときに、捨てるか
      // どうかを訊いてしまうことも無くなる。
      let next: BoardDetail;
      try {
        next = await boardsApi.get(id);
      } catch (e) {
        setError(describeFailure("ボードを開けませんでした", e));
        return;
      }

      // ここから先に待ちは無い。切り替えると Excalidraw ごと作り直すので、
      // 未保存の編集はその場で消える。
      if (!confirmDiscard()) return;

      setPicking(false);
      setCreating(null);
      setCurrent(next);
    },
    [confirmDiscard],
  );

  /** 名前を確定して、作成先の選択に進む。ここではまだ作らない。 */
  const startCreating = useCallback(() => {
    if (!name.trim()) return;
    // 作成先の選択画面に移ると、開いていたボードのキャンバスが外れる。
    // 「作成先を変更」を dirty で止めてあるのと揃える。
    if (!confirmDiscard()) return;

    setCurrent(null);
    setPicking(false);
    setCreating(name.trim());
  }, [confirmDiscard, name]);

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

  /**
   * サーバーが返したボードで手元を差し替える。
   *
   * **開いているのが応答のボードのときだけ差し替える。** 取り直しも改名も
   * 通信を挟むので、そのあいだにボードを切り替えられる。照合せずに入れると、
   * 遅れて届いた応答が今のボードを外し、確認（confirmDiscard）を通さずに
   * 未保存の編集を捨てることになる。
   *
   * **一覧も引き直す。** 木は作成先でまとめて見せる（ADR 0019）ので、
   * 名前が古いままでは差し替えた意味が無い。
   */
  const replaceBoard = useCallback(
    (board: BoardDetail) => {
      setCurrent((shown) => (shown?.id === board.id ? board : shown));
      void reload();
    },
    [reload],
  );

  /**
   * 削除されたので手元から外す。
   *
   * **未保存の確認は通さない。** 消えたのはボードそのもので、捨てるか
   * どうかを訊いても戻せる先が無い。確認は削除の手前で済んでいる（ADR 0042）。
   * `unsaved` は BoardPage が外れるときに自分で下ろす。
   *
   * **一覧も引き直す。** 木は作成先でまとめて見せる（ADR 0019）ので、消えた
   * ボードが残っていると開けない行が並ぶ。
   *
   * **引き直しを待たずに手元からも外す。** `reload()` が失敗しても一覧は
   * 前の値のまま残るので、引き直しだけに任せると開くと 404 になる行が
   * 並び続ける。消えたことはサーバーの応答で確かめてあるので、ここは
   * 推測ではない。引き直しは他の変化を拾うために続けて行う。
   */
  const handleDeleted = useCallback(
    (id: string) => {
      setBoards((shown) => shown.filter((b) => b.id !== id));
      setCurrent(null);
      void reload();
    },
    [reload],
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
        {error && <ErrorNotice failure={error} onClose={() => setError(null)} />}

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
            capabilities={capabilities}
            onError={setError}
            onChangeTarget={() => setPicking(true)}
            // 表示名の取り直しと改名は、どちらも「サーバーが返したボードで
            // 手元を差し替える」だけ。同じ扱いにする（replaceBoard を参照）。
            onTargetRefreshed={replaceBoard}
            onRenamed={replaceBoard}
            onDeleted={handleDeleted}
            onDirtyChange={handleDirtyChange}
          />
        )}
      </main>
    </div>
  );
}
