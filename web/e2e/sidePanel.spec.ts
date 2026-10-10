import { expect, test } from "@playwright/test";

import { openBoardWithMock, openPanelTab } from "./helpers/board";
import { BOARD_ID, baseMock } from "./helpers/fixtures";

/**
 * 右のパネル（`SidePanel`、ADR 0065）。「絵解いた」（注釈）と「etoki AI」（図の
 * ドラフト）をタブで並べる。メンバーは共有のダイアログ（`share.spec.ts`、#248）。
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

    const chat = await openPanelTab(page, "etoki AI");
    await chat.getByLabel("図への指示").fill("返金の分岐も足して");

    await openPanelTab(page, "絵解いた");
    await openPanelTab(page, "etoki AI");

    await expect(chat.getByLabel("図への指示")).toHaveValue("返金の分岐も足して");
  });

  // WAI-ARIA の tabs の形。矢印で隣のタブへ移り、そのまま開く。選ばれている
  // タブだけが Tab キーで止まる。
  test("矢印キーで隣のタブへ移り、そのタブが開く", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const annotations = page.getByRole("tab", { name: "絵解いた", exact: true });
    await annotations.focus();
    await page.keyboard.press("ArrowRight");

    const diagram = page.getByRole("tab", { name: "etoki AI", exact: true });
    await expect(diagram).toBeFocused();
    await expect(diagram).toHaveAttribute("aria-selected", "true");
    await expect(page.getByRole("tabpanel", { name: "etoki AI" })).toBeVisible();
    await expect(annotations).toHaveAttribute("tabindex", "-1");

    // 端から先は反対の端へ回る。
    await page.keyboard.press("ArrowRight");
    await expect(annotations).toBeFocused();
  });

  // 切れると: 注釈が 1 件も無いボードの「絵解いた」は案内の本文だけで、中に
  // フォーカスできるものが無い。タブから Tab キーを押しても、キーボードだけの人は
  // パネルの中身（案内）へ移れず、次の領域へ飛ぶ。
  test("フォーカスできるものが無いパネルも、Tab キーで移れる", async ({ page }) => {
    const mock = baseMock();
    mock.annotations = { [BOARD_ID]: [] };
    await openBoardWithMock(page, mock);

    const panel = page.getByRole("tabpanel", { name: "絵解いた", exact: true });
    await expect(panel).toBeVisible();
    await expect(panel.getByRole("button")).toHaveCount(0);

    await page.getByRole("tab", { name: "絵解いた", exact: true }).focus();
    // タブの列の右に「パネルを閉じる」がある（#202）。焦点の順は見た目の並びの
    // まま（タブ → 閉じる → 中身）にし、入れ替えない。
    await page.keyboard.press("Tab");
    await expect(page.getByRole("button", { name: "パネルを閉じる" })).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(panel).toBeFocused();
  });

  // 畳める（#202）。**畳んでも中身は外さない。** 外すと、図への指示の書きかけが
  // 畳んだだけで消える（タブを切り替えたときと同じ理由）。
  test("畳むと帯が残り、開き直すと書きかけが残っている", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const chat = await openPanelTab(page, "etoki AI");
    await chat.getByLabel("図への指示").fill("返金の分岐も足して");

    await page.getByRole("button", { name: "パネルを閉じる" }).click();
    await expect(page.getByRole("tablist", { name: "パネル" })).toBeHidden();
    const rail = page.getByRole("navigation", { name: "パネル" });
    await expect(rail).toBeVisible();
    // 畳んだら焦点は帯の「いまのタブ」へ。押したボタンが消えるので、移さないと
    // body に落ちる。
    await expect(rail.getByRole("button", { name: "etoki AI" })).toBeFocused();

    await rail.getByRole("button", { name: "etoki AI" }).click();
    await expect(rail).toBeHidden();
    await expect(page.getByRole("tab", { name: "etoki AI", exact: true })).toBeFocused();
    await expect(chat.getByLabel("図への指示")).toHaveValue("返金の分岐も足して");
  });

  // 畳んでいるあいだも状態を隠さない（ADR 0064）。添えるのは手を打つ必要が
  // あるものだけで、作成済みは出さない。**名前まで見る。** 縦書きの見た目の
  // 区切りでは、読み上げに件数の切れ目が伝わらない。
  test("畳んだ帯の「絵解いた」に、未作成と変更ありの件数が出る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await page.getByRole("button", { name: "パネルを閉じる" }).click();

    const rail = page.getByRole("navigation", { name: "パネル" });
    await expect(
      rail.getByRole("button", {
        name: "絵解いた、未作成 1 件、変更あり 1 件",
        exact: true,
      }),
    ).toBeVisible();
    await expect(rail.getByText("作成済み")).toHaveCount(0);
    // 件数の無いタブは名前だけ。
    await expect(
      rail.getByRole("button", { name: "etoki AI", exact: true }),
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
