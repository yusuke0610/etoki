import { expect, test } from "@playwright/test";

import { emptyScene, installApi } from "./helpers/api";
import { annotationCard, drawRectangle, openBoard } from "./helpers/board";
import {
  annotatedScene,
  baseMock,
  board,
  BOARD_ID,
  createdRun,
  interpretation,
} from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

test.describe("解釈と作成", () => {
  test("解釈するまで作成のボタンは出ない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // 中核思想 3。開くだけで GitHub に何かが起きる導線があってはならない。
    await expect(page.getByRole("button", { name: "GitHub に作成する" })).toHaveCount(0);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    await expect(card.getByRole("button", { name: "GitHub に作成する" })).toBeVisible();
  });

  // 解釈はテキストを保存済みシーンから、画像を画面から取る。揃っていないと
  // 1 回の解釈の入力が食い違う（ADR 0018）。
  test("未保存の変更があるあいだは解釈できない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    const button = card.getByRole("button", { name: "解釈する" });
    await expect(button).toBeEnabled();

    await drawRectangle(page);

    await expect(button).toBeDisabled();
    // 押せない理由は title に隠さない。disabled なボタンはフォーカスも当たらず、
    // キーボードと読み上げの利用者に理由が届かない。
    await expect(
      card.getByText("保存してから解釈できます", { exact: false }),
    ).toBeVisible();
  });

  test("解釈すると注釈範囲の画像を添えて送る", async ({ page }) => {
    const mock = baseMock();
    // 画像の書き出しには frame の実体が要る。
    mock.details[BOARD_ID] = { ...board(), scene: annotatedScene() };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await expect(
      card.getByText("ログインの入口まわりを 1 つの epic として読みました。"),
    ).toBeVisible();

    // 矢印やグルーピングはテキストに現れない。画像でしか渡せない（中核思想 2）。
    expect(mock.interpretRequests).toHaveLength(1);
    expect(mock.interpretRequests[0]?.image?.mediaType).toBe("image/png");
    expect(mock.interpretRequests[0]?.image?.data ?? "").not.toHaveLength(0);
  });

  // 画像は任意。frame が見つからなくても解釈そのものは止めない（ADR 0018）。
  test("frame が無くてもテキストだけで解釈できる", async ({ page }) => {
    // frame が無いことがこのテストの前提。既定のシーンには注釈の frame が
    // 入っているので、ここでは明示的に空のシーンで開く。
    const empty = baseMock();
    empty.details[BOARD_ID] = { ...board(), scene: emptyScene() };
    const mock = await installApi(page, empty);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    await expect(
      card.getByText("ログインの入口まわりを 1 つの epic として読みました。"),
    ).toBeVisible();
    expect(mock.interpretRequests[0]?.image).toBeUndefined();
  });

  test("解釈すると summary と epic / issue の階層が出る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    // summary は GitHub には作らない。どう読んだかを見せるためだけに出す。
    await expect(
      card.getByText("ログインの入口まわりを 1 つの epic として読みました。"),
    ).toBeVisible();

    // タイトルは手直しできるので入力欄に出る（ADR 0024）。
    await expect(card.getByLabel("e1 の種別")).toHaveValue("epic");
    await expect(card.getByLabel("e1 のタイトル")).toHaveValue("ログイン基盤");
    await expect(card.getByLabel("i1 のタイトル")).toHaveValue(
      "メールとパスワードでログインする",
    );
    await expect(card.getByLabel("i2 のタイトル")).toHaveValue("ログイン失敗を数える");

    // 親子は epic の下に issue が入る形で出る（ADR 0006）。
    const epic = card.locator("li").filter({ has: page.getByLabel("e1 のタイトル") });
    await expect(epic.getByLabel("i1 のタイトル")).toBeVisible();
  });

  // 作成は取り消せない（ADR 0009）。押す前に本文が読めていなければならない。
  test("解釈結果の本文は畳まれていて、開くと読める", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    const epicBody = card.getByLabel("e1 の本文");

    // 既定は畳む。全部開くと一覧が縦に伸びて、作られるものの全体像が追えない。
    await expect(epicBody).toBeHidden();

    const epic = card.locator("li").filter({ has: page.getByLabel("e1 のタイトル") });
    await epic.getByText("本文", { exact: true }).first().click();
    await expect(epicBody).toBeVisible();
    await expect(epicBody).toHaveValue("入口をまとめる");

    // issue の側も同じように開ける。epic の li も i1 を含むので、内側を取る。
    const issue = card
      .locator("li")
      .filter({ has: page.getByLabel("i1 のタイトル") })
      .last();
    await issue.getByText("本文", { exact: true }).click();
    await expect(card.getByLabel("i1 の本文")).toHaveValue("フォームと検証");
  });

  // ここから下は「何を作るか」を開発者に選ばせる約束（ADR 0024）。
  // 見せるだけで LLM の決めたとおりに作らせるのは中核思想 3 に反する。
  test("外した項目は作成リクエストに載らない", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    await card.getByLabel("i2 を作成する").uncheck();
    await card.getByRole("button", { name: "GitHub に作成する" }).click();

    // モックの応答は固定なので、この件数は「作成が終わった」ことの目印。
    // 何を作らせたのかはリクエストボディにしか現れない。
    await expect(card.getByText("3 件を作成しました。")).toBeVisible();
    expect(mock.createRequests).toHaveLength(1);
    expect(mock.createRequests[0]?.items.map((it) => it.localId)).toEqual(["e1", "i1"]);

    // 手直ししてもハッシュの照合は成立する。縛っているのは解釈の入力になった
    // 保存済みシーンであって、解釈結果ではない（ADR 0010）。
    expect(mock.createRequests[0]?.contentHash).toBe(interpretation().contentHash);
    expect(mock.createRequests[0]?.summary).toBe(interpretation().summary);
  });

  // 親だけ消えて子が残ると、選んだつもりのない親なしの issue が GitHub にできる。
  test("epic を外すと配下の issue も外れる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    await card.getByLabel("e1 を作成する").uncheck();

    await expect(card.getByLabel("i1 を作成する")).not.toBeChecked();
    await expect(card.getByLabel("i2 を作成する")).not.toBeChecked();
  });

  // 親を失ったことは黙って起こさない。作られるものが変わっている。
  test("epic を外して戻した issue は、親なしで作ると分かる形で送る", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    await card.getByLabel("e1 を作成する").uncheck();
    await card.getByLabel("i1 を作成する").check();

    await expect(
      card.getByText("epic に属さない issue として作られます。"),
    ).toBeVisible();

    await card.getByRole("button", { name: "GitHub に作成する" }).click();

    // ボディが積まれるのは応答が返ってから。押した直後に読むと、まだ空の
    // createRequests を見て通ることがある。件数は「作成が終わった」ことの
    // 目印で、送った件数ではない（モックの応答は固定）。
    await expect(card.getByText("3 件を作成しました。")).toBeVisible();

    // parentLocalId を残すとサーバーが 400 で弾く。
    expect(mock.createRequests[0]?.items).toHaveLength(1);
    expect(mock.createRequests[0]?.items[0]?.localId).toBe("i1");
    expect(mock.createRequests[0]?.items[0]?.parentLocalId).toBeUndefined();
  });

  test("手直しした title と body が作成リクエストに載る", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    await card.getByLabel("i1 のタイトル").fill("OAuth でログインする");

    const issue = card
      .locator("li")
      .filter({ has: page.getByLabel("i1 のタイトル") })
      .last();
    await issue.getByText("本文", { exact: true }).click();
    await card.getByLabel("i1 の本文").fill("認可コードフローで受ける");

    await card.getByRole("button", { name: "GitHub に作成する" }).click();

    // 押した直後に読むと、まだ積まれていない createRequests を見て通ることが
    // ある。
    await expect(card.getByText("3 件を作成しました。")).toBeVisible();

    const sent = mock.createRequests[0]?.items.find((it) => it.localId === "i1");
    expect(sent?.title).toBe("OAuth でログインする");
    expect(sent?.body).toBe("認可コードフローで受ける");
  });

  test("1 件も選ばれていなければ作成させず、理由を出す", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    // epic を外すと配下も外れるので、これで 3 件とも外れる。
    await card.getByLabel("e1 を作成する").uncheck();

    await expect(card.getByRole("button", { name: "GitHub に作成する" })).toBeDisabled();
    // 押せない理由は title に隠さない。disabled なボタンには理由が届かない。
    await expect(card.getByText("作るものが 1 件も選ばれていません。")).toBeVisible();
    expect(mock.createRequests).toHaveLength(0);
  });

  // 粒度に issue を指定した注釈では epic を 1 件も作れない（サーバーが弾く）。
  // 選ばせておいて必ず断るより、選ばせないほうがよい。
  test("粒度が issue の注釈では種別を変えさせない", async ({ page }) => {
    const mock = baseMock();
    // 粒度 issue の注釈に返るのは issue だけ。epic が混ざった結果は返らない。
    mock.interpret = {
      status: 200,
      body: {
        ...interpretation(),
        items: [
          { localId: "i1", kind: "issue", title: "期限切れを弾く", body: "TTL の確認" },
        ],
      },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "セッション管理");
    await card.getByRole("button", { name: "解釈する" }).click();

    await expect(card.getByLabel("i1 のタイトル")).toHaveValue("期限切れを弾く");
    await expect(card.getByLabel("i1 の種別")).toHaveCount(0);
  });

  // 解釈をやり直したら、前の結果に対する手直しは捨てる。残すと、いま画面に
  // 出ている解釈とは別のものに対する編集が混ざる。
  test("解釈し直すと手直しは捨てられる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    await card.getByLabel("i1 のタイトル").fill("書き換えたタイトル");
    await card.getByLabel("i2 を作成する").uncheck();

    await card.getByRole("button", { name: "解釈する" }).click();

    await expect(card.getByLabel("i1 のタイトル")).toHaveValue(
      "メールとパスワードでログインする",
    );
    await expect(card.getByLabel("i2 を作成する")).toBeChecked();
  });

  test("作成すると件数が出て、状態が作成済みに変わる", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await expect(card.getByText("未作成")).toBeVisible();

    await card.getByRole("button", { name: "解釈する" }).click();

    // 作成後の再取得で返る状態を先に差し替えておく。
    mock.annotations[BOARD_ID] = [
      {
        id: "frame-uncreated",
        name: "ログイン",
        granularity: "",
        state: "created",
        lastSyncedAt: "2026-08-05T10:00:00Z",
        items: createdRun().items,
      },
    ];

    await card.getByRole("button", { name: "GitHub に作成する" }).click();

    await expect(card.getByText("3 件を作成しました。")).toBeVisible();
    await expect(card.getByText("作成済み")).toBeVisible();
  });

  test("途中で失敗した run は、作れた件数と理由を両方出す", async ({ page }) => {
    const mock = baseMock();
    mock.createItems = {
      status: 201,
      body: {
        ...createdRun(),
        items: createdRun().items.slice(0, 1),
        incomplete: true,
        error: "github: rate limited",
      },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).click();

    // 何も作られていないと誤解させると、再実行で draft issue が重複する。
    const result = card.locator(".creation-result");
    await expect(result.getByText("途中で失敗しました（1 件は作成済み）")).toBeVisible();
    // 理由は既定で畳む。捨てはしない（#86）。開けば読める。
    await expect(result.getByText("github: rate limited")).toBeHidden();
    await result.locator(".error-detail > summary").click();
    await expect(result.getByText("github: rate limited")).toBeVisible();
    // 解釈結果にも同じタイトルが並ぶので、作成結果の側だけを見る。
    await expect(result.getByText("ログイン基盤")).toBeVisible();
    await expect(result.getByText("ログイン失敗を数える")).toHaveCount(0);
  });

  test("LLM が未設定なら、その注釈の中だけにエラーを出す", async ({ page }) => {
    const mock = baseMock();
    mock.interpret = {
      status: 503,
      body: {
        code: "llm_not_configured",
        error: "llm is not configured: set ETOKI_LLM_API_KEY or ETOKI_LLM_BASE_URL",
      },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    // 出すのは code から引いた打ち手。サーバーの内部文言は前に出さない（#86）。
    await expect(card.getByText("LLM が未設定です", { exact: false })).toBeVisible();
    await expect(card.getByText("llm is not configured", { exact: false })).toBeHidden();
    // 画面全体のエラー帯に流すと、どの注釈で起きたか分からなくなる。帯は
    // main の直下にしか出ない。
    await expect(page.locator("main > .error")).toHaveCount(0);
  });

  test("解釈時点とシーンが食い違うと、作成は 409 で止まる", async ({ page }) => {
    const mock = baseMock();
    mock.createItems = {
      status: 409,
      body: { code: "content_hash_mismatch", error: "etoki: content hash mismatch" },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).click();

    // 解釈のやり直しは開発者が決める。ここで勝手に解釈し直して作成を続けない。
    // 409 は 6 つの原因が同居するので、打ち手は code から引いて出す（#86）。
    await expect(
      card.getByText("解釈のあとにボードが変わりました", { exact: false }),
    ).toBeVisible();
    await expect(card.getByText("未作成")).toBeVisible();
  });
});
