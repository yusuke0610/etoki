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
import { createGenerations } from "./board/generation";
import { RepositoryPicker } from "./board/RepositoryPicker";
import { TemplatePicker } from "./board/TemplatePicker";
import {
  BLANK_TEMPLATE,
  templateScene,
  type TemplateChoice,
} from "./excalidraw/template";
import {
  boardLocationUrl,
  NO_BOARD,
  parseBoardLocation,
  type BoardLocation,
} from "./location";
import { useTheme } from "./theme";

/**
 * ボードを開く要求の世代のキー。
 *
 * 守る対象が 1 つなのでキーも 1 つ。**それでも `createGenerations` を使う**のは、
 * 「世代で守る」の実装をリポジトリに 2 つ持たないため。
 */
const OPENING = "board";

/** 履歴の積み方。押した結果として戻れるべきものだけを積む。 */
type HistoryMode = "push" | "replace";

/** `open` の任意引数。 */
type OpenOptions = {
  /** 履歴の積み方。既定は積む。 */
  mode?: HistoryMode;
  /** 作成先の選択画面から始めるか。既定は始めない。 */
  picking?: boolean;
};

/**
 * 作成先の選び直しを始められるボードか。
 *
 * **条件は `BoardPage` が「作成先を変更」を出す条件と同じ**（owner だけ、
 * 固定前だけ。ADR 0014 / 0017）。URL から入る口だけを緩めると、画面から
 * 押せないはずの操作にそこからだけ入れてしまい、進んだ先で 403 / 409 を
 * 受け取る（中核思想 3）。**ずれたことは `web/e2e/url.spec.ts` が落とす。**
 */
