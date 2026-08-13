import { expect, test } from "@playwright/test";

import { installApi, summarize } from "./helpers/api";
import { chooseTarget, openBoard } from "./helpers/board";
import { baseMock, board, unselectedBoard } from "./helpers/fixtures";

test.describe("ボード", () => {
  test("一覧から選ぶとキャンバスと注釈パネルが開く", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");

    await expect(
      page.getByText("左からボードを選ぶか、新しく作成してください。"),
    ).toBeVisible();

    await openBoard(page, "認証まわりのブレスト");
    await expect(page.getByRole("heading", { name: "選択中のフレーム" })).toBeVisible();
  });

  // 作成先を選ぶまでボードは作られない。書ける Project を 1 つも持たない人は
  // ここで先に進めず、それが「作成にはリポジトリへのアクセス権が要る」ことの
  // 表れになる（ADR 0017）。
  test("名前を入れて作成先を選ぶと、そのボードが開く", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");

    const name = page.getByLabel("ボード名");
    const submit = page.getByRole("button", { name: "次へ" });

    // 空のまま進ませない。誤って空名のボードが増えるのを防いでいる。
    await expect(submit).toBeDisabled();

    await name.fill("決済フローのブレスト");
    await expect(submit).toBeEnabled();
    await submit.click();

    // まだ作られていない。先に作成先を選ばせる。
    await expect(page.getByRole("heading", { name: "リポジトリ" })).toBeVisible();
    await expect(
      page.locator(".board-list").getByRole("button", { name: "決済フローのブレスト" }),
    ).toHaveCount(0);

    await chooseTarget(page, "acme/web", "#1 ロードマップ");

    await expect(
      page.getByRole("heading", { name: "決済フローのブレスト", level: 1 }),
    ).toBeVisible();
    // 作成したら入力欄は空に戻り、一覧にも並ぶ。
    await expect(name).toHaveValue("");
    await expect(
      page.locator(".board-list").getByRole("button", { name: "決済フローのブレスト" }),
    ).toBeVisible();
  });

  // 作成先はボードの属性なので、開くまで分からないと取り違えたまま作成に
  // 進める。一覧をリポジトリ → Project でまとめて見せる（ADR 0019）。
  test("一覧は作成先ごとにまとまり、未選択は末尾に出る", async ({ page }) => {
    const mock = baseMock();
    const other = {
      ...board(),
      id: "board-other",
      name: "別プロジェクトのブレスト",
      projectId: "PVT_2",
      projectNumber: 4,
      projectTitle: "技術的負債",
    };
    const legacy = unselectedBoard();

    mock.boards = [summarize(other), ...mock.boards, summarize(legacy)];
    mock.details[other.id] = other;
    mock.details[legacy.id] = legacy;
    mock.annotations[other.id] = [];
    mock.annotations[legacy.id] = [];

    await installApi(page, mock);
    await page.goto("/");

    const tree = page.locator(".board-tree");
    // 枝はリポジトリ、その下が Project、その下がボード。同じリポジトリの
    // Project 違いは 1 つの枝にまとまる。
    await expect(tree.getByRole("button", { name: "acme/web" })).toHaveCount(1);
    await expect(
      tree.getByRole("button", { name: "#4 技術的負債", exact: true }),
    ).toBeVisible();

    // 作成先が未選択なのは移行前のボードだけ。末尾にまとめる。
    // 三角は装飾なので読み上げ名には出ない。名前で確かめる。
    const branches = tree.getByRole("button", { expanded: true });
    await expect(branches.last()).toHaveAccessibleName("作成先なし");
    await expect(tree.getByRole("button", { name: legacy.name })).toBeVisible();
  });

  // 既定は開いた状態。畳むのは利用者が押したときだけ（中核思想 3）。
  test("枝を畳むとボードが隠れ、もう一度押すと戻る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");

    const tree = page.locator(".board-tree");
    const branch = tree.getByRole("button", { name: "acme/web" });
    const boardButton = tree.getByRole("button", { name: "認証まわりのブレスト" });

    await expect(branch).toHaveAttribute("aria-expanded", "true");
    await expect(boardButton).toBeVisible();

    await branch.click();
    await expect(branch).toHaveAttribute("aria-expanded", "false");
    await expect(boardButton).toHaveCount(0);

    await branch.click();
    await expect(boardButton).toBeVisible();
  });

  test("一覧の取得に失敗したらエラーを出し、閉じられる", async ({ page }) => {
    const mock = baseMock();
    mock.boardsError = { status: 500, body: { error: "internal error" } };
    await installApi(page, mock);

    await page.goto("/");

    const alert = page.getByRole("alert");
    await expect(alert).toContainText("ボード一覧を取得できませんでした");
    await alert.getByRole("button", { name: "閉じる" }).click();
    await expect(alert).toBeHidden();
  });
});
