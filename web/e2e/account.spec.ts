import { expect, test, type Page } from "@playwright/test";

import { installApi } from "./helpers/api";
import { boardListHeading } from "./helpers/board";
import {
  BOARD_NAME,
  DOCUMENT_URLS,
  authRequiredMock,
  baseMock,
  signedIn,
  signedOut,
  withAccountDeletion,
} from "./helpers/fixtures";

/**
 * 利用者が自分を消す口と、文書へのリンク（ADR 0101）の画面側の約束。
 *
 * どれを消してどれを残すかはサーバー（Go のテスト）が持つ。ここで見るのは、
 * 消えるものを先に見せること、確かめてから消すこと、消したらログインの画面に
 * 戻ること。
 */

/** 利用者のメニューから、アカウントの削除の確認を開く。 */
async function openAccountDeletion(page: Page) {
  await page.getByRole("button", { name: "Octo Cat" }).click();
  await page.getByRole("button", { name: "アカウントを削除…" }).click();
  return page.getByRole("dialog", { name: "アカウントを削除しますか" });
}

test.describe("アカウントの削除", () => {
  test("消えるものを見せてから消し、ログインの画面に戻る", async ({ page }) => {
    const mock = await installApi(page, withAccountDeletion(baseMock()));
    await page.goto("/");
    await boardListHeading(page).waitFor();

    const dialog = await openAccountDeletion(page);

    // 消えるボードと、そこにいるほかのメンバー。共有していても消える。
    await expect(dialog.getByText("一緒に消えるボード（2 枚）")).toBeVisible();
    await expect(dialog.getByText(BOARD_NAME)).toBeVisible();
    await expect(dialog.getByText("ひとりのメモ")).toBeVisible();
    await expect(
      dialog.getByText("ほかに 2 人のメンバーがいます。その人たちからも消えます。"),
    ).toBeVisible();
    await expect(
      dialog.getByText("残したいボードは、先に参加者をオーナーにしてください。", {
        exact: false,
      }),
    ).toBeVisible();
    // 記録の件数と、届いたか分からない書き込みは混ぜずに別に出す。
    await expect(dialog.getByText("作成した draft issue の記録 3 件")).toBeVisible();
    await expect(
      dialog.getByText("GitHub に届いたか分からない書き込み 1 件"),
    ).toBeVisible();
    // 外れるだけのボードと、GitHub に残るもの。
    await expect(
      dialog.getByText(
        "ほかのボード 1 枚からはメンバーとして外れます。ボードは残ります。",
      ),
    ).toBeVisible();
    await expect(
      dialog.getByText("ログイン・保存した GitHub のトークン・MCP の接続も消えます。"),
    ).toBeVisible();
    await expect(
      dialog.getByText("GitHub に作った draft issue は消えません", { exact: false }),
    ).toBeVisible();
    await expect(
      dialog.getByText("Authorized GitHub Apps から取り消せます", { exact: false }),
    ).toBeVisible();

    const deleted = page.waitForRequest(
      (req) =>
        req.method() === "DELETE" && new URL(req.url()).pathname === "/api/account",
    );
    await dialog.getByRole("button", { name: "削除する" }).click();
    await deleted;

    await expect(page.getByRole("button", { name: "GitHub でログイン" })).toBeVisible();
    expect(mock.account?.deleted).toBe(1);
  });

  test("やめると何も消さずに閉じ、メニューのボタンへ焦点を戻す", async ({ page }) => {
    const mock = await installApi(page, withAccountDeletion(baseMock()));
    await page.goto("/");

    const dialog = await openAccountDeletion(page);
    await dialog.getByRole("button", { name: "削除する" }).waitFor();
    await dialog.getByRole("button", { name: "やめる" }).click();

    await expect(dialog).toBeHidden();
    await expect(page.getByRole("button", { name: "Octo Cat" })).toBeFocused();
    await expect(boardListHeading(page)).toBeVisible();
    expect(mock.account?.deleted).toBe(0);
  });

  // 消えるボードが無いときも、無いと言う。黙ると、一覧が出なかった理由が読めない。
  test("消えるボードが無ければ、無いと言う", async ({ page }) => {
    const mock = withAccountDeletion(baseMock());
    mock.account!.deletion = { status: 200, body: { boards: [], leftBoardCount: 0 } };
    await installApi(page, mock);
    await page.goto("/");

    const dialog = await openAccountDeletion(page);

    await expect(dialog.getByText("一緒に消えるボードはありません。")).toBeVisible();
    await expect(dialog.getByText("外れます", { exact: false })).toHaveCount(0);
    await expect(dialog.getByRole("button", { name: "削除する" })).toBeEnabled();
  });

  // 何が消えるかを見せずに消させない。
  test("消えるものを引けなければ、削除するを出さない", async ({ page }) => {
    const mock = withAccountDeletion(baseMock());
    mock.account!.deletion = {
      status: 500,
      body: { code: "internal", error: "etoki: list boards: database is locked" },
    };
    await installApi(page, mock);
    await page.goto("/");

    const dialog = await openAccountDeletion(page);

    await expect(dialog.getByRole("alert")).toContainText(
      "消えるものを確かめられませんでした",
    );
    await expect(dialog.getByRole("button", { name: "削除する" })).toHaveCount(0);
    await expect(dialog.getByRole("button", { name: "やめる" })).toBeEnabled();
  });

  // 途中まで消えていても、もう一度押せば続きから消える（ADR 0101）。押し直す口を残す。
  test("消せなかったら理由を出し、もう一度押せる", async ({ page }) => {
    const mock = withAccountDeletion(baseMock());
    mock.account!.deleteError = {
      status: 500,
      body: {
        code: "internal",
        error: "etoki: delete board board-1: database is locked",
      },
    };
    await installApi(page, mock);
    await page.goto("/");

    const dialog = await openAccountDeletion(page);
    await dialog.getByRole("button", { name: "削除する" }).click();

    await expect(dialog.getByRole("alert")).toContainText(
      "アカウントを削除できませんでした",
    );
    await expect(dialog.getByRole("button", { name: "削除する" })).toBeEnabled();
    await expect(boardListHeading(page)).toBeVisible();

    // 直ったら、同じ確認から続けて消せる。
    mock.account!.deleteError = undefined;
    await dialog.getByRole("button", { name: "削除する" }).click();
    await expect(page.getByRole("button", { name: "GitHub でログイン" })).toBeVisible();
    expect(mock.account?.deleted).toBe(1);
  });

  // 消しているあいだは押せず、Esc でも閉じない。閉じると、消えたかどうかを
  // 読む場所が無くなる。
  test("消しているあいだは押し直せず、閉じない", async ({ page }) => {
    await installApi(page, withAccountDeletion(baseMock()));
    let release = () => {};
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    // 後から登録したものが先に当たる。応答を止めてから、いつもの偽物へ回す。
    await page.route(
      (url) => url.pathname === "/api/account",
      async (route) => {
        await held;
        await route.fallback();
      },
    );
    await page.goto("/");

    const dialog = await openAccountDeletion(page);
    await dialog.getByRole("button", { name: "削除する" }).click();

    await expect(dialog.getByRole("button", { name: "削除しています…" })).toBeDisabled();
    await expect(dialog.getByRole("button", { name: "やめる" })).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(dialog).toBeVisible();

    release();
    await expect(page.getByRole("button", { name: "GitHub でログイン" })).toBeVisible();
  });
});

