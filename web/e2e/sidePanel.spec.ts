import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { openBoard, openBoardWithMock, openPanelTab } from "./helpers/board";
import { BOARD_NAME, baseMock } from "./helpers/fixtures";

/**
 * 右のパネル（`SidePanel`、ADR 0065）。注釈・図のドラフト・メンバーをタブで並べる。
 *
 * **守りたいのは「切り替えても手元の作業が消えない」こと。** Excalidraw の
 * `Sidebar` に載せなかった理由がそれで（閉じるとタブの中身ごと外れる）、1 度
 * 開いたタブは描いたまま隠している。ここが崩れると、図への指示の書きかけが
 * タブを切り替えただけで消える。
 *
 * 下書きの手直しはタブではなく注釈の詳細に出る。消えないことはそちらで見る
 * （`annotationDetail.spec.ts`）。
 */
test.describe("右のパネル", () => {
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
});
