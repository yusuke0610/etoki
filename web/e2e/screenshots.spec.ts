import { test, type Page } from "@playwright/test";

import { installApi, summarize } from "./helpers/api";
import { annotationCard, drawRectangle, openBoard, picker } from "./helpers/board";
import {
  BOARD_ID,
  baseMock,
  board,
  multiFrameMock,
  signedIn,
  unselectedBoard,
} from "./helpers/fixtures";

/**
 * 主要な画面のスクリーンショットを撮る。
 *
 * 失敗時の証跡ではなく、変更を報告するときに添える成功時の画像を作るのが目的。
 * 期待値を持たないので、これ自体はリグレッションを検知しない。画面の約束を
 * 固定するのは他の spec の仕事。
 *
 * 出力先: web/e2e-output/screenshots/（gitignore 済み）
 */

const SHOT_DIR = "e2e-output/screenshots";
const BOARD_NAME = "認証まわりのブレスト";

async function shot(page: Page, name: string): Promise<void> {
  await page.screenshot({ path: `${SHOT_DIR}/${name}.png`, fullPage: false });
}

test.describe("スクリーンショット", () => {
  test("主要な画面を撮る", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await shot(page, "01-boards-empty");

    await openBoard(page, BOARD_NAME);
    await shot(page, "02-board-states");

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).waitFor();
    await shot(page, "03-interpretation");

    mock.annotations[BOARD_ID] = [
      {
        id: "frame-uncreated",
        name: "ログイン",
        granularity: "",
        state: "created",
        lastSyncedAt: "2026-08-05T10:00:00Z",
      },
    ];
    await card.getByRole("button", { name: "GitHub に作成する" }).click();
    await card.getByText("3 件を作成しました。").waitFor();
    await shot(page, "04-created");
  });

  // 複数フレームのとき、パネルの項目とキャンバスのフレームが対応して見える
  // ことを撮る（ADR 0021）。
  test("カードとフレームの対応を撮る", async ({ page }) => {
    await installApi(page, multiFrameMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await annotationCard(page, "注釈 2").getByRole("button", { name: "注釈 2" }).click();
    // 寄せる動きはアニメーションする。終わる前に撮ると、途中の位置が写る。
    await page.waitForTimeout(1000);
    await shot(page, "15-frame-focused");
  });

  // 未保存のあいだは解釈できない。テキストは保存済みシーンから、画像は画面から
  // 取るので、揃っていないと入力が食い違う（ADR 0018）。
  test("未保存で解釈できない状態を撮る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);

    await annotationCard(page, "ログイン")
      .getByText("保存してから解釈できます", { exact: false })
      .waitFor();
    await shot(page, "05-interpret-blocked");
  });

  // ブレストに入る前の画面。作成先を選ばないとキャンバスが出ない（ADR 0014）。
  test("作成先の選択を撮る", async ({ page }) => {
    const mock = baseMock();
    const board = unselectedBoard();
    mock.boards = [summarize(board)];
    mock.details = { [board.id]: board };
    mock.annotations = { [board.id]: [] };

    await installApi(page, mock);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await page.locator(".board-list").getByRole("button", { name: board.name }).click();
    await page.getByRole("heading", { name: "リポジトリ" }).waitFor();
    await shot(page, "05-target-repositories");

    await picker(page)
      .getByRole("button", { name: /acme\/web/ })
      .click();
    await page.getByRole("button", { name: "#1 ロードマップ" }).waitFor();
    await shot(page, "06-target-projects");

    await picker(page).getByRole("button", { name: "#1 ロードマップ" }).click();
    await page.locator(".badge-target").waitFor();
    // バッジはキャンバスより先に出る。ここで待たないと Excalidraw の
    // 「Loading scene...」を撮ってしまい、報告に使えない画像になる。
    await page.locator(".excalidraw canvas").first().waitFor();
    await page.getByRole("heading", { name: "注釈" }).waitFor();
    await shot(page, "07-target-selected");
  });

  // 押せない理由は title ではなく本文で出す。ホバーできない利用者と読み上げにも
  // 届く必要がある。見た目の話でもあるので撮る。
  test("作成先を変更できない状態を撮る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);
    await page.getByText("未保存", { exact: true }).waitFor();
    await shot(page, "08-target-change-blocked");
  });

  // 共有の画面。誰と共有しているか、自分に何ができるかが見えている必要がある
  // （ADR 0017）。
  test("メンバーの一覧を撮る", async ({ page }) => {
    const mock = baseMock();
    mock.members = {
      [BOARD_ID]: [
        {
          userId: "user-alice",
          login: "alice",
          displayName: "Alice",
          role: "owner",
          createdAt: "2026-08-01T09:00:00Z",
        },
        {
          userId: "user-bob",
          login: "bob",
          displayName: "Bob",
          role: "editor",
          createdAt: "2026-08-03T09:00:00Z",
        },
        {
          userId: "user-carol",
          login: "carol",
          displayName: "Carol",
          role: "viewer",
          createdAt: "2026-08-04T09:00:00Z",
        },
      ],
    };

    await installApi(page, mock);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await page.getByRole("button", { name: "メンバー", exact: true }).click();
    await page.getByText("Carol").waitFor();
    await shot(page, "11-members");
  });

  // 招待された側にリポジトリのアクセス権は要らない。ブレストと解釈まではでき、
  // 作成だけができない。**その理由が読める**ことを撮る。
  test("作成の権限が無い状態を撮る", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), role: "editor" };
    mock.access = {
      [BOARD_ID]: { status: 200, body: { role: "editor", projectAccess: "denied" } },
    };

    await installApi(page, mock);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await annotationCard(page, "ログイン")
      .getByRole("button", { name: "解釈する" })
      .click();
    await page.getByText("この Project に書き込む権限がありません。").waitFor();
    await shot(page, "12-creation-denied");
  });

  // 読むだけの参加者。編集・解釈・作成の導線がまとめて消える。
  test("読むだけの権限で開いた画面を撮る", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), role: "viewer" };
    mock.boards = mock.boards.map((b) => ({ ...b, role: "viewer" }));

    await installApi(page, mock);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await page
      .getByText("読むだけの権限で開いています。編集・解釈・作成はできません。")
      .waitFor();
    await shot(page, "13-viewer");
  });

  // 一覧をリポジトリと Project でまとめた姿（ADR 0019）。ボードが 1 枚の
  // 01 では枝が 1 本しか出ず、まとまっていることが見えない。
  test("作成先ごとにまとまった一覧を撮る", async ({ page }) => {
    const mock = baseMock();
    const another = {
      ...board(),
      id: "board-another",
      name: "課金まわりのブレスト",
      projectId: "PVT_2",
      projectNumber: 4,
      projectTitle: "技術的負債",
    };
    const otherRepo = {
      ...board(),
      id: "board-api",
      name: "配信基盤の棚卸し",
      repositoryName: "api",
      projectId: "PVT_3",
      projectNumber: 2,
      projectTitle: "インフラ",
    };
    const legacy = unselectedBoard();

    mock.boards = [
      summarize(otherRepo),
      summarize(another),
      ...mock.boards,
      summarize(legacy),
    ];
    for (const b of [another, otherRepo, legacy]) {
      mock.details[b.id] = b;
      mock.annotations[b.id] = [];
    }

    await installApi(page, mock);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await shot(page, "14-board-tree");
  });

  // 認証を設定した構成の入口。ここを通らないとボードに触れない（ADR 0015）。
  test("ログイン画面を撮る", async ({ page }) => {
    const mock = baseMock();
    mock.session = { status: 200, body: { authRequired: true, authenticated: false } };

    await installApi(page, mock);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await page.getByRole("button", { name: "GitHub でログイン" }).waitFor();
    await shot(page, "09-login");

    // ログイン後はサイドバーに利用者が出る。
    mock.session = { status: 200, body: signedIn() };
    await page.reload();
    await page.getByText("Octo Cat").waitFor();
    await shot(page, "10-signed-in");
  });
});
