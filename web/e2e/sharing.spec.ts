import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { annotationCard, openBoard } from "./helpers/board";
import { BOARD_ID, baseMock, board } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

test.describe("共有", () => {
  test("オーナーは招待でき、招待した相手が一覧に並ぶ", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await page.getByRole("button", { name: "メンバー", exact: true }).click();

    // 招待できる相手の条件は、失敗してから知らせるのでは遅い。
    await expect(
      page.getByText("招待できるのは、一度 etoki にログインしたことがある人だけです。"),
    ).toBeVisible();

    await page.getByLabel("招待する login").fill("bob");
    await page.getByLabel("招待するロール").selectOption("editor");
    await page.getByRole("button", { name: "招待" }).click();

    await expect(page.getByRole("region", { name: "メンバー" })).toContainText("bob");
  });

  // 招待された側にリポジトリのアクセス権は要らない（ADR 0017）。ブレストには
  // 参加できて、作成だけができない。
  test("書き込み権限が無いと、作成の代わりに理由が出る", async ({ page }) => {
    const mock = baseMock();
    mock.access = {
      [BOARD_ID]: { status: 200, body: { role: "editor", projectAccess: "denied" } },
    };
    mock.details[BOARD_ID] = { ...board(), role: "editor" };

    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    // 解釈まではできる。GitHub は要らない。
    await expect(
      card.getByText("ログインの入口まわりを 1 つの epic として読みました。"),
    ).toBeVisible();

    // 作成だけができない。押させずに理由を出す。
    await expect(card.getByRole("button", { name: "GitHub に作成する" })).toHaveCount(0);
    await expect(
      card.getByText("この Project に書き込む権限がありません。"),
    ).toBeVisible();
  });

  test("viewer は編集も解釈もできない", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), role: "viewer" };
    mock.boards = mock.boards.map((b) => ({ ...b, role: "viewer" }));

    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(
      page.getByText("読むだけの権限で開いています。編集・解釈・作成はできません。"),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "保存" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "解釈する" })).toHaveCount(0);
    // 状態は読める。何が作成済みかは、読むだけの人にも見える必要がある。
    await expect(page.locator(".annotation").first()).toBeVisible();
  });

  test("オーナー以外には作成先の変更を出さない", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), role: "editor" };

    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(page.getByRole("button", { name: "作成先を変更" })).toHaveCount(0);
    await expect(
      page.getByText("作成先を変えられるのはオーナーだけです"),
    ).toBeVisible();
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

    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await page.getByRole("button", { name: "メンバー", exact: true }).click();

    await expect(page.getByRole("region", { name: "メンバー" })).toContainText("Alice");
    await expect(page.getByLabel("招待する login")).toHaveCount(0);
  });

  // 断られた理由はサーバーの文言をそのまま出す。次に何をすればよいかが
  // 決められるのは、理由が読めたときだけ。
  test("招待が断られたら理由を出す", async ({ page }) => {
    const mock = baseMock();
    mock.inviteError = {
      status: 400,
      body: { error: 'etoki: invalid input: "carol" has not signed in to etoki yet' },
    };

    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await page.getByRole("button", { name: "メンバー", exact: true }).click();

    await page.getByLabel("招待する login").fill("carol");
    await page.getByRole("button", { name: "招待" }).click();

    await expect(page.getByRole("alert")).toContainText("has not signed in");
  });
});
