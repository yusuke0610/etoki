import { expect, test } from "@playwright/test";

import { holdCreate, holdSave, installApi, summarize, type ApiMock } from "./helpers/api";
import {
  annotationCard,
  drawRectangle,
  openBoard,
  openBoardWithMock,
} from "./helpers/board";
import { BOARD_ID, BOARD_NAME, baseMock, board } from "./helpers/fixtures";

const OTHER_NAME = "課金まわりのブレスト";
const OTHER_ID = "board-other";

/** 切り替え先のあるボード一覧。離脱の確認は 2 枚ないと確かめられない。 */
function twoBoards(): ApiMock {
  const mock = baseMock();
  const other = { ...board(), id: OTHER_ID, name: OTHER_NAME };

  mock.boards = [...mock.boards, summarize(other)];
  mock.details[other.id] = other;
  mock.annotations[other.id] = [];

  return mock;
}

test.describe("シーンの保存", () => {
  // Excalidraw はマウント時にも onChange を発火する。それを編集として扱うと、
  // 開いただけで未保存になり、表示が信用できなくなる。
  test("開いた直後は未保存にならない", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    await expect(page.getByText("未保存の変更あり")).toBeHidden();
  });

  // 選択やスクロールでも onChange は発火する。見ただけで未保存になってはならない。
  test("キャンバスを触っただけでは未保存にならない", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const canvas = page.locator(".excalidraw canvas").first();
    const box = await canvas.boundingBox();
    if (!box) throw new Error("キャンバスが表示されていない");

    // 何も無いところをクリックし、選択矩形をドラッグする。要素は増えない。
    await page.mouse.click(box.x + 200, box.y + 200);
    await page.mouse.move(box.x + 150, box.y + 150);
    await page.mouse.down();
    await page.mouse.move(box.x + 400, box.y + 320, { steps: 8 });
    await page.mouse.up();

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  });

  // **背景色は要素に現れない。** `appState` にあり、保存はそれを含めてシーン
  // 全体を書く（ADR 0044）。署名が要素しか見ていないと、色を変えただけの
  // キャンバスが「未保存ではない」と出て、確認も出ないまま離れられる。
  // ここは要素が 1 つも変わらない編集なので、`drawRectangle` では代われない。
  test("キャンバスの背景色を変えると未保存になる", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    // 背景色の見本はライブラリのメニューの中にある。**キャンバスは 830px を
    // 下回るので Excalidraw は携帯向けの並びになり**、見本は左の島ではなく
    // ここに出る。見本のボタンは色の値そのものを `title` に持つ。
    await page.locator('[data-testid="main-menu-trigger"]').click();
    await page.locator(".excalidraw .dropdown-menu button[title='#f5faff']").click();

    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
  });

  test("編集すると未保存が出て、保存すると消える", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    await drawRectangle(page);

    // 3 状態の判定は保存済みシーンが基準。編集中の内容は反映されないので、
    // 反映されていないことが画面から分かる必要がある。
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
    await expect(page.getByText("未保存の変更あり")).toBeVisible();

    await page.getByRole("button", { name: "保存" }).click();

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    await expect(page.getByText("未保存の変更あり")).toBeHidden();
  });

  // ブレストは最初のフェーズなので、ここで失うと後段が全部やり直しになる。
  // 保存が明示操作である以上、押し忘れは構造的に起きる。
  test("未保存のままボードを切り替えようとすると、確認が出て残る", async ({ page }) => {
    await openBoardWithMock(page, twoBoards());
    await drawRectangle(page);

    const messages: string[] = [];
    // 断る側。confirm を出しただけで移動していては止めたことにならない。
    page.on("dialog", (dialog) => {
      messages.push(dialog.message());
      void dialog.dismiss();
    });
    await page.locator(".board-list").getByRole("button", { name: OTHER_NAME }).click();

    // 確認は切り替え先を取ってから出る。押した直後には出ていないので待つ。
    await expect.poll(() => messages.length).toBe(1);
    expect(messages[0]).toContain("未保存の変更があります");

    await expect(page.getByRole("heading", { name: BOARD_NAME, level: 1 })).toBeVisible();
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
  });

  // 押した時点では未保存でなくても、切り替え先を取っているあいだに描き足せる。
  // 確認と外すことのあいだに待ちがあると、そのぶんを確認なしで捨てる。
  test("切り替え先を待っているあいだに描いても、確認なしでは捨てない", async ({
    page,
  }) => {
    await installApi(page, twoBoards());

    // 切り替え先の取得を、こちらが放すまで返さない。待ち時間を作るのが目的。
    let release = (): void => {};
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route(
      (url) => url.pathname === `/api/boards/${OTHER_ID}`,
      async (route) => {
        await held;
        // 応答そのものは installApi のモックに任せる。ここは遅らせるだけ。
        await route.fallback();
      },
    );

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const messages: string[] = [];
    page.on("dialog", (dialog) => {
      messages.push(dialog.message());
      void dialog.dismiss();
    });

    // 押した時点では未保存ではない。取得を待っているあいだに描く。
    await page.locator(".board-list").getByRole("button", { name: OTHER_NAME }).click();
    await drawRectangle(page);
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    release();

    // 描いたぶんは確認を経ずに捨てられない。断ったので元のボードに残る。
    await expect.poll(() => messages.length).toBe(1);
    await expect(page.getByRole("heading", { name: BOARD_NAME, level: 1 })).toBeVisible();
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
  });

  // リロードとタブを閉じる操作はアプリ側で止められない。beforeunload を登録して
  // ブラウザに確認させる。**登録漏れは画面の見た目に出ない**ので、ここで固定する。
  test("未保存のままタブを閉じようとすると、ブラウザの確認が出る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await drawRectangle(page);

    const types: string[] = [];
    page.on("dialog", (dialog) => {
      types.push(dialog.type());
      void dialog.dismiss();
    });

    // runBeforeUnload を付けないとハンドラごと飛ばして閉じる。閉じ終わるのは
    // 待たないので、確認が出たかどうかは後から見る。
    await page.close({ runBeforeUnload: true });
    await expect.poll(() => types).toContain("beforeunload");
  });

  // 解釈は保存を要求するので、作成を押す時点ではふつう保存済み。未保存だけを
  // 見ていると、作成中にタブを閉じても何も訊かれず、作成はそこで止まる
  // （ADR 0051、#140）。
  test("作成中にタブを閉じようとすると、ブラウザの確認が出る", async ({ page }) => {
    await installApi(page, baseMock());
    let release = () => {};
    await holdCreate(
      page,
      new Promise<void>((resolve) => {
        release = resolve;
      }),
    );
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).click();
    await expect(card.getByRole("button", { name: "作成中…" })).toBeVisible();
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    const types: string[] = [];
    page.on("dialog", (dialog) => {
      types.push(dialog.type());
      void dialog.dismiss();
    });

    await page.close({ runBeforeUnload: true });
    await expect.poll(() => types).toContain("beforeunload");
    release();
  });

  // 止めるのは「知らせずに捨てる」ことだけ。捨てると決めたなら通す（中核思想 3）。
  test("確認を承諾すれば、ボードは切り替わる", async ({ page }) => {
    await openBoardWithMock(page, twoBoards());
    await drawRectangle(page);

    page.on("dialog", (dialog) => void dialog.accept());
    await page.locator(".board-list").getByRole("button", { name: OTHER_NAME }).click();

    await expect(page.getByRole("heading", { name: OTHER_NAME, level: 1 })).toBeVisible();
    // 切り替えた先は未保存ではない。持ち越すと、開いただけで止められる。
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  });

  // 保存済みなら黙って切り替わる。毎回確認を出すと、確認そのものが読まれなくなる。
  test("保存してあれば、確認なしで切り替わる", async ({ page }) => {
    await openBoardWithMock(page, twoBoards());
    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    // 出た確認は控えておく。Playwright は既定で dialog を閉じるので、拾わずに
    // おくと「出たが自動で閉じられた」と「出なかった」を取り違える。
    const messages: string[] = [];
    page.on("dialog", (dialog) => {
      messages.push(dialog.message());
      void dialog.dismiss();
    });
    await page.locator(".board-list").getByRole("button", { name: OTHER_NAME }).click();

    await expect(page.getByRole("heading", { name: OTHER_NAME, level: 1 })).toBeVisible();
    expect(messages).toEqual([]);
  });

  // 保存のたびに基準の版を取り直す。取りこぼすと 2 回目が必ず衝突する
  // （ADR 0020）。モックが版を照合しているので、ここが落ちれば基準の更新漏れ。
  test("続けて 2 回保存できる", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    for (let i = 0; i < 2; i++) {
      await drawRectangle(page);
      await expect(page.getByText("未保存", { exact: true })).toBeVisible();
      await page.getByRole("button", { name: "保存" }).click();
      await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    }

    await expect(page.getByText("他の人がこのボードを保存しました")).toBeHidden();
  });

  // 共有ボードでは 2 人が同時に描くのが普通に起きる（ADR 0017）。後勝ちで
  // 上書きすると、消えるのは相手の作業すべてになる（ADR 0020）。
  test("他の人が先に保存していると、上書きせずに知らせる", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());

    // こちらが開いた後に、他の人が保存した状況。版が進むので基準は古くなる。
    mock.details[BOARD_ID] = { ...board(), updatedAt: "2026-08-05T09:45:00Z" };

    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();

    await expect(page.getByText("他の人がこのボードを保存しました")).toBeVisible();
    // 未保存のまま残す。ここが消えると、保存できたと誤解したまま閉じられる。
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
    // 相手のシーンはそのまま。上書きしていないことをモック側でも確かめる。
    expect(mock.details[BOARD_ID]?.scene).toBe(board().scene);
  });

  // 上限を超えたシーンは保存できない（ADR 0038）。**描いたものは失われない**
  // ので、そのことと、何をすれば保存できるのかが画面から分かる必要がある。
  test("大きすぎて保存できないときは、貼った画像を減らすよう案内する", async ({
    page,
  }) => {
    const mock = baseMock();
    mock.saveSceneError = {
      status: 413,
      body: { code: "scene_too_large", error: "etoki: board scene is too large" },
    };
    await openBoardWithMock(page, mock);
    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();

    // 打ち手まで言う。「大きすぎます」だけでは、描いた量を減らせと読める。
    await expect(page.getByText("貼った画像が大きすぎて保存できません")).toBeVisible();
    // 拒まれたのは保存だけ。描いたものはキャンバスに残り、続けて編集できる。
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
  });

  // 上限を導入する前に保存された(と想定する)大きいボードは、開いた時点で
  // 保存できないと分かる（issue #103）。押してから 413 で気づくのでは遅い。
  test("上限を超えたまま保存されているボードは、開いた時点で分かる", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), sceneOverLimit: true };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(
      page.getByText("このボードは保存できる上限を超えています"),
    ).toBeVisible();
  });

  // 保存が成功した = サーバーの上限を満たした、という事実で警告を下ろす。
  // フロントは上限の数値を知らないので、この推論でしか消せない（ADR 0038）。
  test("保存に成功すると、上限超過の警告が消える", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), sceneOverLimit: true };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await expect(
      page.getByText("このボードは保存できる上限を超えています"),
    ).toBeVisible();

    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    await expect(page.getByText("このボードは保存できる上限を超えています")).toBeHidden();

    // 開き直しても出ない。サーバーが返す sceneOverLimit で出し直すので、
    // 画面上で消えただけで開き直すと戻るなら、警告の出どころがずれている。
    await page.reload();
    await openBoard(page, BOARD_NAME);
    await expect(page.getByText("このボードは保存できる上限を超えています")).toBeHidden();
  });

  // 付箋は描いている最中に置くもの。**置くだけで保存はしない**（確定させる
  // のは人間の保存操作だけ）。
  test("付箋を置くと未保存になり、保存するとシーンに載る", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());

    await page.getByRole("button", { name: "付箋" }).click();
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    const saved = JSON.parse(mock.details[BOARD_ID]?.scene ?? "{}") as {
      elements: { type: string }[];
    };
    const kinds = saved.elements.map((el) => el.type);
    if (kinds.filter((t) => t === "rectangle").length !== 1) {
      throw new Error(`付箋が保存に載っていない: ${kinds.join(",")}`);
    }
    // **frame は作らない。** 作ると、境界にまたがる要素の帰属判定を etoki が
    // 抱えることになる（ルートの CLAUDE.md）。元からある 3 つのまま。
    expect(kinds.filter((t) => t === "frame")).toHaveLength(3);
  });

  // 押す前に、いまどれくらいの大きさかが見えている（中核思想 3）。
  // **上限との比は出さない**（ADR 0018 / 0038）ので、ここで見るのは大きさだけ。
  test("保存する前にシーンの大きさが出る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await expect(page.locator(".badge-size")).toHaveText(/^[\d.]+ (B|KiB|MiB)$/);
  });

  test("未保存のまま解釈しようとすると、保存を促す", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await drawRectangle(page);

    await expect(
      page.getByText("保存してから解釈できます", { exact: false }).first(),
    ).toBeVisible();
  });

  // 保存は明示操作だけ（ADR 0021）なので、押すまでの手数の少なさがそのまま
  // 値打ちになる。**誰も拾わないと既定の動作（ブラウザの「ページを保存」）に
  // 落ちる。** Excalidraw 側の Ctrl+S は `UIOptions` から外してある（ADR 0045）
  // ので、押しても何も起きず preventDefault もされない（issue #145）。
  test("Ctrl / Cmd + S で保存できる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await drawRectangle(page);
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    const saved = page.waitForRequest(
      (req) => req.method() === "PUT" && new URL(req.url()).pathname.endsWith("/scene"),
    );
    await page.locator(".excalidraw canvas").first().press("ControlOrMeta+s");
    await saved;

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  });

  // 切れると: 非ラテン配列（ロシア語など）では物理の S を押しても `e.key` が
  // "s" にならないので誰も拾わず、**ブラウザの「ページを保存」が開く。**
  // Ctrl+S の習慣はキーの位置で覚えているので、`e.code` も見る。
  //
  // Playwright の `press` は配列を差し替えられないので、`key` と `code` が
  // 食い違うイベントを直に投げる。**ハンドラは window で聴いている**ので届く。
  test("非ラテン配列でも Ctrl / Cmd + S で保存できる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await drawRectangle(page);
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    const saved = page.waitForRequest(
      (req) => req.method() === "PUT" && new URL(req.url()).pathname.endsWith("/scene"),
    );
    await page.evaluate(() => {
      window.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "\u044B", // ロシア語配列で物理の S が返す文字
          code: "KeyS",
          ctrlKey: true,
          bubbles: true,
          cancelable: true,
        }),
      );
    });
    await saved;

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  });

  // **押せないときも既定の動作は止める。** 「保存できなかった」の代わりに
  // ブラウザの保存ダイアログが出るのは、押せない理由を見せるどころではない。
  // viewer には保存そのものが無い（ADR 0017）ので、ここがいちばん外しやすい。
  test("viewer が押しても保存せず、ブラウザの既定にも落とさない", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), role: "viewer" };
    mock.boards = mock.boards.map((b) => ({ ...b, role: "viewer" }));

    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await expect(page.getByRole("button", { name: "保存" })).toHaveCount(0);

    const requests: string[] = [];
    page.on("request", (req) => {
      if (req.method() === "PUT") requests.push(req.url());
    });

    // 既定の動作が止まったかは、押した側で見るしかない。ブラウザの保存
    // ダイアログは Playwright からは観測できない。
    const prevented = await page.evaluate(async () => {
      let seen = false;
      const watch = (e: KeyboardEvent) => {
        if (e.key === "s") seen = e.defaultPrevented;
      };
      // **`keydown` の最後に見る。** 先に登録すると、保存側が
      // preventDefault する前に読むことになる。
      window.addEventListener("keydown", watch);
      document.querySelector("canvas")?.dispatchEvent(
        new KeyboardEvent("keydown", {
          key: "s",
          ctrlKey: true,
          bubbles: true,
          cancelable: true,
        }),
      );
      window.removeEventListener("keydown", watch);
      return seen;
    });

    expect(prevented).toBe(true);
    expect(requests).toEqual([]);
  });

  // 保存中にもう一度押しても、2 本目を投げない（issue #145）。
  //
  // **同じ tick に 2 回押す。** `saving` は state なので、次の描画までは 2 回目が
  // まだ false を読む。ボタンだけなら押し間違いの二度押しで済んだが、
  // Ctrl / Cmd + S には**キーの自動リピート**があり、押しっぱなしで keydown が
  // 連続する。押した時点で弾く ref を外すと、ここが 2 本目を数える。
  //
  // 押す間隔を空けると、そのあいだに再描画が入って state だけでも通ってしまう。
  // dispatchEvent で 2 つ続けて投げているのはそのため。
  test("保存中に押しても、保存を重ねない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);

    let release = (): void => {};
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    await holdSave(page, held);

    const saves: string[] = [];
    page.on("request", (req) => {
      if (req.method() === "PUT" && new URL(req.url()).pathname.endsWith("/scene")) {
        saves.push(req.url());
      }
    });

    await page.evaluate(() => {
      for (let i = 0; i < 2; i++) {
        window.dispatchEvent(
          new KeyboardEvent("keydown", {
            key: "s",
            ctrlKey: true,
            bubbles: true,
            cancelable: true,
          }),
        );
      }
    });

    await expect(page.getByRole("button", { name: "保存中…" })).toBeVisible();

    // 再描画を挟んだあとの 1 回も通さない。こちらはボタンの disabled と同じ式が
    // 効いていることを見る。
    await page.locator(".excalidraw canvas").first().press("ControlOrMeta+s");
    await expect(page.getByRole("button", { name: "保存中…" })).toBeVisible();

    release();
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    expect(saves).toHaveLength(1);
  });

  test("保存すると、それまでの解釈結果は捨てられる", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const card = page.locator("li.annotation").filter({ hasText: "ログイン" });
    await card.getByRole("button", { name: "解釈する" }).click();
    await expect(card.getByRole("button", { name: "GitHub に作成する" })).toBeVisible();

    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();

    // 解釈は保存済みシーンに対する結果。保存したら対象が変わっているので、
    // 古い結果のまま作成させてはならない。
    await expect(card.getByRole("button", { name: "GitHub に作成する" })).toHaveCount(0);
  });
});