function canOpenTargetPicker(board: BoardDetail): boolean {
  return board.role === "owner" && !board.targetLocked;
}

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
  // 何から始めるか。**既定は空白**（中核思想 3）。テンプレートは選ばせるもので、
  // 勝手に適用しない。
  const [template, setTemplate] = useState<TemplateChoice>(BLANK_TEMPLATE);
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
  // URL に書いてあったボードを開きにいったかどうか（ADR 0059）。
  //
  // **1 度きり。** ログイン直後に 1 回だけ読み、以後は URL を書く側に回る。
  // 毎回読み直すと、切り替えたあとの `boards` の取り直しで URL のボードへ
  // 引き戻される。**state ではなく ref。** 描画には出ないうえ、StrictMode の
  // effect の二重実行で 2 回開きにいくのを止めたい。
  const openedFromUrl = useRef(false);
  // いま画面に出ている場所。URL を書き戻すときの基準になる。
  //
  // **state ではなく ref。** 戻る / 進むを拒んだときの書き戻しは popstate の
  // 中で行うので、購読を貼り直さずに最新の値を読めなければならない。state に
  // すると、値が変わるたびに購読を貼り直すことになる。
  const shown = useRef<BoardLocation>(NO_BOARD);
  // ボードを開く要求の世代（`.claude/rules/async-ui.md`）。
  //
  // **開く導線が 2 つある**（サイドバーと戻る / 進む）ので、続けて操作されると
  // 取得が並走する。古い応答を反映すると、押した順と違うボードが開き、URL も
  // そちらを指したまま残る。**いま画面に出ているものとの ID 照合では足りない。**
  // 照合したい相手は「最後に要求されたもの」で、それはまだ画面に出ていない。
  //
  // 初期化関数で 1 度だけ作る（`BoardPage` の世代と同じ形）。
  const [openings] = useState(createGenerations);
  // 配色。**持つのはここだけ**で、キャンバスのメニューで切り替えても BoardPage
  // から戻ってくる（ADR 0055）。ログインや作成先の選択の画面にも効かせるため、
  // ボードより上に置く。
  const [theme, setTheme] = useTheme();

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

  /**
   * URL を画面に合わせて書き換える（ADR 0059）。
   *
   * **state から URL を導く effect は置かない。** effect では「積むのか置き換え
   * るのか」を区別できないうえ、戻る / 進むで URL が先に動いたときに書き戻しと
   * 競合する。導線ごとにここを呼ぶ形は、未保存の確認（`confirmDiscard`）を
   * 導線ごとに通しているのと同じ（`web/CLAUDE.md`）。**導線を足したらここにも
   * 足す。**
   *
   * `shown` も一緒に更新する。**URL と控えを 1 箇所で動かす**ことで、2 つが
   * ずれた状態を作らない。
   */
  const showLocation = useCallback((location: BoardLocation, mode: HistoryMode) => {
    shown.current = location;
    const url = boardLocationUrl(location);
    if (mode === "push") {
      window.history.pushState(null, "", url);
    } else {
      window.history.replaceState(null, "", url);
    }
  }, []);

  /**
   * ボードを取ってくる。取れなければ null。
   *
   * **失敗したら URL をいま出ているものに合わせ直す。** 開けなかったボードを
   * URL に残すと、読み込み直すたびに同じ失敗を繰り返す。サイドバーから開こう
   * として失敗したときは、開いたままのボードの URL に戻る。
   *
   * 非メンバーにも消えたボードにも同じ `not_found` が返る（ADR 0016 / 0017）。
   * **URL から開いたときも見せ方を変えない。** 変えると、返ってきた画面の違いから
   * ボードの存在を確かめられる。
   */
  const loadBoard = useCallback(
    async (id: string): Promise<BoardDetail | null> => {
      const generation = openings.start(OPENING);
      try {
        const board = await boardsApi.get(id);
        // 追い越されていたら捨てる。あとから来た要求が正解。
        return openings.isCurrent(OPENING, generation) ? board : null;
      } catch (e) {
        // **失敗の反映も世代で守る。** 守らないと、追い越された取得の失敗が
        // 新しいボードの上にエラーを出し、URL まで巻き戻す。
        if (!openings.isCurrent(OPENING, generation)) return null;

        setError(describeFailure("ボードを開けませんでした", e));
        showLocation(shown.current, "replace");
        return null;
      }
    },
    [openings, showLocation],
  );

  const logout = useCallback(async () => {
    // ログアウトもキャンバスを外す。押した理由が何であれ、消えるものは同じ。
    if (!confirmDiscard()) return;

    // 捨ててよいと言われたので、通信を待たずにここで外す。待ってから外すと、
    // 待っているあいだの描き足しを確認なしで捨てることになる。
    setCurrent(null);
    setBoards([]);
    // 走っている取得を無効にする。**対象が変わるイベントは関連する世代を
    // 全部無効にする**（`.claude/rules/async-ui.md`）。React state を消しても
    // 進行中のリクエストは止まらないので、遅れて着いた応答がログイン画面の
    // 裏でボードを開き直す。
    openings.invalidateAll();
    // ログイン画面に残るのは URL だけ。**積まずに置き換える。** 積むと
    // 「戻る」でログアウト前の URL に戻れてしまい、画面と食い違う。
    showLocation(NO_BOARD, "replace");

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
  }, [confirmDiscard, openings, reload, showLocation]);

  const open = useCallback(
    async (id: string, options: OpenOptions = {}) => {
      const { mode = "push", picking: wanted = false } = options;

      // **取ってから訊く。** 訊いてから取ると、取っているあいだに描き足せて
      // しまい、そのぶんを確認なしで捨てる。取得が失敗したときに、捨てるか
      // どうかを訊いてしまうことも無くなる。
      const next = await loadBoard(id);
      if (next === null) return;

      // ここから先に待ちは無い。切り替えると Excalidraw ごと作り直すので、
      // 未保存の編集はその場で消える。
      if (!confirmDiscard()) return;

      const picking = wanted && canOpenTargetPicker(next);
      setPicking(picking);
      setCreating(null);
      setCurrent(next);
      showLocation({ boardId: next.id, picking }, mode);
    },
    [confirmDiscard, loadBoard, showLocation],
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
    // **作成中は URL に載せない。** 載る材料（名前とひな形）が URL に無いので、
    // 載せても読み込み直した先で復元できない。**積まずに置き換える**のは、
    // 開いていたボードをここで外すのと形を揃えるため。引き返す先はもともと
    // 無い（`onCancel` は案内文の画面に落ちる）。
    showLocation(NO_BOARD, "replace");
  }, [confirmDiscard, name, showLocation]);

  /** 作成先が決まったのでボードを作る。失敗は picker が表示する。 */
  const createWithTarget = useCallback(
    async (target: BoardTarget) => {
      if (creating === null) return;

      // シーンを組み立てるのは押されたこの時点。選んだ時点で作ると、作成先を
      // 選ばずに引き返した回数だけ使わないシーンを持つことになる。
      const board = await boardsApi.create(creating, target, templateScene(template));
      setName("");
      setTemplate(BLANK_TEMPLATE);
      setCreating(null);
      await reload();
      // 走っている取得を無効にする（ログアウトと同じ理由）。作ったボードを
      // 開いた直後に、前のボードの応答が着いて上書きするのを止める。
      openings.invalidateAll();
      setCurrent(board);
      // 作ったボードを開いた状態。**積む。** 「戻る」で作成の手前に戻れる。
      showLocation({ boardId: board.id, picking: false }, "push");
    },
    [creating, openings, reload, showLocation, template],
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
      setCurrent((open) => (open?.id === board.id ? board : open));
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
      setBoards((listed) => listed.filter((b) => b.id !== id));
      setCurrent(null);
      // 走っている取得を無効にする（ログアウトと同じ理由）。
      openings.invalidateAll();
      // 消えたボードを URL に残さない。**積まずに置き換える。** 積むと、
      // いま見えている URL が消えたボードを指したまま 1 つ増える。
      showLocation(NO_BOARD, "replace");
      void reload();
    },
    [openings, reload, showLocation],
  );

  /** 既存ボードの作成先を選び直す。最初の作成より前だけ通る（ADR 0014）。 */
  const changeTarget = useCallback(
    async (target: BoardTarget) => {
      if (current === null) return;

      const board = await boardsApi.setTarget(current.id, target);
      setPicking(false);
      setCurrent(board);
      showLocation({ boardId: board.id, picking: false }, "replace");
    },
    [current, showLocation],
  );

  /**
   * URL に書いてあったボードを開く（ADR 0059）。ログインが済んでから 1 度だけ。
   *
   * **履歴は積まない。** 起動時に積むと、最初の「戻る」が etoki の中に留まり、
   * 来た場所へ戻れない。
   */
  useEffect(() => {
    if (!signedIn || openedFromUrl.current) return;
    openedFromUrl.current = true;

    const initial = parseBoardLocation(window.location.search);
    if (initial.boardId === null) {
      // 開いていない状態も URL に映しておく。`picking` だけが載った URL を
      // そのまま残すと、控えと URL が最初からずれる。
      showLocation(NO_BOARD, "replace");
      return;
    }

    // 読みにいくのは await の後で state を置く非同期関数なので描画の連鎖は
    // 起きないが、規則が見ているのは effect から setState を含む関数を呼ぶこと
    // 自体なので、ここは外す（`reload` と同じ）。
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void open(initial.boardId, { mode: "replace", picking: initial.picking });
  }, [open, showLocation, signedIn]);

  /**
   * 戻る / 進むに追随する（ADR 0059）。
   *
   * **未保存の確認をここでも通す。** キャンバスが外れる導線が 1 つ増えたので、
   * `web/CLAUDE.md` の約束をそのまま掛ける。
   */
  useEffect(() => {
    if (!signedIn) return;

    const handlePopState = () => {
      const next = parseBoardLocation(window.location.search);
      const here = shown.current;
      if (next.boardId === here.boardId && next.picking === here.picking) return;

      void (async () => {
        // **取ってから訊く。** 訊いてから取ると、取っているあいだの描き足しを
        // 確認なしで捨てる（`open` と同じ理由）。
        const board = next.boardId === null ? null : await loadBoard(next.boardId);
        if (next.boardId !== null && board === null) return;

        if (!confirmDiscard()) {
          // **戻る / 進むはアプリ側で止められない。** 捨てないと決めた以上、
          // できるのは見えている場所を積み直して URL を画面に合わせることまで。
          // **戻す（`history.back()`）のではなく積む。** 何手ぶん動かされたのかを
          // 知る手立てが無いので、戻す量を決められない。
          showLocation(here, "push");
          return;
        }

        setCreating(null);
        if (board === null) {
          // **ここは `loadBoard` を通らないので、世代が進まない。** サイドバーで
          // 始めた取得が走っていると、遅れて着いた応答が「離れたはずのボード」を
          // 開き直し、URL まで積む。`logout` と同じ規則で、対象が変わる時点で
          // 関連する世代を全部無効にする（`.claude/rules/async-ui.md`）。
          openings.invalidateAll();
          setCurrent(null);
          setPicking(false);
          showLocation(NO_BOARD, "replace");
          return;
        }

        const picking = next.picking && canOpenTargetPicker(board);
        setCurrent(board);
        setPicking(picking);
        // URL はブラウザがもう動かしている。**それでも書き直す。** 控えを
        // 揃えるのと、通らなかった `picking` を落とすため。
        showLocation({ boardId: board.id, picking }, "replace");
      })();
    };

    window.addEventListener("popstate", handlePopState);
    return () => window.removeEventListener("popstate", handlePopState);
  }, [confirmDiscard, loadBoard, openings, showLocation, signedIn]);

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
          {/*
            何から始めるかをここで選ばせる（#52）。**画面を 1 枚増やさない。**
            増やすと、空白で始めたい人にも通り抜けるだけの手順が要る。
          */}
          <TemplatePicker value={template} onChange={setTemplate} />
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
            onCancel={
              picking
                ? () => {
                    setPicking(false);
                    showLocation({ boardId: current.id, picking: false }, "replace");
                  }
                : undefined
            }
          />
        ) : (
          // ボードを切り替えたら Excalidraw ごと作り直す。initialData は
          // マウント時にしか読まれないため、key を変えないと前のシーンが残る。
          <BoardPage
            key={current.id}
            board={current}
            capabilities={capabilities}
            onError={setError}
            // **選び直しは URL に載せるが、履歴には積まない**（ADR 0059）。
            // 同じボードの中のモードなので、読み込み直しで戻せれば足りる。
            // 積むと、選び終えた後の「戻る」が選択画面に引き返す。
            onChangeTarget={() => {
              setPicking(true);
              showLocation({ boardId: current.id, picking: true }, "replace");
            }}
            // 表示名の取り直しと改名は、どちらも「サーバーが返したボードで
            // 手元を差し替える」だけ。同じ扱いにする（replaceBoard を参照）。
            onTargetRefreshed={replaceBoard}
            onRenamed={replaceBoard}
            onDeleted={handleDeleted}
            onDirtyChange={handleDirtyChange}
            theme={theme}
            onThemeChange={setTheme}
          />
        )}
      </main>
    </div>
  );
}
