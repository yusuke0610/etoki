import { expect, test } from "@playwright/test";

import { installApi, summarize } from "./helpers/api";
import { annotationCard, openBoard } from "./helpers/board";
import { BOARD_ID, baseMock, board } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

/** 作成先の URL を控えていないボード。URL を保存する前に選んだものが該当する。 */
function withoutProjectUrl() {
  const mock = baseMock();
  const detail = { ...board(), projectUrl: "" };

  mock.details[BOARD_ID] = detail;
  mock.boards = [summarize(detail)];

  return mock;
}

// draft issue の作成は取り消せない（ADR 0009）。取り消せない操作の結果を
// 確かめられないと、「途中で失敗しました」と出たときに何が作られたのかを
// 見にいけない。その導線が壊れていないことを href で固定する（ADR 0025）。
test.describe("GitHub へ辿る導線", () => {
  test("ヘッダの作成先バッジが Project へのリンクになる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // 番号から組み立てず、保存された URL をそのまま使う。owner が user か
    // org かで形が変わり、etoki はどちらなのかを知らない。
    await expect(page.locator(".badge-target")).toHaveAttribute(
      "href",
      "https://github.com/orgs/acme/projects/1",
    );
  });

  test("URL を控えていないボードはリポジトリの Projects へ落ちる", async ({ page }) => {
    await installApi(page, withoutProjectUrl());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // 番号（1）は持っているが、そこからは組み立てない。組み立てると owner の
    // 種別を当てにいくことになり、外すと 404 になる。
    await expect(page.locator(".badge-target")).toHaveAttribute(
      "href",
      "https://github.com/acme/web/projects",
    );
  });

  test("前回作成したぶんから Project へ飛べる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "パスワード再設定");
    await card.getByText("前回作成した 2 件").click();

    await expect(
      card.getByRole("link", { name: "GitHub でこの Project を開く" }),
    ).toHaveAttribute("href", "https://github.com/orgs/acme/projects/1");
  });

  test("作成結果から Project へ飛べる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).click();
    await expect(card.getByText("3 件を作成しました。")).toBeVisible();

    await expect(card.locator(".creation-result").getByRole("link")).toHaveAttribute(
      "href",
      "https://github.com/orgs/acme/projects/1",
    );
  });

  // Project そのものに着地しないなら、そう書く。リポジトリの Projects まで
  // しか辿れないのに「Project を開く」と言うと、リンクの約束が崩れる。
  test("一覧止まりのときは飛び先をそう書く", async ({ page }) => {
    await installApi(page, withoutProjectUrl());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "パスワード再設定");
    await card.getByText("前回作成した 2 件").click();

    await expect(
      card.getByRole("link", { name: "GitHub でリポジトリの Projects を開く" }),
    ).toBeVisible();
  });
});
