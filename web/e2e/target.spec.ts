import { expect, test, type Page } from "@playwright/test";

import { installApi, summarize } from "./helpers/api";
import { chooseTarget, drawRectangle, openBoard, picker } from "./helpers/board";
import { BOARD_ID, baseMock, board, unselectedBoard } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";
const UNSELECTED_NAME = "作成先未選択のブレスト";

/** 作成先が未選択のボードを 1 枚足したモック。 */
function withUnselected() {
  const mock = baseMock();
  const board = unselectedBoard();

  mock.boards = [summarize(board), ...mock.boards];
  mock.details[board.id] = board;
  mock.annotations[board.id] = [];

  return mock;
}

/** サイドバーからボードを開く。キャンバスが出ないので openBoard は使えない。 */
async function openUnselected(page: Page): Promise<void> {
  await page
    .locator(".board-list")
    .getByRole("button", { name: UNSELECTED_NAME })
    .click();
  await expect(
    page.getByRole("heading", { name: UNSELECTED_NAME, level: 1 }),
  ).toBeVisible();
}

test.describe("作成先の選択", () => {
  // 「ボードに入る → 対象リポジトリ選択 → ブレスト開始」。選ぶまでキャンバスを
  // 出さない。既存 DB からの移行もこの入口を通る（ADR 0014）。
  test("作成先が未選択なら、キャンバスの前にリポジトリ選択が出る", async ({ page }) => {
    await installApi(page, withUnselected());
    await page.goto("/");
    await openUnselected(page);

    await expect(page.getByRole("heading", { name: "リポジトリ" })).toBeVisible();
    await expect(page.locator(".excalidraw canvas")).toHaveCount(0);
  });

  // 利用者が選ぶのはリポジトリだが、保存するのはそこに紐づく Project。
  // draft issue はリポジトリではなく Project に属するため 2 段になる。
  test("リポジトリを選ぶと、そのリポジトリのプロジェクトが出る", async ({ page }) => {
    await installApi(page, withUnselected());
    await page.goto("/");
    await openUnselected(page);

    await picker(page)
      .getByRole("button", { name: /acme\/web/ })
      .click();

    await expect(
      page.getByRole("heading", { name: "acme/web のプロジェクト" }),
    ).toBeVisible();
    await expect(
      picker(page).getByRole("button", { name: "#1 ロードマップ" }),
    ).toBeVisible();
    await expect(
      picker(page).getByRole("button", { name: "#4 技術的負債" }),
    ).toBeVisible();
  });

  test("プロジェクトを選ぶとブレストに進み、作成先が見えている", async ({ page }) => {
    await installApi(page, withUnselected());
    await page.goto("/");
    await openUnselected(page);

    await chooseTarget(page, /acme\/web/, "#1 ロードマップ");

    await expect(page.locator(".excalidraw canvas").first()).toBeVisible();
    // どこに作られるのかは常に見えている必要がある。作った draft issue は
    // 取り消せない。
    await expect(page.locator(".badge-target")).toHaveText("acme/web");
  });

  // 1 件しか無くても自動で確定しない。作成先は取り返しがつかないので、
  // システムが決めず開発者に選ばせる（中核思想 3）。
  test("プロジェクトが 1 件でも自動では選ばれない", async ({ page }) => {
    const mock = withUnselected();
    mock.projects["acme/web"] = {
      status: 200,
      body: [
        {
          id: "PVT_only",
          number: 1,
          title: "唯一のプロジェクト",
          url: "https://github.com/orgs/acme/projects/1",
        },
      ],
    };

    await installApi(page, mock);
    await page.goto("/");
    await openUnselected(page);
    await picker(page)
      .getByRole("button", { name: /acme\/web/ })
      .click();

    await expect(
      page.getByRole("button", { name: "#1 唯一のプロジェクト" }),
    ).toBeVisible();
    await expect(page.locator(".excalidraw canvas")).toHaveCount(0);
  });

  // 権限不足と「本当に 1 つも無い」は API からは区別できない。両方書く。
  test("リポジトリが 0 件なら、権限を確かめるよう案内する", async ({ page }) => {
    const mock = withUnselected();
    mock.repositories = { status: 200, body: [] };

    await installApi(page, mock);
    await page.goto("/");
    await openUnselected(page);

    await expect(page.getByText("リポジトリが 1 つも見つかりませんでした")).toBeVisible();
  });

  test("リポジトリの取得に失敗したら、選択画面にその旨を出す", async ({ page }) => {
    const mock = withUnselected();
    mock.repositories = {
      status: 502,
      body: { code: "github_unavailable", error: "github api: 401: Bad credentials" },
    };

    await installApi(page, mock);
    await page.goto("/");
    await openUnselected(page);

    await expect(page.getByRole("alert")).toContainText(
      "リポジトリを取得できませんでした",
    );
  });

  // 選択画面に移るとキャンバスごと外れ、未保存の編集は失われる。黙って捨てない。
  test("未保存の変更があるうちは作成先を変更できない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const change = page.getByRole("button", { name: "作成先を変更" });
    await expect(change).toBeEnabled();

    await drawRectangle(page);

    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
    await expect(change).toBeDisabled();
    // 押せない理由は本文として出す。title に隠すと、ホバーできない利用者と
    // 読み上げには届かない（disabled なボタンにはフォーカスも当たらない）。
    await expect(page.getByText("保存してから作成先を変更できます")).toBeVisible();

    await page.getByRole("button", { name: "保存" }).click();
    await expect(change).toBeEnabled();
  });

  // 固定済みでも API を直接叩けば 409 が返る。画面はその理由を黙って
  // 飲み込まず、選択画面に出す。
  test("設定が拒否されたら、選択画面に理由を出す", async ({ page }) => {
    const mock = withUnselected();
    mock.setTargetError = {
      status: 409,
      body: {
        code: "target_locked",
        error: "etoki: board target is locked: board-unselected",
      },
    };

    await installApi(page, mock);
    await page.goto("/");
    await openUnselected(page);

    await chooseTarget(page, /acme\/web/, "#1 ロードマップ");

    await expect(page.getByRole("alert")).toContainText("作成先を設定できませんでした");
    // 失敗したのだからブレストには進ませない。
    await expect(page.locator(".excalidraw canvas")).toHaveCount(0);
  });

  test("固定前は作成先を選び直せる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await page.getByRole("button", { name: "作成先を変更" }).click();
    await expect(page.getByRole("heading", { name: "リポジトリ" })).toBeVisible();

    await picker(page)
      .getByRole("button", { name: /acme\/api/ })
      .click();
    await expect(page.getByText("紐づく Projects v2 がありません")).toBeVisible();

    // やめればブレストに戻れる。未選択のときは戻る先が無いので出さない。
    await page.getByRole("button", { name: "やめる" }).click();
    await expect(page.locator(".excalidraw canvas").first()).toBeVisible();
  });

  // draft issue を 1 件でも作ると固定される。押せるのに 409 で断るより、
  // 押せないことを見せるほうが状態として正しい。
  test("固定済みなら変更ボタンを出さない", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), targetLocked: true };

    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(page.getByRole("button", { name: "作成先を変更" })).toHaveCount(0);
    // 確定していることだけでなく、なぜ確定なのかも本文で読める必要がある。
    await expect(page.getByText("作成先は確定（draft issue を作成済み）")).toBeVisible();
  });

  test("新しいボードは作成先の選択から始まる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");

    await page.getByLabel("ボード名").fill("決済まわり");
    await page.getByRole("button", { name: "次へ" }).click();

    await expect(page.getByRole("heading", { name: "リポジトリ" })).toBeVisible();
    await expect(page.locator(".excalidraw canvas")).toHaveCount(0);
  });
});