test.describe("文書へのリンク", () => {
  // ログインの前に読めるところに置く（ADR 0101）。
  test("ログインの画面に、設定された文書へのリンクを出す", async ({ page }) => {
    const mock = authRequiredMock();
    mock.session = { status: 200, body: { ...signedOut(), ...DOCUMENT_URLS } };
    await installApi(page, mock);
    await page.goto("/");

    await expect(page.getByRole("link", { name: "利用規約" })).toHaveAttribute(
      "href",
      DOCUMENT_URLS.termsUrl,
    );
    await expect(
      page.getByRole("link", { name: "プライバシーポリシー" }),
    ).toHaveAttribute("href", DOCUMENT_URLS.privacyUrl);
  });

  test("利用者のメニューにも出す", async ({ page }) => {
    const mock = baseMock();
    mock.session = { status: 200, body: { ...signedIn(), ...DOCUMENT_URLS } };
    await installApi(page, mock);
    await page.goto("/");
    await page.getByRole("button", { name: "Octo Cat" }).click();

    await expect(page.getByRole("link", { name: "利用規約" })).toHaveAttribute(
      "href",
      DOCUMENT_URLS.termsUrl,
    );
    await expect(
      page.getByRole("link", { name: "プライバシーポリシー" }),
    ).toHaveAttribute("href", DOCUMENT_URLS.privacyUrl);
  });

  // 設定していなければ出さない。空のリンクや、どこにも行かないリンクを置かない。
  test("設定されていなければ出さない", async ({ page }) => {
    await installApi(page, authRequiredMock());
    await page.goto("/");
    await page.getByRole("button", { name: "GitHub でログイン" }).waitFor();

    await expect(page.getByRole("link", { name: "利用規約" })).toHaveCount(0);
    await expect(page.getByRole("link", { name: "プライバシーポリシー" })).toHaveCount(0);
  });

  // 片方だけ設定したら、片方だけ出す。
  test("片方だけ設定したら、片方だけ出す", async ({ page }) => {
    const mock = baseMock();
    mock.session = {
      status: 200,
      body: { ...signedIn(), privacyUrl: DOCUMENT_URLS.privacyUrl },
    };
    await installApi(page, mock);
    await page.goto("/");
    await page.getByRole("button", { name: "Octo Cat" }).click();
    await page.getByRole("button", { name: "ログアウト" }).waitFor();

    await expect(page.getByRole("link", { name: "利用規約" })).toHaveCount(0);
    await expect(page.getByRole("link", { name: "プライバシーポリシー" })).toBeVisible();
  });
});
