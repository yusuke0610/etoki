import { expect, test, type Page } from "@playwright/test";

import { holdBoardDetail, installApi, summarize, type ApiMock } from "./helpers/api";
import { drawRectangle, openBoard } from "./helpers/board";
import { baseMock, board, BOARD_ID } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";
const OTHER_ID = "board-other";
const OTHER_NAME = "別プロジェクトのブレスト";

/** 2 枚のボードを持つモック。切り替えと履歴を見るのに要る。 */
function twoBoards(): ApiMock {
  const mock = baseMock();
  const other = {
    ...board(),
    id: OTHER_ID,
    name: OTHER_NAME,
    projectId: "PVT_2",
    projectNumber: 4,
    projectTitle: "技術的負債",
  };
  mock.boards = [...mock.boards, summarize(other)];
  mock.details[other.id] = other;
  mock.annotations[other.id] = [];
  return mock;
}

/** いま出ている URL のクエリ。`?` を含む。 */
function search(page: Page): string {
  return new URL(page.url()).search;
}

test.describe("ボードの URL", () => {
  // ADR 0056。開いているボードが URL に出ないと、リロードでも共有でも
  // 「サイドバーから探す」しか手が無い。
  test("開いているボードが URL に出る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    expect(search(page)).toBe("");

    await openBoard(page, BOARD_NAME);
    expect(search(page)).toBe(`?board=${BOARD_ID}`);
  });

  test("URL から直接開ける", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto(`/?board=${BOARD_ID}`);

    await expect(page.getByRole("heading", { name: BOARD_NAME, level: 1 })).toBeVisible();
    await expect(page.locator(".excalidraw canvas").first()).toBeVisible();
  });

  // リロードで開き直せることがこの機能の主目的。
  test("リロードしても開いたままになる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await page.reload();
    await expect(page.getByRole("heading", { name: BOARD_NAME, level: 1 })).toBeVisible();
  });

  // 非メンバーにも消えたボードにも同じ not_found が返る（ADR 0016 / 0017）。
  // **URL から開いたときも見せ方を変えない。** 変えると、画面の違いから
  // ボードの存在を確かめられる。
  test("権限の無いボード ID は案内文に落ち、URL も戻る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/?board=someone-elses-board");

    await expect(page.getByRole("alert")).toContainText(
      "見つかりませんでした。消されたか、権限がありません。",
    );
    await expect(
      page.getByText("左からボードを選ぶか、新しく作成してください。"),
    ).toBeVisible();
    // 開けなかったボードを URL に残すと、読み込み直すたびに同じ失敗を繰り返す。
    expect(search(page)).toBe("");
  });

  test("戻る / 進むでボードが切り替わる", async ({ page }) => {
    await installApi(page, twoBoards());
    await page.goto("/");

    await openBoard(page, BOARD_NAME);
    await openBoard(page, OTHER_NAME);
    expect(search(page)).toBe(`?board=${OTHER_ID}`);

    await page.goBack();
    await expect(page.getByRole("heading", { name: BOARD_NAME, level: 1 })).toBeVisible();
    expect(search(page)).toBe(`?board=${BOARD_ID}`);

    await page.goForward();
    await expect(page.getByRole("heading", { name: OTHER_NAME, level: 1 })).toBeVisible();
    expect(search(page)).toBe(`?board=${OTHER_ID}`);
  });

  // キャンバスが外れる導線が 1 つ増えたので、`confirmDiscard` をここにも
  // 掛ける（`web/CLAUDE.md`）。戻る / 進むはアプリ側で止められないので、
  // 捨てないと決めたら URL を積み直して画面に合わせる。
  test("戻るで未保存の確認が出て、拒むと画面も URL も動かない", async ({ page }) => {
    await installApi(page, twoBoards());
    await page.goto("/");

    await openBoard(page, BOARD_NAME);
    await openBoard(page, OTHER_NAME);
    await drawRectangle(page);
    await expect(page.locator(".dirty")).toBeVisible();

    let asked = "";
    page.once("dialog", (dialog) => {
      asked = dialog.message();
      void dialog.dismiss();
    });

    await page.goBack();

    await expect.poll(() => asked).toContain("描いた内容は失われます");
    // 捨てないと答えたので、開いているのは元のまま。
    await expect(page.getByRole("heading", { name: OTHER_NAME, level: 1 })).toBeVisible();
    await expect(page.locator(".dirty")).toBeVisible();
    // URL も画面に合わせ直す。ここが抜けると、アドレスバーだけが別のボードを
    // 指したまま残る。
    await expect.poll(() => search(page)).toBe(`?board=${OTHER_ID}`);
  });

  test("戻るで未保存の確認に応じると、前のボードへ移る", async ({ page }) => {
    await installApi(page, twoBoards());
    await page.goto("/");

    await openBoard(page, BOARD_NAME);
    await openBoard(page, OTHER_NAME);
    await drawRectangle(page);

    page.once("dialog", (dialog) => void dialog.accept());
    await page.goBack();

    await expect(page.getByRole("heading", { name: BOARD_NAME, level: 1 })).toBeVisible();
    await expect.poll(() => search(page)).toBe(`?board=${BOARD_ID}`);
  });

  // 開く導線が 2 つ（サイドバーと戻る / 進む）あるので、取得が並走する
  // （`.claude/rules/async-ui.md`）。古い応答を反映すると、押した順と違う
  // ボードが開き、URL もそちらを指す。
  test("追い越された取得の応答は捨てる", async ({ page }) => {
    const mock = twoBoards();
    await installApi(page, mock);

    // 1 枚目の取得だけを止める。全部止めると、追い越す側まで待つことになり、
    // 確かめたい「古い応答があとから着く」並びを作れない。
    let release = (): void => {};
    await holdBoardDetail(
      page,
      BOARD_ID,
      new Promise<void>((r) => (release = () => r())),
    );

    await page.goto("/");
    // 止めてあるので開き切らない。押したことだけ確かめて次へ進む。
    await page.locator(".board-list").getByRole("button", { name: BOARD_NAME }).click();
    await openBoard(page, OTHER_NAME);

    // ここで古いほうの応答が着く。
    release();

    // 追い越された応答は捨てるので、開いているのは 2 枚目のまま。
    await expect(page.getByRole("heading", { name: OTHER_NAME, level: 1 })).toBeVisible();
    await expect(page.getByRole("heading", { name: BOARD_NAME, level: 1 })).toHaveCount(
      0,
    );
    // URL も巻き戻らない。ここが抜けると、画面と URL が別のボードを指す。
    await expect.poll(() => search(page)).toBe(`?board=${OTHER_ID}`);
  });

  test.describe("作成先の選び直し", () => {
    test("選択画面も URL に出て、リロードで戻る", async ({ page }) => {
      await installApi(page, baseMock());
      await page.goto("/");
      await openBoard(page, BOARD_NAME);

      await page.getByRole("button", { name: "作成先を変更" }).click();
      await expect(page.locator(".picker")).toBeVisible();
      expect(search(page)).toBe(`?board=${BOARD_ID}&picking=1`);

      await page.reload();
      await expect(page.locator(".picker")).toBeVisible();
    });

    test("引き返すと URL からも消える", async ({ page }) => {
      await installApi(page, baseMock());
      await page.goto(`/?board=${BOARD_ID}&picking=1`);
      await expect(page.locator(".picker")).toBeVisible();

      await page.getByRole("button", { name: "やめる" }).click();
      await expect(
        page.getByRole("heading", { name: BOARD_NAME, level: 1 }),
      ).toBeVisible();
      expect(search(page)).toBe(`?board=${BOARD_ID}`);
    });

    // 作成先を変えられるのは owner だけ（ADR 0017）。**URL から入る口だけを
    // 緩めない。** 緩めると、画面から押せないはずの操作にそこからだけ入れる。
    test("viewer は URL から選択画面に入れない", async ({ page }) => {
      const mock = baseMock();
      mock.details[BOARD_ID] = { ...board(), role: "viewer" };
      mock.boards = mock.boards.map((b) => ({ ...b, role: "viewer" }));

      await installApi(page, mock);
      await page.goto(`/?board=${BOARD_ID}&picking=1`);

      await expect(
        page.getByRole("heading", { name: BOARD_NAME, level: 1 }),
      ).toBeVisible();
      await expect(page.locator(".picker")).toHaveCount(0);
      // 通らなかった指定は URL からも落とす。残すと、読み込み直すたびに
      // 通らない指定を送り直すことになる。
      expect(search(page)).toBe(`?board=${BOARD_ID}`);
    });

    // 固定済みなら変更手段そのものが無い（ADR 0014）。上と同じ穴。
    test("作成先が固定されていたら URL から選択画面に入れない", async ({ page }) => {
      const mock = baseMock();
      mock.details[BOARD_ID] = { ...board(), targetLocked: true };

      await installApi(page, mock);
      await page.goto(`/?board=${BOARD_ID}&picking=1`);

      await expect(
        page.getByRole("heading", { name: BOARD_NAME, level: 1 }),
      ).toBeVisible();
      await expect(page.locator(".picker")).toHaveCount(0);
      expect(search(page)).toBe(`?board=${BOARD_ID}`);
    });
  });
});
