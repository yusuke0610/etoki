import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import {
  annotationCard,
  openBoard,
  openBoardWithMock,
  openPanelTab,
} from "./helpers/board";
import { BOARD_ID, BOARD_NAME, baseMock } from "./helpers/fixtures";

/**
 * 右のパネル（`SidePanel`、ADR 0065）。注釈・図のドラフト・メンバーをタブで並べる。
 *
 * **守りたいのは「切り替えても手元の作業が消えない」こと。** Excalidraw の
 * `Sidebar` に載せなかった理由がそれで（閉じるとタブの中身ごと外れる）、1 度
 * 開いたタブは描いたまま隠している。ここが崩れると、手直し中の下書きがタブを
 * 切り替えただけで消える。
 */
test.describe("右のパネル", () => {
  // 切れると: 下書きのタイトルや本文を直している途中で、メンバーを確かめに
  // 行って戻ると、直したぶんが解釈の出したままに戻る。作成は取り消せない
  // （ADR 0009）ので、戻ったことに気づかずに押すと、直す前の中身で作られる。
  test("タブを切り替えても、手直し中の下書きは消えない", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    const title = card.getByLabel("e1 のタイトル");
    await expect(title).toHaveValue("ログイン基盤");
    await title.fill("ログイン基盤（手直し済み）");

    await openPanelTab(page, "メンバー");
    await openPanelTab(page, "注釈");

    await expect(
      annotationCard(page, "ログイン").getByLabel("e1 のタイトル"),
    ).toHaveValue("ログイン基盤（手直し済み）");
  });

  test("図のドラフトの書きかけの指示は、タブを切り替えても残る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const chat = await openPanelTab(page, "図のドラフト");
    await chat.getByLabel("図への指示").fill("返金の分岐も足して");

    await openPanelTab(page, "注釈");
    await openPanelTab(page, "図のドラフト");

    await expect(chat.getByLabel("図への指示")).toHaveValue("返金の分岐も足して");
  });

  // 押したときだけ引く（`.claude/rules/async-ui.md`）。描いたまま隠すだけに
  // すると、ボードを開いた時点でメンバーの一覧を取りに行く。
  test("メンバーはタブを開くまで取りにいかない", async ({ page }) => {
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
    await openPanelTab(page, "メンバー");
    await responded;
    // 開いたら取りにいく。**回数は数えない。** E2E は開発サーバーで動くので、
    // React の StrictMode が effect を 2 度走らせる（本番では 1 回）。守りたいのは
    // 上の「開くまで 0 回」のほう。
    expect(fetched).not.toEqual([]);
  });

  // WAI-ARIA の tabs の形。矢印で隣のタブへ移り、そのまま開く。選ばれている
  // タブだけが Tab キーで止まる。
  test("矢印キーで隣のタブへ移り、そのタブが開く", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const annotations = page.getByRole("tab", { name: "注釈", exact: true });
    await annotations.focus();
    await page.keyboard.press("ArrowRight");

    const diagram = page.getByRole("tab", { name: "図のドラフト", exact: true });
    await expect(diagram).toBeFocused();
    await expect(diagram).toHaveAttribute("aria-selected", "true");
    await expect(page.getByRole("tabpanel", { name: "図のドラフト" })).toBeVisible();
    await expect(annotations).toHaveAttribute("tabindex", "-1");

    // 端から先は反対の端へ回る。
    await page.keyboard.press("ArrowLeft");
    await page.keyboard.press("ArrowLeft");
    await expect(page.getByRole("tab", { name: "メンバー", exact: true })).toBeFocused();
  });

  // 切れると: 共有が未設定のメンバーのタブは理由の本文だけで、中にフォーカスできる
  // ものが無い。タブから Tab キーを押しても、キーボードだけの人はパネルの中身
  // （理由）へ移れず、次の領域へ飛ぶ。
  test("フォーカスできるものが無いパネルも、Tab キーで移れる", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: { interpretation: true, diagramDraft: true, creation: true, sharing: false },
    };
    await openBoardWithMock(page, mock);

    const panel = await openPanelTab(page, "メンバー");
    await expect(panel.getByText("共有には認証の設定が必要です")).toBeVisible();

    await page.getByRole("tab", { name: "メンバー", exact: true }).focus();
    await page.keyboard.press("Tab");
    await expect(panel).toBeFocused();
  });

  // 切れると: 取得に失敗したメンバー一覧は、ボードを開き直すまで復旧できない。
  // パネルは開いたタブを隠したまま残すので、タブを切り替えても取り直されない。
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

    const panel = await openPanelTab(page, "メンバー");
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

    const panel = await openPanelTab(page, "メンバー");
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

    const panel = await openPanelTab(page, "メンバー");
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
