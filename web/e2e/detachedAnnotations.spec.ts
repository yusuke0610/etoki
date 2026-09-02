import { expect, test } from "@playwright/test";

import { installApi, type ApiMock } from "./helpers/api";
import { openBoard } from "./helpers/board";
import { baseMock, BOARD_ID } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";
const GONE_ID = "frame-gone";

/**
 * 注釈にした frame をキャンバスから消して保存したあと（#111）。
 *
 * **記録は消えていない**（ADR 0007）のに、画面から辿る導線だけが無かった。
 * GitHub には draft issue が残っているのに etoki からは存在しないことになる、
 * という ADR 0009 が避けたかった形。
 */
function withDetached(): ApiMock {
  const mock = baseMock();
  mock.detached[BOARD_ID] = [
    {
      id: GONE_ID,
      lastSyncedAt: "2026-08-04T12:00:00Z",
      items: [
        {
          itemId: "PVTI_gone",
          kind: "epic",
          title: "消した囲みで作った epic",
          body: "囲みは消えているが GitHub には残っている",
          localId: "e1",
          action: "created",
        },
      ],
    },
  ];
  return mock;
}

test.describe("キャンバスに無い注釈", () => {
  // 名前は取れない（シーンから消えている）。何の囲みだったかは、作ったものから
  // 読むしかない。
  test("作ったものから何の囲みだったかを辿れる", async ({ page }) => {
    await installApi(page, withDetached());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const section = page.locator(".panel-section").filter({
      has: page.getByRole("heading", { name: "キャンバスに無い注釈" }),
    });

    await expect(section.getByText("GitHub にある 1 件")).toBeVisible();
    await expect(section.getByText("消した囲みで作った epic")).toBeVisible();

    // **引き直しても繋がらない。** frame を引き直すと要素の ID が変わるので、
    // 以後は別の注釈として扱われる。書かないと「引き直せば戻る」と読める。
    await expect(
      section.getByText("囲みを引き直しても、これらとは繋がりません"),
    ).toBeVisible();
  });

  // 履歴の口はシーンに注釈が残っているかを見ない（ADR 0007）。導線だけが
  // 無かったので、そこから読めることまで確かめる。
  test("実行の履歴へ辿れる", async ({ page }) => {
    const mock = withDetached();
    mock.runs = {
      [GONE_ID]: {
        status: 200,
        body: [
          {
            id: 1,
            createdAt: "2026-08-04T12:00:00Z",
            items: [
              {
                itemId: "PVTI_gone",
                kind: "epic",
                title: "消した囲みで作った epic",
                body: "",
                localId: "e1",
                action: "created",
              },
            ],
          },
        ],
      },
    };

    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const section = page.locator(".panel-section").filter({
      has: page.getByRole("heading", { name: "キャンバスに無い注釈" }),
    });

    await section.getByText("実行の履歴").click();

    // **押されるまで引かない**（中核思想 3）。注釈のカードと同じ扱いにする。
    const request = page.waitForRequest(
      (r) =>
        r.method() === "GET" &&
        new URL(r.url()).pathname ===
          `/api/boards/${BOARD_ID}/annotations/${GONE_ID}/runs`,
    );
    await section.getByRole("button", { name: "履歴を読み込む" }).click();
    await request;

    // 履歴の中だけで引く。同じタイトルは上の畳み込みにも並ぶ。
    const history = section.locator(".run-history");
    await expect(history.getByText("消した囲みで作った epic")).toBeVisible();
  });

  // ふつうは空。常に空の枠が並ぶと、本当に何か残っているときに気づけない。
  test("消えた注釈が無ければ節ごと出さない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(
      page.getByRole("heading", { name: "キャンバスに無い注釈" }),
    ).toBeHidden();
  });

  // 3 状態も名前も無い。状態の一覧に混ぜると「押せない注釈」が並ぶ。
  test("状態の一覧には混ざらない", async ({ page }) => {
    await installApi(page, withDetached());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const states = page.locator(".panel-section").filter({
      has: page.getByRole("heading", { name: "状態" }),
    });

    await expect(states.getByText("消した囲みで作った epic")).toBeHidden();
    await expect(states.locator("li.annotation")).toHaveCount(3);
  });
});
