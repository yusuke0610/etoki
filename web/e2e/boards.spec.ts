import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { openBoard } from "./helpers/board";
import { baseMock } from "./helpers/fixtures";

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

    await page.getByRole("button", { name: "acme/web" }).click();
    await page.getByRole("button", { name: "#1 ロードマップ" }).click();

    await expect(
      page.getByRole("heading", { name: "決済フローのブレスト", level: 1 }),
    ).toBeVisible();
    // 作成したら入力欄は空に戻り、一覧にも並ぶ。
    await expect(name).toHaveValue("");
    await expect(
      page.locator(".board-list").getByRole("button", { name: "決済フローのブレスト" }),
    ).toBeVisible();
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
