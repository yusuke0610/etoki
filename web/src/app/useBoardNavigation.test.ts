import { act, renderHook, waitFor } from "@testing-library/react";
import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
  type MockInstance,
} from "vitest";

import { ApiError, authApi, boardsApi } from "../api/boards";
import type {
  BoardDetail,
  BoardTarget,
  BoardWithFiles,
  SessionStatus,
} from "../api/types";
import type { NewBoardFlow } from "../boards/useNewBoardFlow";
import { useBoardNavigation } from "./useBoardNavigation";

/**
 * ボードを画面に出す導線（#347 で `App` から切り出し）。
 *
 * **守りたいのは 2 つ。** 1 つは、未保存の確認を通さずにキャンバスを外さない
 * こと（ADR 0021）。もう 1 つは、追い越された応答で、押した順と違うボードを
 * 開かないこと（`.claude/rules/async-ui.md`）。どちらも画面の上では一瞬の
 * 競合で、E2E（`web/e2e/url.spec.ts`）では届く順を決めきれないものをここで
 * 押さえる。
 *
 * 履歴を積んだか置き換えたかは `history.length` の増え方で見る。積むと 1 増え、
 * 置き換えると増えない。
 */

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const target: BoardTarget = {
  repositoryOwner: "acme",
  repositoryName: "web",
  projectId: "PVT_1",
  projectNumber: 1,
  projectTitle: "ロードマップ",
  projectUrl: "https://github.com/orgs/acme/projects/1",
};

function detail(id: string, patch: Partial<BoardDetail> = {}): BoardDetail {
  return {
    id,
    name: id,
    role: "owner",
    createdAt: "2026-10-01T00:00:00Z",
    updatedAt: "2026-10-07T00:00:00Z",
    repositoryOwner: "acme",
    repositoryName: "web",
    projectId: "PVT_1",
    projectNumber: 1,
    projectTitle: "ロードマップ",
    projectUrl: "https://github.com/orgs/acme/projects/1",
    scene: "{}",
    premise: "",
    targetLocked: false,
    sceneOverLimit: false,
    ...patch,
  };
}

function board(id: string, patch: Partial<BoardWithFiles> = {}): BoardWithFiles {
  return { ...detail(id), files: {}, ...patch };
}

const boom = new ApiError(500, "test_unknown", "boom");

/** いまの URL（パスとクエリ）。 */
const url = () => window.location.pathname + window.location.search;

/**
 * ブラウザの戻る / 進むで URL が動いたことにする。
 *
 * ブラウザは `popstate` を出す前に URL を動かし終えている。動かすのに
 * `replaceState` を使うのは、`history.length` の数え方にこちらの操作を混ぜない
 * ため。
 */
