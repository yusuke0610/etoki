import { test, type Page } from "@playwright/test";

import { breakAnnotations, breakBoards, installApi, summarize } from "./helpers/api";
import { annotationCard, drawRectangle, openBoard, picker } from "./helpers/board";
import {
  ANNOTATION_IDS,
  BOARD_ID,
  baseMock,
  board,
  interpretation,
  matchedInterpretationMock,
  mixedFramesMock,
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
    // 本文が読めることがこの画面の要点なので、開いた状態で撮る。作成は
    // 取り消せない（ADR 0009）。
    for (const summary of await card.getByText("本文", { exact: true }).all()) {
      await summary.click();
    }
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
    // 結果はパネルの下端に出る。寄せずに撮ると、この画面の要点である作成結果と
    // GitHub へ辿るリンク（ADR 0025）が画面の外に残る。
    await card.locator(".creation-result").scrollIntoViewIfNeeded();
    await shot(page, "04-created");
  });

  // changed の注釈に更新の出口ができた（ADR 0026）。何が書き換わり、何が
  // GitHub 側に取り残されるのかを、押す前に見せている画面を撮る。
  test("更新と取り残しの内訳を撮る", async ({ page }) => {
    await installApi(page, matchedInterpretationMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "セッション管理");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.locator(".left-behind").scrollIntoViewIfNeeded();
    await shot(page, "19-update-and-left-behind");
  });

  // 作る前に選び直せること、選び直した結果がどう見えるかを撮る（ADR 0024）。
  test("作るものを選び直した画面を撮る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).waitFor();

    // 親を外して子だけ戻した状態。構造が変わったことが出ているかを見る。
    await card.getByLabel("e1 を作成する").uncheck();
    await card.getByLabel("i1 を作成する").check();
    await shot(page, "17-interpretation-selected");

    // 1 件も選ばれていないと押せない。理由が読めるかを見る。
    await card.getByLabel("i1 を作成する").uncheck();
    // 理由が出るのを待ってから撮る。待たないと、まだ止まっていない画面が
    // 写る。
    await card.getByText("作るものが 1 件も選ばれていません。").waitFor();
    await shot(page, "18-creation-blocked");
  });

  // 複数フレームのとき、パネルの項目とキャンバスのフレームが対応して見える
  // ことを撮る（ADR 0022）。
  test("カードとフレームの対応を撮る", async ({ page }) => {
    await installApi(page, multiFrameMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await annotationCard(page, "注釈 2").getByRole("button", { name: "注釈 2" }).click();
    // 寄せる動きはアニメーションする。終わる前に撮ると、途中の位置が写る。
    await page.waitForTimeout(1000);
    await shot(page, "16-frame-focused");
  });

  // 注釈にした frame と、ユーザーが自分の用途で使った frame は混在するのが
  // 前提（ルートの CLAUDE.md）。キャンバス上で見分けが付くかを撮る。
  test("注釈にした frame の印を撮る", async ({ page }) => {
    await installApi(page, mixedFramesMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await page.locator(".annotation-overlay-frame").first().waitFor();
    await shot(page, "20-annotation-marks");
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

  // 固定済みでも表示名は取り直せる（ADR 0037）。確定していることと、名前だけは
  // 直せることが同じ場所で読める必要があるので撮る。
  test("固定済みの作成先と、名前の取り直しを撮る", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), targetLocked: true };
    mock.projects["acme/web"] = {
      status: 200,
      body: [
        {
          id: "PVT_1",
          number: 1,
          title: "改名後のロードマップ",
          url: "https://github.com/orgs/acme/projects/1",
        },
      ],
    };

    await installApi(page, mock);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await shot(page, "21-target-locked");

    await page.getByRole("button", { name: "作成先の名前を取り直す" }).click();
    // 木の名前が変わるまで待つ。待たずに撮ると、取り直す前の画面が写る。
    await page
      .locator(".board-list")
      .getByRole("button", { name: "#1 改名後のロードマップ" })
      .waitFor();
    await shot(page, "22-target-display-refreshed");
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

  // 他の人が先に保存した状態（ADR 0020）。上書きしなかったことと、この後どう
  // すればよいかが読める必要がある。
  test("保存が衝突した状態を撮る", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    mock.details[BOARD_ID] = { ...board(), updatedAt: "2026-08-05T09:45:00Z" };
    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();
    await page.getByText("他の人がこのボードを保存しました").waitFor();
    await shot(page, "15-save-conflict");
  });

  // 設定していない機能の見せ方（ADR 0030）。LLM を設定しない構成は README が
  // 想定している使い方なので、その画面が行き止まりに見えないかを画像で見る。
  test("設定していない機能の見せ方を撮る", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: { interpretation: false, creation: false, sharing: false },
    };

    await installApi(page, mock);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    // 理由はパネルに 1 つだけ出る（注釈ごとには並べない）。
    await page.getByText("ETOKI_LLM_API_KEY").waitFor();
    await shot(page, "21-not-configured");
  });

  // 失敗の見せ方（#86）。**打ち手を前に、サーバーの文言は畳んだ側に。**
  // 同じ 409 でもすべきことは違うので、code から引いた文を先に読ませる。
  test("失敗したときの見せ方を撮る", async ({ page }) => {
    const mock = baseMock();
    const target = unselectedBoard();
    mock.boards = [summarize(target)];
    mock.details = { [target.id]: target };
    mock.annotations = { [target.id]: [] };
    // 固定済みのボードに作成先を設定しようとした状態。API を直接叩けば起きる。
    mock.setTargetError = {
      status: 409,
      body: { code: "target_locked", error: "etoki: board target is locked" },
    };

    await installApi(page, mock);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await page.locator(".board-list").getByRole("button", { name: target.name }).click();
    await picker(page)
      .getByRole("button", { name: /acme\/web/ })
      .click();
    await picker(page).getByRole("button", { name: "#1 ロードマップ" }).click();

    const alert = page.getByRole("alert");
    await alert.waitFor();
    // 畳んだ側に何が入っているかも報告に写す。開いた状態で撮る。
    await alert.locator("summary").click();
    await shot(page, "20-error-notice");
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

  // 描画に失敗した状態（ADR 0027）。真っ白にせず、キャンバスを巻き込まずに
  // 出せているかは画像でしか分からない。
  test("落ちたパネルとアプリ全体を撮る", async ({ page }) => {
    await installApi(page, baseMock());
    await breakAnnotations(page);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await page.locator(".board-list").getByRole("button", { name: BOARD_NAME }).click();
    await page.getByRole("alert").waitFor();
    // キャンバスが載ったままであることが要点なので、描画を待ってから撮る。
    await page.locator(".excalidraw canvas").first().waitFor();
    await shot(page, "19-panel-error");
  });

  test("アプリ全体が落ちた画面を撮る", async ({ page }) => {
    await installApi(page, baseMock());
    await breakBoards(page);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await page.getByRole("alert").waitFor();
    await shot(page, "20-app-error");
  });

  // 付箋を 1 枚置いた状態と、保存に送る大きさの表示。**大きさは上限との比を
  // 出さない**（ADR 0018 / 0038）ので、催促に見えていないかを画像で見る。
  test("付箋を置いた状態と大きさの表示を撮る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await page.getByRole("button", { name: "付箋" }).click();
    await page.getByText("未保存", { exact: true }).waitFor();
    await shot(page, "23-sticky-note");
  });

  // 引いた解釈が 2 件並んだ状態。どれを作成に送るのかが読めるかを見る。
  test("解釈の履歴を撮る", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).waitFor();

    mock.interpret = {
      status: 200,
      body: { ...interpretation(), summary: "粒度を変えてもう一度読み解きました。" },
    };
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByLabel("解釈結果").waitFor();
    await shot(page, "24-interpretation-history");
  });

  // 実行の履歴（ADR 0007）。畳み込みの下に並ぶので、同じものを 2 回出して
  // いるように見えていないかを画像で見る。
  test("実行の履歴を撮る", async ({ page }) => {
    const mock = baseMock();
    mock.runs = {
      [ANNOTATION_IDS.created]: {
        status: 200,
        body: [
          {
            id: 2,
            createdAt: "2026-08-04T12:00:00Z",
            items: [
              {
                itemId: "PVTI_issue",
                kind: "issue",
                title: "再設定メールを送る",
                body: "有効期限つきのリンクを送る",
                localId: "i1",
                action: "created",
              },
            ],
          },
          {
            id: 1,
            createdAt: "2026-08-03T12:00:00Z",
            items: [
              {
                itemId: "PVTI_epic",
                kind: "epic",
                title: "パスワード再設定",
                body: "忘れたときの導線をまとめる",
                localId: "e1",
                action: "created",
              },
            ],
          },
        ],
      },
    };
    await installApi(page, mock);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "パスワード再設定");
    await card.getByText("実行の履歴").click();
    await card.getByRole("button", { name: "履歴を読み込む" }).click();
    await card.locator(".run-history").getByText("再設定メールを送る").waitFor();
    await shot(page, "25-run-history");
  });

  // 名前を変えている最中。見出しが入力に変わるので、押し間違いで名前が
  // 変わるように見えていないかを画像で見る。
  test("名前の変更中を撮る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await page.getByRole("button", { name: "名前を変更" }).click();
    await page.getByLabel("ボードの名前").fill("認証の設計会");
    await shot(page, "26-rename");
  });
});
