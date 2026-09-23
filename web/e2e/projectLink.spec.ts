import { expect, test } from "@playwright/test";

import { installApi, summarize } from "./helpers/api";
import { annotationCard, openBoard } from "./helpers/board";
import { BOARD_ID, baseMock, board, matchedInterpretationMock } from "./helpers/fixtures";

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

  test("GitHub にある項目から Project へ飛べる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "パスワード再設定");
    await card.getByText("GitHub にある 2 件").click();

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

    await expect(
      card
        .locator(".creation-result")
        .getByRole("link", { name: "GitHub でこの Project を開く" }),
    ).toHaveAttribute("href", "https://github.com/orgs/acme/projects/1");
  });

  // Project そのものに着地しないなら、そう書く。リポジトリの Projects まで
  // しか辿れないのに「Project を開く」と言うと、リンクの約束が崩れる。
  test("一覧止まりのときは飛び先をそう書く", async ({ page }) => {
    await installApi(page, withoutProjectUrl());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "パスワード再設定");
    await card.getByText("GitHub にある 2 件").click();

    await expect(
      card.getByRole("link", { name: "GitHub でリポジトリの Projects を開く" }),
    ).toBeVisible();
  });

  // item ごとのリンク（ADR 0057）。「途中で失敗しました（3 件は作成済み）」の
  // ときに知りたいのは「どの 3 件か」で、Project 全体では答えにならない。
  test.describe("item ごとのリンク", () => {
    test("GitHub にある項目から 1 件ずつ開ける", async ({ page }) => {
      await installApi(page, baseMock());
      await page.goto("/");
      await openBoard(page, BOARD_NAME);

      const card = annotationCard(page, "パスワード再設定");
      await card.getByText("GitHub にある 2 件").click();

      // 同じ文言のリンクが行の数だけ並ぶので、名前にタイトルを含める。
      await expect(
        card.getByRole("link", { name: "「パスワード再設定」を GitHub で開く" }),
      ).toHaveAttribute(
        "href",
        "https://github.com/orgs/acme/projects/1?pane=issue&itemId=101",
      );

      // 識別子を控えていなかった頃の item にはリンクを出さない。番号や node ID
      // から推測して組まない。
      await expect(
        card.getByRole("link", { name: "「再設定メールを送る」を GitHub で開く" }),
      ).toHaveCount(0);

      // リストごとの 1 本は残る。Project 全体を見にいくのは別の用事。
      await expect(
        card.getByRole("link", { name: "GitHub でこの Project を開く" }),
      ).toBeVisible();
    });

    test("作成結果から 1 件ずつ開ける", async ({ page }) => {
      await installApi(page, baseMock());
      await page.goto("/");
      await openBoard(page, BOARD_NAME);

      const card = annotationCard(page, "ログイン");
      await card.getByRole("button", { name: "解釈する" }).click();
      await card.getByRole("button", { name: "GitHub に作成する" }).click();
      await expect(card.getByText("3 件を作成しました。")).toBeVisible();

      const result = card.locator(".creation-result");
      for (const [title, id] of [
        ["ログイン基盤", 201],
        ["メールとパスワードでログインする", 202],
        ["ログイン失敗を数える", 203],
      ] as const) {
        await expect(
          result.getByRole("link", { name: `「${title}」を GitHub で開く` }),
        ).toHaveAttribute(
          "href",
          `https://github.com/orgs/acme/projects/1?pane=issue&itemId=${id}`,
        );
      }
    });

    // 押す前に「GitHub 側にそのまま残ります」と見せている item を、その場で
    // 見にいけるようにする。
    test("取り残しの予告から開ける", async ({ page }) => {
      await installApi(page, matchedInterpretationMock());
      await page.goto("/");
      await openBoard(page, BOARD_NAME);

      const card = annotationCard(page, "セッション管理");
      await card.getByRole("button", { name: "解釈する" }).click();

      await expect(
        card
          .locator(".left-behind")
          .getByRole("link", { name: "「触らないほう」を GitHub で開く" }),
      ).toHaveAttribute(
        "href",
        "https://github.com/orgs/acme/projects/1?pane=issue&itemId=88",
      );
    });

    // Project の URL を知らないなら、土台を組み立て直さない（ADR 0025）。
    test("URL を控えていないボードでは item ごとのリンクを出さない", async ({ page }) => {
      await installApi(page, withoutProjectUrl());
      await page.goto("/");
      await openBoard(page, BOARD_NAME);

      const card = annotationCard(page, "パスワード再設定");
      await card.getByText("GitHub にある 2 件").click();

      await expect(card.getByRole("link", { name: /を GitHub で開く$/ })).toHaveCount(0);
    });
  });
});