function browserMovesTo(to: string) {
  window.history.replaceState(null, "", to);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

/** 走っている非同期の処理を流しきる。 */
const settle = () =>
  act(
    () =>
      new Promise<void>((resolve) => {
        setTimeout(resolve, 0);
      }),
  );

function setup(initial: { signedIn?: boolean; consenting?: boolean } = {}) {
  const showFailure = vi.fn();
  const reload = vi.fn<() => Promise<void>>().mockResolvedValue(undefined);
  const remove = vi.fn<(id: string) => void>();
  const clear = vi.fn<() => void>();
  // `useNewBoardFlow` の `create` は、名前を見てから渡された確認を呼ぶ。
  // テストごとに `mockImplementationOnce` で確認の呼び方を決める。
  const create = vi.fn<NewBoardFlow["create"]>();
  const replaceSession = vi.fn<(session: SessionStatus) => void>();

  const hook = renderHook(
    ({ signedIn, consenting }: { signedIn: boolean; consenting: boolean }) =>
      useBoardNavigation({
        signedIn,
        consenting,
        showFailure,
        boardList: { reload, remove, clear },
        newBoard: { create },
        replaceSession,
      }),
    {
      initialProps: {
        signedIn: initial.signedIn ?? true,
        consenting: initial.consenting ?? false,
      },
    },
  );
  return {
    hook,
    showFailure,
    reload,
    remove,
    clear,
    create,
    replaceSession,
  };
}

type Setup = ReturnType<typeof setup>;

/** 一覧からボードを開いた状態にする。 */
async function openFromList(hook: Setup["hook"], opened: BoardWithFiles) {
  vi.mocked(boardsApi.get).mockResolvedValueOnce(opened);
  await act(() => hook.result.current.open(opened.id));
  expect(hook.result.current.current?.id).toBe(opened.id);
}

let confirm: MockInstance<typeof window.confirm>;

beforeEach(() => {
  window.history.replaceState(null, "", "/");
  vi.spyOn(boardsApi, "get");
  confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
});

afterEach(() => {
  vi.restoreAllMocks();
  window.history.replaceState(null, "", "/");
});

describe("useBoardNavigation", () => {
  describe("一覧から開く", () => {
    it("ボードを出して URL を積み、未保存でなければ訊かない", async () => {
      const { hook } = setup();
      const before = window.history.length;

      await openFromList(hook, board("b1"));

      expect(url()).toBe("/?board=b1");
      expect(window.history.length - before).toBe(1);
      expect(hook.result.current.picking).toBe(false);
      expect(confirm).not.toHaveBeenCalled();
    });

    it("未保存なら取ってから訊き、断られたら前のボードのまま URL も動かさない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.handleDirtyChange(true));
      const before = window.history.length;

      const fetching = deferred<BoardWithFiles>();
      vi.mocked(boardsApi.get).mockReturnValueOnce(fetching.promise);
      confirm.mockReturnValue(false);
      let opening!: Promise<void>;
      act(() => {
        opening = hook.result.current.open("b2");
      });
      // **取っているあいだは訊かない。** 訊いてから取ると、取っているあいだの
      // 描き足しを確認なしで捨てる。
      await settle();
      expect(confirm).not.toHaveBeenCalled();

      await act(async () => {
        fetching.resolve(board("b2"));
        await opening;
      });
      expect(confirm).toHaveBeenCalledTimes(1);
      expect(hook.result.current.current?.id).toBe("b1");
      expect(url()).toBe("/?board=b1");
      expect(window.history.length - before).toBe(0);
    });

    it("取れなければ知らせて、ボードは開かない", async () => {
      const { hook, showFailure } = setup();
      vi.mocked(boardsApi.get).mockRejectedValueOnce(boom);

      await act(() => hook.result.current.open("b1"));

      expect(hook.result.current.current).toBeNull();
      expect(url()).toBe("/");
      expect(showFailure).toHaveBeenCalledWith(
        expect.objectContaining({
          message: expect.stringMatching(/^ボードを開けませんでした/),
        }),
      );
    });

    it("追い越された取得は、あとから届いても開かない", async () => {
      const { hook } = setup();
      const first = deferred<BoardWithFiles>();
      const second = deferred<BoardWithFiles>();
      vi.mocked(boardsApi.get)
        .mockReturnValueOnce(first.promise)
        .mockReturnValueOnce(second.promise);

      let a!: Promise<void>;
      let b!: Promise<void>;
      act(() => {
        a = hook.result.current.open("b1");
        b = hook.result.current.open("b2");
      });
      await act(async () => {
        second.resolve(board("b2"));
        await b;
      });
      await act(async () => {
        first.resolve(board("b1"));
        await a;
      });

      expect(hook.result.current.current?.id).toBe("b2");
      expect(url()).toBe("/?board=b2");
    });

    it("追い越された取得の失敗は知らせず、URL も巻き戻さない", async () => {
      const { hook, showFailure } = setup();
      const first = deferred<BoardWithFiles>();
      vi.mocked(boardsApi.get)
        .mockReturnValueOnce(first.promise)
        .mockResolvedValueOnce(board("b2"));

      let a!: Promise<void>;
      act(() => {
        a = hook.result.current.open("b1");
      });
      await act(() => hook.result.current.open("b2"));
      await act(async () => {
        first.reject(boom);
        await a;
      });

      expect(showFailure).not.toHaveBeenCalled();
      expect(hook.result.current.current?.id).toBe("b2");
      expect(url()).toBe("/?board=b2");
    });
  });

  describe("一覧へ戻る", () => {
    it("未保存なら訊き、断られたら何も変えない", async () => {
      const { hook, reload } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.handleDirtyChange(true));
      reload.mockClear();
      confirm.mockReturnValue(false);
      const before = window.history.length;

      act(() => hook.result.current.closeBoard());

      expect(confirm).toHaveBeenCalledTimes(1);
      expect(hook.result.current.current?.id).toBe("b1");
      expect(url()).toBe("/?board=b1");
      expect(window.history.length - before).toBe(0);
      expect(reload).not.toHaveBeenCalled();
    });

    it("閉じたら URL を積み、一覧を読み直す", async () => {
      const { hook, reload } = setup();
      await openFromList(hook, board("b1"));
      reload.mockClear();
      const before = window.history.length;

      act(() => hook.result.current.closeBoard());

      expect(hook.result.current.current).toBeNull();
      expect(url()).toBe("/");
      // 一覧は押して移った先なので、「戻る」で閉じたボードへ引き返せる。
      expect(window.history.length - before).toBe(1);
      expect(reload).toHaveBeenCalledTimes(1);
    });

    it("読み込み中に閉じたら、あとから届いたボードは開かない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      const fetching = deferred<BoardWithFiles>();
      vi.mocked(boardsApi.get).mockReturnValueOnce(fetching.promise);

      let opening!: Promise<void>;
      act(() => {
        opening = hook.result.current.open("b2");
      });
      act(() => hook.result.current.closeBoard());
      await act(async () => {
        fetching.resolve(board("b2"));
        await opening;
      });

      expect(hook.result.current.current).toBeNull();
      expect(url()).toBe("/");
    });
  });

  describe("戻る / 進む", () => {
    it("別のボードへ動いたら取ってから開き、履歴は積まない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      vi.mocked(boardsApi.get).mockResolvedValueOnce(board("b2"));
      const before = window.history.length;

      act(() => browserMovesTo("/?board=b2"));

      await waitFor(() => expect(hook.result.current.current?.id).toBe("b2"));
      expect(url()).toBe("/?board=b2");
      expect(window.history.length - before).toBe(0);
    });

    it("未保存で断られたら、出ている場所を積み直して URL を画面に合わせる", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.handleDirtyChange(true));
      confirm.mockReturnValue(false);
      const before = window.history.length;

      act(() => browserMovesTo("/"));
      await settle();

      expect(confirm).toHaveBeenCalledTimes(1);
      expect(hook.result.current.current?.id).toBe("b1");
      expect(url()).toBe("/?board=b1");
      // **戻すのではなく積む。** 何手ぶん動かされたのかを知る手立てが無い。
      expect(window.history.length - before).toBe(1);
    });

    it("一覧へ動いたら閉じて、一覧を読み直す", async () => {
      const { hook, reload } = setup();
      await openFromList(hook, board("b1"));
      reload.mockClear();

      act(() => browserMovesTo("/"));
      await settle();

      expect(hook.result.current.current).toBeNull();
      expect(url()).toBe("/");
      expect(reload).toHaveBeenCalledTimes(1);
    });

    it("出ている場所と同じなら、取りにいかない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      vi.mocked(boardsApi.get).mockClear();

      act(() => browserMovesTo("/?board=b1"));
      await settle();

      expect(boardsApi.get).not.toHaveBeenCalled();
      expect(hook.result.current.current?.id).toBe("b1");
    });

    it("読み込み中に一覧へ戻ったら、走っていた取得は開かない（#205）", async () => {
      const { hook } = setup();
      const fetching = deferred<BoardWithFiles>();
      vi.mocked(boardsApi.get).mockReturnValueOnce(fetching.promise);

      let opening!: Promise<void>;
      act(() => {
        opening = hook.result.current.open("b1");
      });
      // 画面に出ているのは一覧のままなので、「同じ場所」として抜ける。それでも
      // 走っていた取得は捨てる。
      act(() => browserMovesTo("/"));
      await act(async () => {
        fetching.resolve(board("b1"));
        await opening;
      });

      expect(hook.result.current.current).toBeNull();
      expect(url()).toBe("/");
    });

    it("移った先が取れなければ知らせて、URL を出ている場所に書き戻す", async () => {
      const { hook, showFailure } = setup();
      await openFromList(hook, board("b1"));
      vi.mocked(boardsApi.get).mockRejectedValueOnce(boom);

      act(() => browserMovesTo("/?board=gone"));
      await settle();

      expect(showFailure).toHaveBeenCalledTimes(1);
      expect(hook.result.current.current?.id).toBe("b1");
      // 開けなかったボードを URL に残すと、読み込み直すたびに同じ失敗を繰り返す。
      expect(url()).toBe("/?board=b1");
    });

    it("ログインが済むまでは追わない", async () => {
      setup({ signedIn: false });

      act(() => browserMovesTo("/?board=b1"));
      await settle();

      expect(boardsApi.get).not.toHaveBeenCalled();
    });
  });

  describe("起動時の URL", () => {
    it("URL のボードを開き、履歴は積まない", async () => {
      window.history.replaceState(null, "", "/?board=b1");
      vi.mocked(boardsApi.get).mockResolvedValueOnce(board("b1"));
      const before = window.history.length;

      const { hook } = setup();

      await waitFor(() => expect(hook.result.current.current?.id).toBe("b1"));
      expect(url()).toBe("/?board=b1");
      // 起動時に積むと、最初の「戻る」が etoki の中に留まり、来た場所へ戻れない。
      expect(window.history.length - before).toBe(0);
    });

    it("ボードの無い URL は、開いていない形に書き直す", async () => {
      window.history.replaceState(null, "", "/?picking=1");

      setup();
      await settle();

      expect(url()).toBe("/");
      expect(boardsApi.get).not.toHaveBeenCalled();
    });

    // 条件は `BoardPage` が「作成先を変更」を出す条件と同じ（ADR 0014 / 0017）。
    it.each([
      { name: "owner で作成先が固定されていない", patch: {}, picking: true },
      { name: "編集できる人", patch: { role: "editor" as const }, picking: false },
      { name: "作成先が固定済み", patch: { targetLocked: true }, picking: false },
    ])(
      "作成先の選び直しから始めるのは、選び直せるボードだけ（$name）",
      async ({ patch, picking }) => {
        window.history.replaceState(null, "", "/?board=b1&picking=1");
        vi.mocked(boardsApi.get).mockResolvedValueOnce(board("b1", patch));

        const { hook } = setup();

        await waitFor(() => expect(hook.result.current.current?.id).toBe("b1"));
        expect(hook.result.current.picking).toBe(picking);
        // 通らなかった指定は URL からも落とす。
        expect(url()).toBe(picking ? "/?board=b1&picking=1" : "/?board=b1");
      },
    );

    it("同意の画面では開かず、URL も書き換えない", async () => {
      window.history.replaceState(null, "", "/?authorize=abc&board=b1");

      setup({ consenting: true });
      await settle();

      expect(boardsApi.get).not.toHaveBeenCalled();
      expect(url()).toBe("/?authorize=abc&board=b1");
    });

    it("ログインが済んでから 1 度だけ開く", async () => {
      window.history.replaceState(null, "", "/?board=b1");
      vi.mocked(boardsApi.get).mockResolvedValue(board("b1"));

      const { hook } = setup({ signedIn: false });
      await settle();
      expect(boardsApi.get).not.toHaveBeenCalled();

      hook.rerender({ signedIn: true, consenting: false });
      await waitFor(() => expect(hook.result.current.current?.id).toBe("b1"));
      // 読み直しで引き戻されないよう、2 度目は読まない。
      hook.rerender({ signedIn: false, consenting: false });
      hook.rerender({ signedIn: true, consenting: false });
      await settle();

      expect(boardsApi.get).toHaveBeenCalledTimes(1);
    });
  });

  describe("新しいボード", () => {
    /** 作成先が未選択のボード（ADR 0100）。作るときは作成先を送らない。 */
    const untargeted = detail("b-new", {
      repositoryOwner: "",
      repositoryName: "",
      projectId: "",
      projectNumber: 0,
      projectTitle: "",
      projectUrl: "",
    });

    it("作れたら、作ったボードを画像なしでそのまま開いて URL を積み、一覧を読み直す", async () => {
      const { hook, create, reload } = setup();
      create.mockImplementationOnce(async (ask) => (ask() ? untargeted : null));
      const before = window.history.length;

      await act(() => hook.result.current.createBoard());

      expect(create).toHaveBeenCalledTimes(1);
      // **作成先の選択を挟まない。** 未選択のままキャンバスを開く。
      expect(hook.result.current.current).toEqual({ ...untargeted, files: {} });
      expect(hook.result.current.picking).toBe(false);
      expect(url()).toBe("/?board=b-new");
      // 「戻る」で作成の手前に戻れる。
      expect(window.history.length - before).toBe(1);
      expect(reload).toHaveBeenCalledTimes(1);
    });

    it("開いていたボードが未保存なら、作る前に訊いて断られたら開いていたボードのまま", async () => {
      const { hook, create } = setup();
      create.mockImplementationOnce(async (ask) => (ask() ? untargeted : null));
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.handleDirtyChange(true));
      confirm.mockReturnValue(false);

      await act(() => hook.result.current.createBoard());

      expect(confirm).toHaveBeenCalledTimes(1);
      expect(hook.result.current.current?.id).toBe("b1");
      expect(url()).toBe("/?board=b1");
    });

    it("捨ててよいと言われたら、作ったボードを開く", async () => {
      const { hook, create } = setup();
      create.mockImplementationOnce(async (ask) => (ask() ? untargeted : null));
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.handleDirtyChange(true));

      await act(() => hook.result.current.createBoard());

      expect(confirm).toHaveBeenCalledTimes(1);
      expect(hook.result.current.current?.id).toBe("b-new");
      expect(url()).toBe("/?board=b-new");
    });

    it("作っているあいだに戻る / 進むで移っていたら、作ったボードは開かない", async () => {
      const { hook, create, reload } = setup();
      const creating = deferred<BoardDetail | null>();
      create.mockImplementationOnce((ask) =>
        ask() ? creating.promise : Promise.resolve(null),
      );
      vi.mocked(boardsApi.get).mockResolvedValueOnce(board("b2"));

      let created!: Promise<void>;
      act(() => {
        created = hook.result.current.createBoard();
      });
      act(() => browserMovesTo("/?board=b2"));
      await waitFor(() => expect(hook.result.current.current?.id).toBe("b2"));
      reload.mockClear();
      await act(async () => {
        creating.resolve(untargeted);
        await created;
      });

      // 開いたボードを確認なしに外さない。作ったボードは一覧から開ける。
      expect(hook.result.current.current?.id).toBe("b2");
      expect(url()).toBe("/?board=b2");
      expect(reload).toHaveBeenCalledTimes(1);
    });

    // 切れると: 作っている最中にもう一度押すと、送る前に弾かれた 2 回目が世代を
    // 進め、1 回目に作れたボードを移ったものと読んで開かない。ダイアログは閉じ、
    // ボードは一覧にだけ増える（#481 の full review）。
    it("作っている最中にもう一度押しても、作ったボードを開く", async () => {
      const { hook, create } = setup();
      const creating = deferred<BoardDetail | null>();
      create.mockImplementationOnce((ask) =>
        ask() ? creating.promise : Promise.resolve(null),
      );
      // 2 回目は送る前に弾かれる（`useNewBoardFlow` の二重押しの守り）ので、確かめない。
      create.mockResolvedValueOnce(null);

      let first!: Promise<void>;
      act(() => {
        first = hook.result.current.createBoard();
      });
      await act(() => hook.result.current.createBoard());
      await act(async () => {
        creating.resolve(untargeted);
        await first;
      });

      expect(hook.result.current.current?.id).toBe("b-new");
      expect(url()).toBe("/?board=b-new");
    });

    it("作れなかったら何も変えない", async () => {
      const { hook, create, reload } = setup();
      create.mockResolvedValueOnce(null);

      await act(() => hook.result.current.createBoard());

      expect(hook.result.current.current).toBeNull();
      expect(url()).toBe("/");
      expect(reload).not.toHaveBeenCalled();
    });
  });

  describe("サーバーが返したボードで差し替える", () => {
    it("開いているボードなら差し替え、画像と版は手元のものを持ち越す（ADR 0092）", async () => {
      const { hook, reload } = setup();
      await openFromList(
        hook,
        board("b1", { files: { f1: "{}" }, updatedAt: "2026-10-07T00:00:00Z" }),
      );
      reload.mockClear();

      act(() =>
        hook.result.current.replaceBoard(
          detail("b1", { name: "新しい名前", updatedAt: "2026-10-08T00:00:00Z" }),
        ),
      );

      expect(hook.result.current.current).toEqual(
        board("b1", {
          name: "新しい名前",
          files: { f1: "{}" },
          updatedAt: "2026-10-07T00:00:00Z",
        }),
      );
      expect(reload).toHaveBeenCalledTimes(1);
    });

    it("別のボードの応答は入れない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));

      act(() => hook.result.current.replaceBoard(detail("b2", { name: "遅れて届いた" })));

      expect(hook.result.current.current).toEqual(board("b1"));
    });
  });

  describe("削除", () => {
    it("未保存でも訊かずに外し、一覧から除いて URL を置き換える", async () => {
      const { hook, remove, reload } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.handleDirtyChange(true));
      reload.mockClear();
      const before = window.history.length;

      act(() => hook.result.current.handleDeleted("b1"));

      // 確認は削除の手前で済んでいる（ADR 0042）。
      expect(confirm).not.toHaveBeenCalled();
      expect(hook.result.current.current).toBeNull();
      expect(remove).toHaveBeenCalledWith("b1");
      expect(url()).toBe("/");
      // 消えたボードを指す URL を 1 つ増やさない。
      expect(window.history.length - before).toBe(0);
      expect(reload).toHaveBeenCalledTimes(1);
    });

    it("削除のあとに、読み込み中だった取得が届いても開かない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      const fetching = deferred<BoardWithFiles>();
      vi.mocked(boardsApi.get).mockReturnValueOnce(fetching.promise);

      let opening!: Promise<void>;
      act(() => {
        opening = hook.result.current.open("b2");
      });
      act(() => hook.result.current.handleDeleted("b1"));
      await act(async () => {
        fetching.resolve(board("b2"));
        await opening;
      });

      expect(hook.result.current.current).toBeNull();
      expect(url()).toBe("/");
    });
  });

  /**
   * 作成先はボードの上のダイアログで選ぶ（ADR 0100）。**ボードの画面は外さない**
   * ので、開き直しも取り直しもしない。外すと絵解きの結果と手直しが消える。
   */
  describe("作成先を選ぶ", () => {
    it("ダイアログを開いたら URL に載せ、履歴は積まない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      const before = window.history.length;

      act(() => hook.result.current.startPicking());

      expect(hook.result.current.picking).toBe(true);
      expect(url()).toBe("/?board=b1&picking=1");
      expect(window.history.length - before).toBe(0);
    });

    it("決めたら応答のボードでその場で差し替え、画像と版は持ち越してダイアログを閉じる", async () => {
      const { hook, reload } = setup();
      // 開いてから貼った画像はキャンバスが持っていて、設定の応答は運ばない（ADR 0074）。
      await openFromList(
        hook,
        board("b1", { files: { pasted: "{}" }, updatedAt: "2026-10-07T00:00:00Z" }),
      );
      act(() => hook.result.current.startPicking());
      vi.mocked(boardsApi.get).mockClear();
      reload.mockClear();
      const setTarget = vi.spyOn(boardsApi, "setTarget").mockResolvedValueOnce(
        // 応答の版はサーバーのいまの版。基準にしない（ADR 0092）。
        detail("b1", { projectTitle: "新しい作成先", updatedAt: "2026-10-08T00:00:00Z" }),
      );
      const before = window.history.length;

      await act(() => hook.result.current.changeTarget(target));

      expect(setTarget).toHaveBeenCalledWith("b1", target);
      expect(hook.result.current.current).toEqual(
        board("b1", {
          projectTitle: "新しい作成先",
          files: { pasted: "{}" },
          updatedAt: "2026-10-07T00:00:00Z",
        }),
      );
      // **開き直さない。** 開き直すとキャンバスが作り直され、絵解きの結果が消える。
      expect(boardsApi.get).not.toHaveBeenCalled();
      expect(hook.result.current.picking).toBe(false);
      expect(url()).toBe("/?board=b1");
      expect(window.history.length - before).toBe(0);
      // 一覧は作成先でまとめて見せる（ADR 0019）。
      expect(reload).toHaveBeenCalledTimes(1);
    });

    // 失敗はダイアログの選択部品が出す（`RepositoryPicker`）。閉じると出す先が無い。
    it("設定できなければ投げ、ダイアログは開いたまま", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.startPicking());
      vi.spyOn(boardsApi, "setTarget").mockRejectedValueOnce(boom);

      await act(async () => {
        await expect(hook.result.current.changeTarget(target)).rejects.toThrow("boom");
      });

      expect(hook.result.current.current).toEqual(board("b1"));
      expect(hook.result.current.picking).toBe(true);
      expect(url()).toBe("/?board=b1&picking=1");
    });

    it("やめたらダイアログを閉じるだけで、取り直さない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.startPicking());
      vi.mocked(boardsApi.get).mockClear();
      const before = window.history.length;

      act(() => hook.result.current.cancelPicking());

      expect(hook.result.current.picking).toBe(false);
      expect(hook.result.current.current).toEqual(board("b1"));
      expect(boardsApi.get).not.toHaveBeenCalled();
      expect(url()).toBe("/?board=b1");
      expect(window.history.length - before).toBe(0);
    });

    // 一覧へ戻ったあとに応答を入れると、一覧の画面のまま URL だけがボードを指す。
    it("設定を待つあいだに一覧へ戻ったら、応答は画面にも URL にも入れない", async () => {
      const { hook, reload } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.startPicking());
      const setting = deferred<BoardDetail>();
      vi.spyOn(boardsApi, "setTarget").mockReturnValueOnce(setting.promise);

      let changed!: Promise<void>;
      act(() => {
        changed = hook.result.current.changeTarget(target);
      });
      act(() => hook.result.current.closeBoard());
      reload.mockClear();
      await act(async () => {
        setting.resolve(detail("b1", { projectTitle: "新しい作成先" }));
        await changed;
      });

      expect(hook.result.current.current).toBeNull();
      expect(hook.result.current.picking).toBe(false);
      expect(url()).toBe("/");
      // 作成先そのものは変わったので、一覧は読み直す。
      expect(reload).toHaveBeenCalledTimes(1);
    });

    it("設定を待つあいだに戻る / 進むで別のボードへ移ったら、応答は入れない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.startPicking());
      const setting = deferred<BoardDetail>();
      vi.spyOn(boardsApi, "setTarget").mockReturnValueOnce(setting.promise);
      vi.mocked(boardsApi.get).mockResolvedValueOnce(board("b2"));

      let changed!: Promise<void>;
      act(() => {
        changed = hook.result.current.changeTarget(target);
      });
      act(() => browserMovesTo("/?board=b2"));
      await waitFor(() => expect(hook.result.current.current?.id).toBe("b2"));
      await act(async () => {
        setting.resolve(detail("b1", { projectTitle: "新しい作成先" }));
        await changed;
      });

      expect(hook.result.current.current).toEqual(board("b2"));
      expect(url()).toBe("/?board=b2");
    });

    // 切れると: 戻る / 進むは移る前に世代を全部進めるので、未保存の確認で断って
    // 同じボードに残っても応答を捨て、サーバーは新しい作成先なのに画面は前の作成先を
    // 出し続ける（#481 の full review）。作るときに食い違いで断られる。
    it("設定を待つあいだに戻る / 進むで移ろうとして断ったら、応答はボードに入れる", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.startPicking());
      act(() => hook.result.current.handleDirtyChange(true));
      confirm.mockReturnValue(false);
      const setting = deferred<BoardDetail>();
      vi.spyOn(boardsApi, "setTarget").mockReturnValueOnce(setting.promise);

      let changed!: Promise<void>;
      act(() => {
        changed = hook.result.current.changeTarget(target);
      });
      act(() => browserMovesTo("/"));
      await settle();
      expect(confirm).toHaveBeenCalledTimes(1);
      await act(async () => {
        setting.resolve(detail("b1", { projectTitle: "新しい作成先" }));
        await changed;
      });

      expect(hook.result.current.current?.id).toBe("b1");
      expect(hook.result.current.current?.projectTitle).toBe("新しい作成先");
      // ダイアログと URL は動かさない。移る途中かどうかを応答の時点では言えず、
      // 書き換えると、移った先の履歴を上書きしうる。断って戻した場所（ダイアログ
      // を開いたボード）のままにし、閉じるのは「やめる」。
      expect(hook.result.current.picking).toBe(true);
      expect(url()).toBe("/?board=b1&picking=1");
    });
  });

  describe("ログアウト", () => {
    it("未保存なら訊き、断られたらログアウトしない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.handleDirtyChange(true));
      confirm.mockReturnValue(false);
      const logout = vi.spyOn(authApi, "logout");

      await act(() => hook.result.current.logout());

      expect(logout).not.toHaveBeenCalled();
      expect(hook.result.current.current?.id).toBe("b1");
    });

    it("通信を待たずにボードと一覧を外して URL を置き換え、読み直した状態に替える", async () => {
      const { hook, clear, replaceSession } = setup();
      await openFromList(hook, board("b1"));
      const loggingOut = deferred<void>();
      vi.spyOn(authApi, "logout").mockReturnValueOnce(loggingOut.promise);
      const signedOut: SessionStatus = { authRequired: true, authenticated: false };
      vi.spyOn(authApi, "session").mockResolvedValueOnce(signedOut);
      const before = window.history.length;

      let done!: Promise<void>;
      act(() => {
        done = hook.result.current.logout();
      });
      // 待ってから外すと、待っているあいだの描き足しを確認なしで捨てる。
      expect(hook.result.current.current).toBeNull();
      expect(clear).toHaveBeenCalledTimes(1);
      expect(url()).toBe("/");
      // 積むと「戻る」でログアウト前の URL に戻れてしまう。
      expect(window.history.length - before).toBe(0);

      await act(async () => {
        loggingOut.resolve();
        await done;
      });
      expect(replaceSession).toHaveBeenCalledWith(signedOut);
    });

    it("ログアウトできなければ知らせて、一覧を読み直す", async () => {
      const { hook, showFailure, reload } = setup();
      vi.spyOn(authApi, "logout").mockRejectedValueOnce(boom);

      await act(() => hook.result.current.logout());

      expect(showFailure).toHaveBeenCalledWith(
        expect.objectContaining({
          message: expect.stringMatching(/^ログアウトできませんでした/),
        }),
      );
      expect(reload).toHaveBeenCalledTimes(1);
    });

    it("読み込み中にログアウトしたら、あとから届いたボードは開かない", async () => {
      const { hook } = setup();
      const fetching = deferred<BoardWithFiles>();
      vi.mocked(boardsApi.get).mockReturnValueOnce(fetching.promise);
      vi.spyOn(authApi, "logout").mockResolvedValueOnce(undefined);
      vi.spyOn(authApi, "session").mockResolvedValueOnce({
        authRequired: true,
        authenticated: false,
      });

      let opening!: Promise<void>;
      act(() => {
        opening = hook.result.current.open("b1");
      });
      await act(() => hook.result.current.logout());
      await act(async () => {
        fetching.resolve(board("b1"));
        await opening;
      });

      expect(hook.result.current.current).toBeNull();
      expect(url()).toBe("/");
    });
  });

  // 手直しした下書きも、離れると戻らない（#432、ADR 0021 の追記）。未保存の
  // シーンとは失うものが違うので、何を失うかを言い分ける。
  describe("手直しした下書き", () => {
    it("手直しがあれば一覧へ戻る前に訊き、断られたら何も変えない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.handleDraftsEditedChange(true));
      confirm.mockReturnValue(false);

      act(() => hook.result.current.closeBoard());

      expect(confirm).toHaveBeenCalledWith(
        "手直しした下書きがあります。このまま移動すると、手直しは失われます。",
      );
      expect(hook.result.current.current?.id).toBe("b1");
    });

    it("未保存の変更と手直しの両方があれば、両方を失うと言う", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => {
        hook.result.current.handleDirtyChange(true);
        hook.result.current.handleDraftsEditedChange(true);
      });
      confirm.mockReturnValue(false);

      act(() => hook.result.current.closeBoard());

      expect(confirm).toHaveBeenCalledWith(
        "未保存の変更と、手直しした下書きがあります。このまま移動すると、描いた内容も手直しも失われます。",
      );
    });

    // 作成先を選ぶダイアログはキャンバスを外さない（ADR 0100）。手直しも残るので
    // 失うものが無い。訊くと、選ぶたびに確認に慣れて、離れる前の確認を読まなくなる。
    it("作成先の選び直しは、手直しがあっても訊かずに始める", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.handleDraftsEditedChange(true));
      act(() => hook.result.current.handleDirtyChange(true));

      act(() => hook.result.current.startPicking());

      expect(confirm).not.toHaveBeenCalled();
      expect(hook.result.current.picking).toBe(true);
    });

    it("手直しが下りたら、もう訊かない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.handleDraftsEditedChange(true));
      act(() => hook.result.current.handleDraftsEditedChange(false));

      act(() => hook.result.current.closeBoard());

      expect(confirm).not.toHaveBeenCalled();
      expect(hook.result.current.current).toBeNull();
    });
  });

  it("未保存と手直しの印を受け取る関数は、描き直しても同じものを渡す", async () => {
    const { hook } = setup();
    const first = hook.result.current.handleDirtyChange;
    const firstDrafts = hook.result.current.handleDraftsEditedChange;

    await openFromList(hook, board("b1"));

    // 作り直すと、BoardPage 側の登録し直しが走って「外れたので下ろす」が
    // 誤って動く。
    expect(hook.result.current.handleDirtyChange).toBe(first);
    expect(hook.result.current.handleDraftsEditedChange).toBe(firstDrafts);
  });
});
