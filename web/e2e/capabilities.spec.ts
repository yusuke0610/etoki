import { expect, test } from "@playwright/test";

import { holdBoardList, installApi, type ApiMock } from "./helpers/api";
import { summarize } from "./helpers/boardData";
import {
  annotationCard,
  annotationDetail,
  backToList,
  interpret,
  newBoardDialog,
  openAnnotationDetail,
  openBoardMenu,
  openBoardWithMock,
  openShareDialog,
  waitForBoard,
} from "./helpers/board";
import { BOARD_ID, baseMock, board, unselectedBoard } from "./helpers/fixtures";

/** GitHub だけが未設定の構成（ADR 0030 / 0090）。 */
function withoutGitHub(mock: ApiMock): ApiMock {
  mock.capabilities = {
    status: 200,
    body: {
      interpretation: true,
      diagramDraft: true,
      creation: false,
      sharing: true,
      mcpConnections: false,
    },
  };
  return mock;
}

/**
 * 設定していない機能の見せ方（ADR 0030）。
 *
 * LLM や GitHub を設定しなくても etoki は起動する（ADR 0008）。**その構成で
 * 使っている人に、押す前に「いまできないこと」を見せる。** 押してから 503 の
 * 生の文字列が出るのでは、何を設定すればよいか決められない。
 *
 * 押した後の 503 と同じ文言が出ることまで見る。別々に持つと片方だけ古くなる。
 */

