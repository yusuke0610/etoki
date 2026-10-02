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

  // 畳める（#202）。**畳んでも中身は外さない。** 外すと、図への指示の書きかけが
  // 畳んだだけで消える（タブを切り替えたときと同じ理由）。
  test("畳むと帯が残り、開き直すと書きかけが残っている", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const chat = await openPanelTab(page, "図のドラフト");
    await chat.getByLabel("図への指示").fill("返金の分岐も足して");

    await page.getByRole("button", { name: "パネルを閉じる" }).click();
    await expect(page.getByRole("tablist", { name: "パネル" })).toBeHidden();
    const rail = page.getByRole("navigation", { name: "パネル" });
    await expect(rail).toBeVisible();
    // 畳んだら焦点は帯の「いまのタブ」へ。押したボタンが消えるので、移さないと
    // body に落ちる。
    await expect(rail.getByRole("button", { name: "図のドラフト" })).toBeFocused();

    await rail.getByRole("button", { name: "図のドラフト" }).click();
    await expect(rail).toBeHidden();
    await expect(
      page.getByRole("tab", { name: "図のドラフト", exact: true }),
    ).toBeFocused();
    await expect(chat.getByLabel("図への指示")).toHaveValue("返金の分岐も足して");
  });

  // 畳んでいるあいだも状態を隠さない（ADR 0064）。添えるのは手を打つ必要が
  // あるものだけで、作成済みは出さない。**名前まで見る。** 縦書きの見た目の
  // 区切りでは、読み上げに件数の切れ目が伝わらない。
  test("畳んだ帯の注釈に、未作成と変更ありの件数が出る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await page.getByRole("button", { name: "パネルを閉じる" }).click();

    const rail = page.getByRole("navigation", { name: "パネル" });
    await expect(
      rail.getByRole("button", { name: "注釈、未作成 1 件、変更あり 1 件", exact: true }),
    ).toBeVisible();
    await expect(rail.getByText("作成済み")).toHaveCount(0);
    // 件数の無いタブは名前だけ。
    await expect(
      rail.getByRole("button", { name: "メンバー", exact: true }),
    ).toBeVisible();
  });

  // 開閉は端末ごとに覚え、ボードをまたいで同じにする。
  test("読み込み直しても畳んだまま", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await page.getByRole("button", { name: "パネルを閉じる" }).click();

    await page.reload();
    await expect(page.getByRole("navigation", { name: "パネル" })).toBeVisible();
    await expect(page.getByRole("tablist", { name: "パネル" })).toBeHidden();
  });

  // サイトデータを遮断した環境では、`localStorage` に触れただけで投げる。開閉は
  // 好みでしかないので、そのせいで画面を落とさず、開いた状態で始める。
  test("保存先が使えなくても、開いた状態で始まり畳める", async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(window, "localStorage", {
        get() {
          throw new Error("denied");
        },
      });
    });
    await openBoardWithMock(page, baseMock());

    await expect(page.getByRole("tablist", { name: "パネル" })).toBeVisible();
    await page.getByRole("button", { name: "パネルを閉じる" }).click();
    await expect(page.getByRole("navigation", { name: "パネル" })).toBeVisible();
  });
});
