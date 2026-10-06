import { expect, test } from "@playwright/test";

import {
  annotationCard,
  annotationDetail,
  interpret,
  openAnnotationDetail,
  openBoardMenu,
  openBoardWithMock,
  openPanelTab,
} from "./helpers/board";
import { BOARD_ID, baseMock, board } from "./helpers/fixtures";

test.describe("共有", () => {
  test("オーナーは招待でき、招待した相手が一覧に並ぶ", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await openPanelTab(page, "メンバー");

    // 招待できる相手の条件は、失敗してから知らせるのでは遅い。
    await expect(
      page.getByText("招待できるのは、一度 etoki にログインしたことがある人だけです。"),
    ).toBeVisible();

    await page.getByLabel("招待する login").fill("bob");
    await page.getByLabel("招待するロール").selectOption("editor");
    await page.getByRole("button", { name: "確認する" }).click();
    await page.getByRole("button", { name: "@bob を招待する" }).click();

    await expect(page.getByRole("region", { name: "メンバー" })).toContainText("bob");
  });

  // **login だけで招待しない**（ADR 0053、#142）。etoki が知っているのは「最後に
  // その login でログインした人」までなので、表示名・ID・最終ログインを見せてから
  // 送る。確かめた相手の ID を招待に載せ、確かめる前には何も送らない。
  test("招待する前に、login が当たった相手を見せる", async ({ page }) => {
    let invites = 0;
    page.on("request", (req) => {
      if (req.method() === "POST" && new URL(req.url()).pathname.endsWith("/members")) {
        invites++;
      }
    });

    const mock = await openBoardWithMock(page, baseMock());
    await openPanelTab(page, "メンバー");

    await page.getByLabel("招待する login").fill("bob");
    await page.getByRole("button", { name: "確認する" }).click();

    const confirm = page.getByRole("group", { name: "招待する相手の確認" });
    await expect(confirm).toContainText("@bob");
    await expect(confirm).toContainText("user-bob");
    await expect(confirm).toContainText("2026/8/1");
    expect(invites).toBe(0);

    // やめたら何も送らない。
    await confirm.getByRole("button", { name: "やめる" }).click();
    await expect(confirm).toHaveCount(0);
    expect(invites).toBe(0);

    await page.getByRole("button", { name: "確認する" }).click();
    const sent = page.waitForRequest(
      (req) =>
        req.method() === "POST" && new URL(req.url()).pathname.endsWith("/members"),
    );
    await page.getByRole("button", { name: "@bob を招待する" }).click();
    expect((await sent).postDataJSON()).toEqual({
      login: "bob",
      userId: "user-bob",
      role: "editor",
    });
    await expect(page.getByRole("region", { name: "メンバー" })).toContainText("bob");
    expect(mock.members?.[BOARD_ID]).toHaveLength(1);
  });

  // 確認したあとで持ち主が変わった、またはその login を持つ人がいなくなった。
  // 見せている相手はもう招待できないので消し、もう一度確かめてもらう。
  for (const { name, status, code, error, message } of [
    {
      name: "持ち主が変わった",
      status: 409,
      code: "invitee_changed",
      error: "etoki: the login now belongs to a different user: bob",
      message: "この login の持ち主が変わりました",
    },
    {
      name: "持ち主がいなくなった",
      status: 400,
      code: "invalid_input",
      error: 'etoki: invalid input: "bob" has not signed in to etoki yet',
      message: "招待できませんでした",
    },
  ] as const) {
    test(`確認したあとで${name}ら、確認からやり直させる`, async ({ page }) => {
      const mock = baseMock();
      mock.inviteError = { status, body: { code, error } };
      await openBoardWithMock(page, mock);
      await openPanelTab(page, "メンバー");

      await page.getByLabel("招待する login").fill("bob");
      await page.getByRole("button", { name: "確認する" }).click();
      await page.getByRole("button", { name: "@bob を招待する" }).click();

      await expect(page.getByRole("alert")).toContainText(message);
      await expect(page.getByRole("group", { name: "招待する相手の確認" })).toHaveCount(
        0,
      );
    });
  }

  // 招待された側にリポジトリのアクセス権は要らない（ADR 0017）。ブレストには
  // 参加できて、作成だけができない。
  test("書き込み権限が無いと、作成の代わりに理由が出る", async ({ page }) => {
    const mock = baseMock();
    mock.access = {
      [BOARD_ID]: { status: 200, body: { role: "editor", projectAccess: "denied" } },
    };
    mock.details[BOARD_ID] = { ...board(), role: "editor" };

    await openBoardWithMock(page, mock);

    const card = annotationCard(page, "ログイン");
    const detail = annotationDetail(page, "ログイン");
    await interpret(card);

    // 解釈まではできる。GitHub は要らない。
    await expect(
      detail.getByText("ログインの入口まわりを 1 つの epic として読みました。"),
    ).toBeVisible();

    // 作成だけができない。押させずに理由を出す。
    await expect(detail.getByRole("button", { name: "GitHub に作成する" })).toHaveCount(
      0,
    );
    await expect(
      detail.getByText("この Project に書き込む権限がありません。"),
    ).toBeVisible();
  });

  // 作成先の Project に書けるかは、解釈まで進まなくても読める（#217）。
  // **書けないと分かったときだけ出す。** unknown（確かめていない・確かめられ
  // なかった）を「書けません」に見せない（ADR 0017）。
  for (const [projectAccess, shown] of [
    ["denied", true],
    ["allowed", false],
    ["unknown", false],
  ] as const) {
    test(`下の帯の「書けません」：${projectAccess}`, async ({ page }) => {
      const mock = baseMock();
      mock.access = {
        [BOARD_ID]: { status: 200, body: { role: "owner", projectAccess } },
      };
      const accessed = page.waitForResponse((r) =>
        /^\/api\/boards\/[^/]+\/access$/.test(new URL(r.url()).pathname),
      );
      await openBoardWithMock(page, mock);
      await accessed;

      const footer = page.locator(".board-context");
      // 作成先は Project まで書く。リポジトリ名だけでは作る先が決まらない。
      await expect(footer.locator(".badge-target")).toHaveText(
        "acme/web › #1 ロードマップ",
      );
      const denied = footer.getByText("書けません", { exact: true });
      if (shown) {
        await expect(denied).toBeVisible();
      } else {
        // 応答を受けてから 1 回描かれるのを待ち、出ていないことを見る。
        await page.evaluate(
          () => new Promise((resolve) => requestAnimationFrame(() => resolve(null))),
        );
        await expect(denied).toHaveCount(0);
      }
    });
  }

  test("viewer は編集も解釈もできない", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), role: "viewer" };
    mock.boards = mock.boards.map((b) => ({ ...b, role: "viewer" }));

    await openBoardWithMock(page, mock);

    await expect(
      page.getByText("読むだけの権限で開いています。編集・解釈・作成はできません。"),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "保存" })).toHaveCount(0);
    // 状態は読める。何が作成済みかは、読むだけの人にも見える必要がある。
    await expect(page.locator(".annotation").first()).toBeVisible();

    // 詳細は開ける（GitHub にあるものを読むため）が、解釈の口は無い。
    // **開いてから「無い」を見る。** 解釈の口は詳細の中にあるので、開かずに
    // 見ると出していても通る（#201）。
    const detail = await openAnnotationDetail(page, "パスワード再設定");
    await expect(
      detail.getByRole("button", { name: "GitHub にある 2 件" }),
    ).toBeVisible();
    await expect(detail.getByRole("button", { name: /^解釈/ })).toHaveCount(0);
    await expect(
      detail.getByText(
        "読むだけの権限で開いています。粒度と種別は変えられず、解釈と作成もできません。",
      ),
    ).toBeVisible();
  });

  test("オーナー以外には作成先の変更を出さない", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), role: "editor" };

    await openBoardWithMock(page, mock);

    // 口はメニューの中にある。**開いてから「無い」を見る。** 閉じたままだと
    // 出していても通る。
    const menu = await openBoardMenu(page);
    await expect(menu.getByRole("button", { name: "作成先を変更" })).toHaveCount(0);
    await expect(menu.getByText("作成先を変えられるのはオーナーだけです")).toBeVisible();
  });

  // ボードごと畳めるのは owner だけ（ADR 0042）。押せるのに 403 で断るより、
  // 押せないことを見せるほうが状態として正しい。**消したボタンの代わりに理由を
  // 出す**（ADR 0017 / 0030）ので、無いことと理由が出ていることを両方見る。
  test("オーナー以外には削除の導線を出さない", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), role: "editor" };

    await openBoardWithMock(page, mock);

    const menu = await openBoardMenu(page);
    await expect(menu.getByRole("button", { name: "ボードを削除" })).toHaveCount(0);
    await expect(menu.getByText("ボードを削除できるのはオーナーだけです")).toBeVisible();
    // 改名は editor にも許す。並べて見ることで、消えているのが削除だけだと
    // 分かる。**同じメニューの中で見る**ので、開いていなかった、では通らない。
    await expect(menu.getByRole("button", { name: "名前を変更" })).toBeVisible();
  });

  // メンバー一覧は owner でなくても見られる。誰と共有しているかを owner だけが
  // 知っている状態にすると、招待された側は自分が何に呼ばれたのか分からない。
  test("オーナー以外はメンバーを見られるが、招待欄は出ない", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), role: "editor" };
    mock.members = {
      [BOARD_ID]: [
        {
          userId: "user-alice",
          login: "alice",
          displayName: "Alice",
          role: "owner",
          createdAt: "2026-08-01T09:00:00Z",
        },
      ],
    };

    await openBoardWithMock(page, mock);
    await openPanelTab(page, "メンバー");

    await expect(page.getByRole("region", { name: "メンバー" })).toContainText("Alice");
    await expect(page.getByLabel("招待する login")).toHaveCount(0);
  });

  // 断られたら、まず打ち手を出す（#86）。サーバーの文言は捨てずに畳んでおく。
  // 「まだログインしていない」のような具体は、開けば読める。
  test("招待が断られたら理由を出す", async ({ page }) => {
    const mock = baseMock();
    mock.lookupInviteeError = {
      status: 400,
      body: {
        code: "invalid_input",
        error: 'etoki: invalid input: "carol" has not signed in to etoki yet',
      },
    };

    await openBoardWithMock(page, mock);
    await openPanelTab(page, "メンバー");

    await page.getByLabel("招待する login").fill("carol");
    await page.getByRole("button", { name: "確認する" }).click();

    const alert = page.getByRole("alert");
    await expect(alert).toContainText("招待する相手を確認できませんでした");
    await expect(alert.getByText("has not signed in")).toBeHidden();

    await alert.locator("summary").click();
    await expect(alert.getByText("has not signed in")).toBeVisible();
  });
});