test.describe("設定していない機能", () => {
  test("LLM が未設定なら、押す前に解釈できないことと理由を出す", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: {
        interpretation: false,
        diagramDraft: false,
        creation: true,
        sharing: true,
        mcpConnections: false,
      },
    };
    // 画面が案内するだけでなく、叩けば 503 が返る構成そのものを再現する。
    mock.interpret = {
      status: 503,
      body: {
        code: "llm_not_configured",
        error: "llm is not configured: set ETOKI_LLM_API_KEY or ETOKI_LLM_BASE_URL",
      },
    };
    await openBoardWithMock(page, mock);

    // 解釈の口は注釈の詳細の帯にある（#201）。
    const detail = await openAnnotationDetail(page, "ログイン");
    const interpret = detail.getByRole("button", { name: "絵解く" });

    // 黙って消さない。押せないことと、何が足りないかを両方出す。
    await expect(interpret).toBeVisible();
    await expect(interpret).toBeDisabled();

    // 理由は開いた詳細の帯に 1 つ。詳細は 1 つずつしか開かないので、注釈の
    // 数だけ並ばない。
    // **環境変数の名前は出さない**（ADR 0083）。名前を知っているのは起動の
    // 配線だけで、押した後の 503 の本文として「詳細」に出る。
    const reason = page.getByText("LLM が未設定です。");
    await expect(reason).toBeVisible();
    await expect(reason).toHaveCount(1);
    // 押せないボタンはフォーカスも当たらない。読み上げに理由が届くよう、
    // ボタンからこの文を指しておく（既存の「保存してから」と同じ形）。
    await expect(interpret).toHaveAccessibleDescription(/LLM が未設定です。/);
    await expect(page.getByText("ETOKI_", { exact: false })).toHaveCount(0);
  });

  test("GitHub が未設定なら、作成の代わりに理由を出す", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: {
        interpretation: true,
        diagramDraft: true,
        creation: false,
        sharing: true,
        mcpConnections: false,
      },
    };
    await openBoardWithMock(page, mock);

    const card = annotationCard(page, "ログイン");
    const detail = annotationDetail(page, "ログイン");
    await interpret(card);

    // 解釈はできる。**ブレストと解釈まで進めることが読めている**必要がある。
    await expect(
      detail.getByText("ログインの入口まわりを 1 つの epic として読みました。"),
    ).toBeVisible();
    await expect(detail.getByRole("button", { name: "GitHub に作成する" })).toHaveCount(
      0,
    );
    await expect(detail.getByText("GitHub が未設定です。")).toBeVisible();
    await expect(
      detail.getByText("ブレストと絵解きはこのまま続けられます。"),
    ).toBeVisible();
  });

  // GitHub が未設定でもブレストを始められる（ADR 0090、#297）。作成先の候補を
  // 引けないので、選ばずに作り、そのままキャンバスを開く。止めていたころは、
  // LLM を設定しなくてもブレストだけ先にできるという約束（ADR 0008）が、
  // GitHub を設定するまで果たされなかった。
  //
  // **送ったリクエストに作成先が無いことまで見る。** 画面が同じでも、選んだ体の
  // 値を送る実装では未選択のボードにならない。
  test("GitHub が未設定なら、作成先を選ばずにボードを作り、そのまま開く", async ({
    page,
  }) => {
    const mock = withoutGitHub(baseMock());
    await installApi(page, mock);
    await page.goto("/");

    const dialog = await newBoardDialog(page);
    // 作成先の選択を挟まないことを、押す前に言う（中核思想 3）。
    await expect(
      dialog.getByText("GitHub が未設定なので、作成先は選ばずに作ります。", {
        exact: false,
      }),
    ).toBeVisible();
    await expect(
      dialog.getByText("設定したあとで開くと選べます。", { exact: false }),
    ).toBeVisible();
    await dialog.getByLabel("ボード名").fill("ブレスト");
    // 選択画面へ進まず、ここで作るので「作成」と名乗る。
    await expect(dialog.getByRole("button", { name: "次へ" })).toHaveCount(0);
    const created = page.waitForRequest(
      (req) => req.method() === "POST" && new URL(req.url()).pathname === "/api/boards",
    );
    await dialog.getByRole("button", { name: "作成", exact: true }).click();

    const body = (await created).postDataJSON() as Record<string, unknown>;
    expect(body.name).toBe("ブレスト");
    for (const key of ["repositoryOwner", "repositoryName", "projectId"]) {
      expect(body, key).not.toHaveProperty(key);
    }
    await waitForBoard(page, "ブレスト");
    await expect(page.locator(".picker")).toHaveCount(0);
  });

  // 作ったら、一覧の読み直しを待たずに開く。待つと、ダイアログが閉じたあとの
  // 一覧で別のボードを開けてしまい、読み直しのあとで作ったボードが確認なしに
  // 上書きする。**読み直しを止めたまま開くことを見る。**
  test("作ったボードは、一覧の読み直しを待たずに開く", async ({ page }) => {
    const mock = withoutGitHub(baseMock());
    await installApi(page, mock);
    await page.goto("/");
    await expect(
      page.locator(".board-list").getByRole("button", { name: board().name }),
    ).toBeVisible();

    let release: () => void = () => {};
    await holdBoardList(
      page,
      new Promise<void>((resolve) => {
        release = resolve;
      }),
    );
    const dialog = await newBoardDialog(page);
    await dialog.getByLabel("ボード名").fill("ブレスト");
    // 読み直しに行ったことも見る。開いたことだけだと、読み直しを消しても通る。
    const listReload = page.waitForRequest(
      (request) =>
        request.method() === "GET" && new URL(request.url()).pathname === "/api/boards",
    );
    await dialog.getByRole("button", { name: "作成", exact: true }).click();
    await listReload;

    await waitForBoard(page, "ブレスト");
    release();
  });

  // 作成の応答を待つあいだも「戻る」で別のボードを開ける。応答で作ったボードに
  // 切り替えると、開いたボードを確認なしに外し、描き始めていれば未保存の変更
  // ごと捨てる。**送ったあとに別のボードを開いたら、作ったボードは開かない。**
  test("作成を待つあいだに戻って開いたボードを、作成の応答で外さない", async ({
    page,
  }) => {
    const mock = withoutGitHub(baseMock());
    await openBoardWithMock(page, mock);
    await backToList(page);

    let release: () => void = () => {};
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route(
      (url) => url.pathname === "/api/boards",
      async (route) => {
        if (route.request().method() !== "POST") return route.fallback();
        await held;
        await route.fallback();
      },
    );

    const dialog = await newBoardDialog(page);
    await dialog.getByLabel("ボード名").fill("ブレスト");
    await dialog.getByRole("button", { name: "作成", exact: true }).click();
    await page.goBack();
    await waitForBoard(page, board().name);

    // 応答を扱い終えた印として、そのあとの一覧の読み直しを待つ。
    const listReload = page.waitForResponse(
      (r) =>
        r.request().method() === "GET" && new URL(r.url()).pathname === "/api/boards",
    );
    release();
    await listReload;

    await expect(
      page.getByRole("heading", { name: board().name, level: 1 }),
    ).toBeVisible();
    // **完全一致で引く。** 戻って開いたボードの名前も「ブレスト」を含む。
    await expect(
      page.getByRole("heading", { name: "ブレスト", exact: true, level: 1 }),
    ).toHaveCount(0);
  });

  // 作れなかった理由はダイアログの中に出す。モーダルで開いたダイアログは
  // 最上位の層に載るので、外の通知はその下に隠れて読めない。
  test("作れなかったら、ダイアログの中に理由を出して入力を残す", async ({ page }) => {
    const mock = withoutGitHub(baseMock());
    await installApi(page, mock);
    await page.route(
      (url) => url.pathname === "/api/boards",
      async (route) => {
        if (route.request().method() !== "POST") {
          await route.fallback();
          return;
        }
        await route.fulfill({
          status: 500,
          json: { code: "internal", error: "boom" },
        });
      },
    );
    await page.goto("/");

    const dialog = await newBoardDialog(page);
    await dialog.getByLabel("ボード名").fill("ブレスト");
    await dialog.getByRole("button", { name: "作成", exact: true }).click();

    await expect(dialog.getByRole("alert")).toContainText("ボードを作成できませんでした");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByLabel("ボード名")).toHaveValue("ブレスト");
    await expect(dialog.getByRole("button", { name: "作成", exact: true })).toBeEnabled();
  });

  // 作成先が未選択のボードは、開くと作成先の選択から始まる（ADR 0014）。
  // GitHub が未設定なら候補を引けず、選べないまま一覧へ戻るしかない。キャンバスを
  // そのまま開く（ADR 0090）。**一覧のカードも「開くと作成先を選べます」と
  // 言わない。** 言うと、押した先がキャンバスなのに約束と違う。
  test("GitHub が未設定なら、作成先が未選択のボードもそのまま開く", async ({ page }) => {
    const mock = withoutGitHub(baseMock());
    const legacy = unselectedBoard();
    mock.boards = [summarize(legacy), ...mock.boards];
    mock.details[legacy.id] = legacy;
    mock.annotations[legacy.id] = [];
    await installApi(page, mock);
    await page.goto("/");

    const card = page.locator(".board-list").getByRole("button", { name: legacy.name });
    await expect(card).not.toHaveAccessibleDescription(/開くと作成先を選べます/);
    await expect(page.locator(".board-card.unselected")).toHaveCount(0);
    await card.click();

    await waitForBoard(page, legacy.name);
    await expect(page.locator(".picker")).toHaveCount(0);
  });

  // 作成先を省けるのは GitHub が未設定と分かっている構成だけ。使える機能を
  // 引けていないうちは、いつもの流れ（作成先を選ぶ）に倒す。
  test("使える機能を引けないうちは、新しいボードは作成先の選択へ進む", async ({
    page,
  }) => {
    const mock = baseMock();
    mock.capabilities = { status: 500, body: { code: "internal", error: "boom" } };
    await installApi(page, mock);
    await page.goto("/");

    const dialog = await newBoardDialog(page);
    await expect(
      dialog.getByText("作成先は選ばずに作ります", { exact: false }),
    ).toHaveCount(0);
    await dialog.getByLabel("ボード名").fill("ブレスト");
    await dialog.getByRole("button", { name: "次へ" }).click();
    await expect(page.locator(".picker")).toBeVisible();
  });

  // 確かめていないことを「使えない」に倒さない（中核思想 3）。
  test("使える機能を引けなくても、新しいボードは押せる", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = { status: 500, body: { code: "internal", error: "boom" } };
    await installApi(page, mock);
    await page.goto("/");

    await expect(
      page.getByRole("button", { name: "新しいボード", exact: true }),
    ).toBeEnabled();
  });

  // 表示名の取り直しは GitHub の Project 一覧を引く（ADR 0037）。設定して
  // いない構成では押しても引けないので、押させずに理由を出す。
  test("GitHub が未設定なら、作成先の名前も取り直させない", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), targetLocked: true };
    mock.capabilities = {
      status: 200,
      body: {
        interpretation: true,
        diagramDraft: true,
        creation: false,
        sharing: true,
        mcpConnections: false,
      },
    };
    await openBoardWithMock(page, mock);

    // 口はメニューの中にある。**開いてから「無い」を見る。** 閉じたままだと
    // 出していても通る。
    const menu = await openBoardMenu(page);
    await expect(
      menu.getByRole("button", { name: "作成先の名前を取り直す" }),
    ).toHaveCount(0);
    // 確定していることは変わらず読める。理由は押した後に返る 503 と同じ文言。
    await expect(menu.getByText("作成先は確定（draft issue を作成済み）")).toBeVisible();
    await expect(menu.getByText("GitHub が未設定です。").first()).toBeVisible();
  });

  // 口は黙って消さず、開いた先で理由を出す（ADR 0030）。図のドラフトで LLM が
  // 未設定のときと同じ形。
  test("共有が未設定なら、共有のダイアログを開いた先で理由を出す", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: {
        interpretation: true,
        diagramDraft: true,
        creation: true,
        sharing: false,
        mcpConnections: false,
      },
    };
    await openBoardWithMock(page, mock);

    const panel = await openShareDialog(page);
    await expect(panel.getByText("共有には認証の設定が必要です")).toBeVisible();
    // **開いてから「無い」を見る。** 閉じているあいだ中身は DOM に無いので、
    // 開かずに見ると一覧を出していても通る。
    await expect(page.getByRole("region", { name: "メンバー" })).toHaveCount(0);
  });

  // 確かめられなかったことを「使えない」として見せない（中核思想 3）。
  // これまでどおり押せて、押した後に理由が出る。
  test("使える機能を引けなくても、操作は止めない", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = { status: 500, body: { code: "internal", error: "boom" } };
    await openBoardWithMock(page, mock);

    const detail = await openAnnotationDetail(page, "ログイン");
    await expect(detail.getByRole("button", { name: "絵解く" })).toBeEnabled();
    await detail.getByRole("button", { name: "閉じる" }).click();
    await openShareDialog(page);
    await expect(page.getByRole("region", { name: "メンバー" })).toBeVisible();
  });
});
