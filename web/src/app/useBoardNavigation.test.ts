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
  // `useNewBoardFlow` の `start` / `createWithoutTarget` は、名前を見てから
  // 渡された確認を呼ぶ。ここでは名前の検査を省き、確認だけ呼ぶ。
  const start = vi.fn<NewBoardFlow["start"]>((confirm) => confirm());
  const leave = vi.fn<() => void>();
  const create = vi.fn<NewBoardFlow["create"]>();
  const createWithoutTarget = vi.fn<NewBoardFlow["createWithoutTarget"]>();
  const replaceSession = vi.fn<(session: SessionStatus) => void>();

  const hook = renderHook(
    ({ signedIn, consenting }: { signedIn: boolean; consenting: boolean }) =>
      useBoardNavigation({
        signedIn,
        consenting,
        showFailure,
        boardList: { reload, remove, clear },
        newBoard: { start, leave, create, createWithoutTarget },
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
    start,
    leave,
    create,
    createWithoutTarget,
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
      const { hook, leave } = setup();
      const before = window.history.length;

      await openFromList(hook, board("b1"));

      expect(url()).toBe("/?board=b1");
      expect(window.history.length - before).toBe(1);
      expect(hook.result.current.picking).toBe(false);
      // 開いたら作成先の選択の流れからは抜ける。
      expect(leave).toHaveBeenCalled();
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
    it("作成先の選択へ進むときは未保存を訊き、断られたら何も変えない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.handleDirtyChange(true));
      confirm.mockReturnValue(false);

      act(() => hook.result.current.startCreating());

      expect(confirm).toHaveBeenCalledTimes(1);
      expect(hook.result.current.current?.id).toBe("b1");
      expect(url()).toBe("/?board=b1");
    });

    it("作成先の選択へ進んだら、開いていたボードを外して URL を置き換える", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      const before = window.history.length;

      act(() => hook.result.current.startCreating());

      expect(hook.result.current.current).toBeNull();
      expect(url()).toBe("/");
      expect(window.history.length - before).toBe(0);
    });

    it("作れたら、作ったボードを画像なしで開いて URL を積み、一覧を読み直す", async () => {
      const { hook, create, reload } = setup();
      create.mockResolvedValueOnce(detail("b-new"));
      const before = window.history.length;

      await act(() => hook.result.current.createWithTarget(target));

      expect(create).toHaveBeenCalledWith(target);
      expect(hook.result.current.current).toEqual({ ...detail("b-new"), files: {} });
      expect(url()).toBe("/?board=b-new");
      // 「戻る」で作成の手前に戻れる。
      expect(window.history.length - before).toBe(1);
      expect(reload).toHaveBeenCalledTimes(1);
    });

    it("作っているあいだに戻る / 進むで移っていたら、作ったボードは開かない", async () => {
      const { hook, create, reload } = setup();
      const creating = deferred<BoardDetail | null>();
      create.mockReturnValueOnce(creating.promise);
      vi.mocked(boardsApi.get).mockResolvedValueOnce(board("b2"));

      let created!: Promise<void>;
      act(() => {
        created = hook.result.current.createWithTarget(target);
      });
      act(() => browserMovesTo("/?board=b2"));
      await waitFor(() => expect(hook.result.current.current?.id).toBe("b2"));
      reload.mockClear();
      await act(async () => {
        creating.resolve(detail("b-new"));
        await created;
      });

      // 開いたボードを確認なしに外さない。作ったボードは一覧から開ける。
      expect(hook.result.current.current?.id).toBe("b2");
      expect(url()).toBe("/?board=b2");
      expect(reload).toHaveBeenCalledTimes(1);
    });

    it("作れなかったら何も変えない", async () => {
      const { hook, create, reload } = setup();
      create.mockResolvedValueOnce(null);

      await act(() => hook.result.current.createWithTarget(target));

      expect(hook.result.current.current).toBeNull();
      expect(url()).toBe("/");
      expect(reload).not.toHaveBeenCalled();
    });

    it("作成先を選ばずに作るときも、未保存の確認を渡し、作ったボードを開く", async () => {
      const { hook, createWithoutTarget } = setup();
      createWithoutTarget.mockImplementationOnce(async (ask) =>
        ask() ? detail("b-new") : null,
      );
      act(() => hook.result.current.handleDirtyChange(true));

      await act(() => hook.result.current.createWithoutTarget());

      expect(confirm).toHaveBeenCalledTimes(1);
      expect(hook.result.current.current?.id).toBe("b-new");
      expect(url()).toBe("/?board=b-new");
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

  describe("作成先の選び直し", () => {
    it("始めたら URL に載せ、履歴は積まない", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      const before = window.history.length;

      act(() => hook.result.current.startPicking());

      expect(hook.result.current.picking).toBe(true);
      expect(url()).toBe("/?board=b1&picking=1");
      expect(window.history.length - before).toBe(0);
    });

    it("作成先を決めたら、画像ごと取り直して選択画面を閉じる", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.startPicking());
      const setTarget = vi
        .spyOn(boardsApi, "setTarget")
        .mockResolvedValueOnce(detail("b1"));
      // 開いてから貼って保存した画像も、取り直せば戻ってくる。
      const fetched = board("b1", { files: { pasted: "{}" } });
      vi.mocked(boardsApi.get).mockResolvedValueOnce(fetched);

      await act(() => hook.result.current.changeTarget(target));

      expect(setTarget).toHaveBeenCalledWith("b1", target);
      expect(hook.result.current.current).toEqual(fetched);
      expect(hook.result.current.picking).toBe(false);
      expect(url()).toBe("/?board=b1");
    });

    it("やめたら取り直してから戻り、取れなければ選択画面に残る", async () => {
      const { hook, showFailure } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.startPicking());

      vi.mocked(boardsApi.get).mockRejectedValueOnce(boom);
      await act(() => hook.result.current.cancelPicking());
      expect(showFailure).toHaveBeenCalledTimes(1);
      expect(hook.result.current.picking).toBe(true);

      // 開いてから保存したものが画面から消えないよう、手元の値では戻さない。
      const fetched = board("b1", { updatedAt: "2026-10-09T00:00:00Z" });
      vi.mocked(boardsApi.get).mockResolvedValueOnce(fetched);
      await act(() => hook.result.current.cancelPicking());
      expect(hook.result.current.current).toEqual(fetched);
      expect(hook.result.current.picking).toBe(false);
      expect(url()).toBe("/?board=b1");
    });

    it("設定を待つあいだに「やめる」の取り直しが届いても、選択画面を閉じない（#242）", async () => {
      const { hook } = setup();
      await openFromList(hook, board("b1"));
      act(() => hook.result.current.startPicking());
      const cancelling = deferred<BoardWithFiles>();
      const setting = deferred<BoardDetail>();
      vi.spyOn(boardsApi, "setTarget").mockReturnValueOnce(setting.promise);
      vi.mocked(boardsApi.get)
        .mockReturnValueOnce(cancelling.promise)
        .mockResolvedValueOnce(board("b1", { projectTitle: "新しい作成先" }));

      let cancelled!: Promise<void>;
      let changed!: Promise<void>;
      act(() => {
        cancelled = hook.result.current.cancelPicking();
        changed = hook.result.current.changeTarget(target);
      });
      await act(async () => {
        cancelling.resolve(board("b1", { projectTitle: "変える前の作成先" }));
        await cancelled;
      });
      expect(hook.result.current.picking).toBe(true);

      await act(async () => {
        setting.resolve(detail("b1"));
        await changed;
      });
      expect(hook.result.current.picking).toBe(false);
      expect(hook.result.current.current?.projectTitle).toBe("新しい作成先");
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

  it("未保存の印を受け取る関数は、描き直しても同じものを渡す", async () => {
    const { hook } = setup();
    const first = hook.result.current.handleDirtyChange;

    await openFromList(hook, board("b1"));

    // 作り直すと、BoardPage 側の登録し直しが走って「外れたので下ろす」が
    // 誤って動く。
    expect(hook.result.current.handleDirtyChange).toBe(first);
  });
});
