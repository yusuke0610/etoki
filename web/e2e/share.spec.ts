import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { openBoard, openBoardWithMock, openShareDialog } from "./helpers/board";
import { BOARD_ID, BOARD_NAME, baseMock } from "./helpers/fixtures";

/**
 * 共有のダイアログ（`ShareDialog`、#248、ADR 0078）。メンバーの一覧と招待は
 * 右のパネルではなく、キャンバスのメニューの「共有…」から開く。
 *
 * **守りたいのは、開いたときにだけ引くことと、閉じたあとに居場所を失わない
 * こと。** ロールごとの出し分けは `sharing.spec.ts` が見ている。
 */
test.describe("共有のダイアログ", () => {
  // 切れると: 開いた項目はメニューごと消えるので、焦点が body に落ち、
  // キーボードの利用者はどこにいるか分からなくなる。
  test("閉じると、焦点がメニューの口へ戻る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const dialog = await openShareDialog(page);
    await dialog.getByRole("button", { name: "閉じる", exact: true }).click();

    await expect(dialog).toBeHidden();
    await expect(page.locator('[data-testid="main-menu-trigger"]')).toBeFocused();

    // Esc でも同じ。閉じたときの処理は 1 か所に集めてある。
    const reopened = await openShareDialog(page);
    await page.keyboard.press("Escape");
    await expect(reopened).toBeHidden();
    await expect(page.locator('[data-testid="main-menu-trigger"]')).toBeFocused();
  });

  // 閉じたら中身は外す。開き直したときは引き直すので、開いていないあいだに
  // 増えたメンバーも出る。描いたまま隠す作り（右のパネル）に戻すと落ちる。
  test("開き直すと、一覧を引き直す", async ({ page }) => {
    const mock = baseMock();
    mock.members = {
      [BOARD_ID]: [
        {
          userId: "user-1",
          login: "alice",
          displayName: "Alice",
          role: "owner",
          createdAt: "2026-08-05T10:00:00Z",
        },
      ],
    };
    await openBoardWithMock(page, mock);

    const dialog = await openShareDialog(page);
    await expect(dialog).toContainText("Alice");
    await page.keyboard.press("Escape");

    mock.members[BOARD_ID]?.push({
      userId: "user-2",
      login: "bob",
      displayName: "Bob",
      role: "editor",
      createdAt: "2026-08-06T10:00:00Z",
    });
    const reopened = await openShareDialog(page);
    await expect(reopened).toContainText("Bob");
  });

  // 押したときだけ引く（`.claude/rules/async-ui.md`）。ボードを開いた時点では
  // メンバーの一覧を取りに行かない。
  test("メンバーはダイアログを開くまで取りにいかない", async ({ page }) => {
    await installApi(page, baseMock());
    const fetched: string[] = [];
    page.on("request", (req) => {
      if (
        req.method() === "GET" &&
        /^\/api\/boards\/[^/]+\/members$/.test(new URL(req.url()).pathname)
      ) {
        fetched.push(req.url());
      }
    });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    expect(fetched).toEqual([]);

    const responded = page.waitForResponse((r) =>
      /^\/api\/boards\/[^/]+\/members$/.test(new URL(r.url()).pathname),
    );
    await openShareDialog(page);
    await responded;
    // 開いたら取りにいく。**回数は数えない。** E2E は開発サーバーで動くので、
    // React の StrictMode が effect を 2 度走らせる（本番では 1 回）。守りたいのは
    // 上の「開くまで 0 回」のほう。
    expect(fetched).not.toEqual([]);
  });

  // 切れると: 取得に失敗したメンバー一覧は、ダイアログを開き直すまで復旧できない。
  // その場で取り直す口が要る。
  test("メンバー一覧の取得に失敗したら、再試行で取り直せる", async ({ page }) => {
    const mock = baseMock();
    mock.members = {
      [BOARD_ID]: [
        {
          userId: "user-1",
          login: "alice",
          displayName: "Alice",
          role: "owner",
          createdAt: "2026-08-05T10:00:00Z",
        },
      ],
    };
    mock.membersListError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    await openBoardWithMock(page, mock);

    const panel = await openShareDialog(page);
    await expect(panel.getByRole("alert")).toContainText(
      "メンバーを取得できませんでした",
    );

    delete mock.membersListError;
    await panel.getByRole("button", { name: "再試行" }).click();

    await expect(panel).toContainText("Alice");
    await expect(panel.getByRole("alert")).toHaveCount(0);
    await expect(panel.getByRole("button", { name: "再試行" })).toHaveCount(0);
  });

  // 再試行は一覧の取得だけのもの。招待の失敗に出すと、何を再試行するのか読めない。
  test("招待の失敗には再試行を出さない", async ({ page }) => {
    const mock = baseMock();
    mock.members = { [BOARD_ID]: [] };
    mock.inviteError = {
      status: 403,
      body: { code: "forbidden_role", error: "etoki: insufficient role" },
    };
    await openBoardWithMock(page, mock);

    const panel = await openShareDialog(page);
    await panel.getByLabel("招待する login").fill("bob");
    await panel.getByRole("button", { name: "確認する" }).click();
    await panel.getByRole("button", { name: "@bob を招待する" }).click();

    await expect(panel.getByRole("alert")).toBeVisible();
    await expect(panel.getByRole("button", { name: "再試行" })).toHaveCount(0);
  });

  // 切れると: 取得の失敗のあとで招待の確認も失敗すると、再試行が確認の失敗の隣に
  // 並び、押すと確認ではなく一覧を取り直す。取得の失敗と操作の失敗は別に出す。
  test("取得の失敗のあとで確認が失敗しても、再試行は取得の失敗にだけ添う", async ({
    page,
  }) => {
    const mock = baseMock();
    mock.members = { [BOARD_ID]: [] };
    mock.membersListError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    mock.lookupInviteeError = {
      status: 400,
      body: {
        code: "invalid_input",
        error: 'etoki: invalid input: "carol" has not signed in to etoki yet',
      },
    };
    await openBoardWithMock(page, mock);

    const panel = await openShareDialog(page);
    await panel.getByLabel("招待する login").fill("carol");
    await panel.getByRole("button", { name: "確認する" }).click();

    const alerts = panel.getByRole("alert");
    await expect(alerts).toHaveCount(2);
    await expect(
      alerts.filter({ hasText: "メンバーを取得できませんでした" }),
    ).toHaveCount(1);
    await expect(
      alerts.filter({ hasText: "招待する相手を確認できませんでした" }),
    ).toHaveCount(1);

    // 取り直せたら取得の失敗だけが消え、確認の失敗は残る。
    delete mock.membersListError;
    await panel.getByRole("button", { name: "再試行" }).click();
    await expect(alerts).toHaveCount(1);
    await expect(alerts).toContainText("招待する相手を確認できませんでした");
    await expect(panel.getByRole("button", { name: "再試行" })).toHaveCount(0);
  });
});
