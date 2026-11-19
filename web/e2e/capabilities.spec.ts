import { expect, test } from "@playwright/test";

import { installApi, type ApiMock } from "./helpers/api";
import { summarize } from "./helpers/boardData";
import {
  annotationCard,
  annotationDetail,
  interpret,
  openAnnotationDetail,
  openBoard,
  openBoardMenu,
  openBoardWithMock,
  openShareDialog,
  openTargetPicker,
} from "./helpers/board";
import { BOARD_ID, baseMock, board, unselectedBoard } from "./helpers/fixtures";

/** GitHub だけが未設定の構成（ADR 0030）。 */
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

  // GitHub が未設定でも、作成先が未選択のボードはキャンバスをそのまま開く
  // （ADR 0090 / 0100）。作成先を選ぶ口は消さず、開いた先で候補を引けない理由を
  // 出す（ADR 0030 は選択の口を capability で止めないと決めている）。
  test("GitHub が未設定なら、作成先を選ぶダイアログに候補を引けない理由を出す", async ({
    page,
  }) => {
    const mock = withoutGitHub(baseMock());
    const legacy = unselectedBoard();
    mock.boards = [summarize(legacy), ...mock.boards];
    mock.details[legacy.id] = legacy;
    mock.annotations[legacy.id] = [];
    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, legacy.name);

    const dialog = await openTargetPicker(page);
    const alert = dialog.getByRole("alert");
    await expect(alert).toContainText("リポジトリを取得できませんでした");
    await expect(alert).toContainText("GitHub が未設定です。");
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
