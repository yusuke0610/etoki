import { expect, test } from "@playwright/test";

import { installApi, listEntry, summarize } from "./helpers/api";
import {
  backToList,
  chooseFromMenu,
  chooseTarget,
  drawRectangle,
  newBoardDialog,
  openBoard,
  openBoardWithMock,
  picker,
  startNewBoard,
} from "./helpers/board";
import {
  BOARD_ID,
  BOARD_NAME,
  annotations,
  baseMock,
  board,
  unselectedBoard,
} from "./helpers/fixtures";

test.describe("ボード", () => {
  test("一覧から選ぶとキャンバスと注釈パネルが開く", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");

    // 最初に出るのは一覧の画面。キャンバスはまだ無い（ADR 0064）。
    await expect(
      page.getByRole("heading", { name: "ボード", exact: true, level: 2 }),
    ).toBeVisible();
    await expect(page.locator(".excalidraw")).toHaveCount(0);

    await openBoard(page, BOARD_NAME);
    await expect(page.getByRole("heading", { name: "選択中のフレーム" })).toBeVisible();
    // 開いたら一覧の画面は外れる。描いているあいだ、他のボードは視界に入れない
    // （中核思想 1）。
    await expect(page.locator(".board-list")).toHaveCount(0);
  });

  // 作成先を選ぶまでボードは作られない。書ける Project を 1 つも持たない人は
  // ここで先に進めず、それが「作成にはリポジトリへのアクセス権が要る」ことの
  // 表れになる（ADR 0017）。
  test("ダイアログで名前を入れて作成先を選ぶと、そのボードが開く", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");

    // 作成のリクエストを控える。選択画面には一覧が無いので、「一覧にまだ
    // 並ばない」では作っていないことを確かめられない（一覧が無ければ常に通る）。
    const created: string[] = [];
    page.on("request", (req) => {
      if (req.method() === "POST" && new URL(req.url()).pathname === "/api/boards") {
        created.push(req.url());
      }
    });

    const dialog = await newBoardDialog(page);
    const name = dialog.getByLabel("ボード名");
    const submit = dialog.getByRole("button", { name: "次へ" });

    // 開いたら名前の欄から始める。
    await expect(name).toBeFocused();
    // 必須であることは支援技術にも伝える。「次へ」が押せないだけでは、読み上げで
    // 欄を移っている人には届かない。
    await expect(name).toHaveJSProperty("required", true);
    // 空白だけでは進ませない。誤って空名のボードが増えるのを防いでいる。
    await expect(submit).toBeDisabled();
    await name.fill("   ");
    await expect(submit).toBeDisabled();

    await name.fill("決済フローのブレスト");
    await expect(submit).toBeEnabled();
    // Enter でも進める。
    await name.press("Enter");

    // まだ作られていない。先に作成先を選ばせる。
    await expect(page.getByRole("heading", { name: "リポジトリ" })).toBeVisible();
    expect(created).toEqual([]);

    await chooseTarget(page, "acme/web", "#1 ロードマップ");

    await expect(
      page.getByRole("heading", { name: "決済フローのブレスト", level: 1 }),
    ).toBeVisible();
    expect(created).toHaveLength(1);

    // 作成したら一覧に並び、ダイアログの入力は空に戻る。
    await backToList(page);
    await expect(
      page.locator(".board-list").getByRole("button", { name: "決済フローのブレスト" }),
    ).toBeVisible();
    await expect((await newBoardDialog(page)).getByLabel("ボード名")).toHaveValue("");
  });

  // ひな形は選ばせるもので、勝手に適用しない（中核思想 3）。既定が空白で
  // あることと、選んだときにその絵で作られることの両方を見る。
  test("ひな形を選ぶと、その絵でボードが作られる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");

    const dialog = await newBoardDialog(page);
    const template = dialog.getByLabel("ひな形");
    // 既定は空白。開いた直後に何かが選ばれていると、選んだ覚えのない絵が出る。
    await expect(template).toHaveValue("");

    await dialog.getByLabel("ボード名").fill("注文フローのブレスト");
    await template.selectOption("sequence");

    // 作成のリクエストを捕まえる。**シーンが載っていることを直接見る。**
    // 画面が開けたことだけでは、空のシーンで作られても緑になる。
    const [request] = await Promise.all([
      page.waitForRequest(
        (req) => req.url().endsWith("/api/boards") && req.method() === "POST",
      ),
      (async () => {
        await dialog.getByRole("button", { name: "次へ" }).click();
        await chooseTarget(page, "acme/web", "#1 ロードマップ");
      })(),
    ]);

    const body = request.postDataJSON() as { scene?: string };
    expect(body.scene, "ひな形のシーンが送られていない").toBeTruthy();
    expect(body.scene).toContain("利用者");
    // **ひな形は絵だけを置く**（ADR 0045）。frame を配ると、注釈にできる枠を
    // etoki が作ったことになり、「frame は人が引く」線が黙って崩れる。
    expect(body.scene).not.toContain('"type":"frame"');
    // 注釈のメタデータも載らない。**種別が載る先は人が引いた frame** で、
    // 選ぶのは注釈パネル（`AnnotationPanel` の「種別」）。
    expect(body.scene).not.toContain('"etoki":');

    await expect(
      page.getByRole("heading", { name: "注文フローのブレスト", level: 1 }),
    ).toBeVisible();
    // 囲むのは人なので、開いた時点では注釈が無い。
    await expect(page.getByText("保存済みの注釈はありません。")).toBeVisible();
    // 作ったあとは空白に戻す。次のボードが前の選択を引き継ぐと、選んだ覚えの
    // ない絵が出る。
    await backToList(page);
    await expect((await newBoardDialog(page)).getByLabel("ひな形")).toHaveValue("");
  });

  // 選び直すために戻った人に、名前を打ち直させない（#200）。
  test("作成先の選択から戻ると、入力を残したままダイアログが開き直す", async ({
    page,
  }) => {
    await installApi(page, baseMock());
    await page.goto("/");

    await startNewBoard(page, "やり直すブレスト", "sequence");
    await picker(page).getByRole("button", { name: "やめる" }).click();

    const dialog = page.getByRole("dialog", { name: "新しいボード" });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByLabel("ボード名")).toHaveValue("やり直すブレスト");
    await expect(dialog.getByLabel("ひな形")).toHaveValue("sequence");
    await expect(dialog.getByLabel("ボード名")).toBeFocused();
  });

  // やめた入力が次に開いたときに残っていると、別のボードのつもりで同じ名前を
  // 作りうる。
  test("キャンセルと Esc は入力を消し、焦点を「新しいボード」へ戻す", async ({
    page,
  }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    const open = page.getByRole("button", { name: "新しいボード", exact: true });

    for (const close of ["キャンセル", "Escape"] as const) {
      const dialog = await newBoardDialog(page);
      await dialog.getByLabel("ボード名").fill("やめるブレスト");
      await dialog.getByLabel("ひな形").selectOption("sequence");

      if (close === "Escape") {
        await page.keyboard.press("Escape");
      } else {
        await dialog.getByRole("button", { name: "キャンセル" }).click();
      }

      await expect(dialog, close).toBeHidden();
      await expect(open, close).toBeFocused();
      const reopened = await newBoardDialog(page);
      await expect(reopened.getByLabel("ボード名"), close).toHaveValue("");
      await expect(reopened.getByLabel("ひな形"), close).toHaveValue("");
      await reopened.getByRole("button", { name: "キャンセル" }).click();
    }
  });

  // 作成先はボードの属性なので、開くまで分からないと取り違えたまま作成に
  // 進める。一覧を作成先ごとの節に分けて見せる（ADR 0019）。
  test("一覧は作成先ごとの節に分かれ、未選択は末尾に出る", async ({ page }) => {
    const mock = baseMock();
    const other = {
      ...board(),
      id: "board-other",
      name: "別プロジェクトのブレスト",
      projectId: "PVT_2",
      projectNumber: 4,
      projectTitle: "技術的負債",
    };
    const legacy = unselectedBoard();

    mock.boards = [summarize(other), ...mock.boards, summarize(legacy)];
    mock.details[other.id] = other;
    mock.details[legacy.id] = legacy;
    mock.annotations[other.id] = [];
    mock.annotations[legacy.id] = [];

    await installApi(page, mock);
    await page.goto("/");

    // 見出しに作成先（リポジトリと Project）を書く。並びは一覧に最初に現れた順で、
    // 作成先が未選択の節は末尾。
    const headings = page.locator(".board-list").getByRole("heading", { level: 3 });
    await expect(headings).toHaveText([
      "acme/web › #4 技術的負債",
      "acme/web › #1 ロードマップ",
      "作成先が未選択",
    ]);
    const unselected = page.getByRole("region", { name: "作成先が未選択" });
    await expect(unselected.getByRole("button", { name: legacy.name })).toBeVisible();
    // 開くと作成先の選択から始まることを、押す前に言う（オーナーなので選べる）。
    await expect(
      unselected.getByRole("button", { name: legacy.name }),
    ).toHaveAccessibleDescription(/開くと作成先を選べます/);
  });

  // 作成先を選べるのはオーナーだけ（ADR 0017）。押した先で選べないのに
  // 「選べます」と書くと、案内が嘘になる。
  test("オーナーでなければ、未選択のカードは作成先をオーナーが選ぶと書く", async ({
    page,
  }) => {
    const mock = baseMock();
    const legacy = { ...unselectedBoard(), role: "editor" as const };
    mock.boards = [...mock.boards, summarize(legacy)];
    mock.details[legacy.id] = legacy;
    mock.annotations[legacy.id] = [];

    await installApi(page, mock);
    await page.goto("/");

    await expect(
      page.locator(".board-list").getByRole("button", { name: legacy.name }),
    ).toHaveAccessibleDescription(/作成先はオーナーが選びます/);
  });

  // 開く前に、どのボードに手を打つものがあるかが分かる（#200、中核思想 3）。
  // **0 件の状態は出さない。** 3 つとも並べると、手を打つものが無いボードも
  // 同じ長さの行で埋まる。
  test("カードには 0 件でない状態の件数だけが出る", async ({ page }) => {
    const mock = baseMock();
    const only = (id: string, name: string) => ({ ...board(), id, name });
    const partial = only("board-partial", "未作成だけのブレスト");
    const empty = only("board-empty", "囲んでいないブレスト");
    const broken = only("board-broken", "読めないブレスト");
    for (const b of [partial, empty, broken]) {
      mock.boards.push(summarize(b));
      mock.details[b.id] = b;
    }
    mock.annotations[partial.id] = annotations().filter((a) => a.state === "uncreated");
    mock.annotations[empty.id] = [];
    mock.annotations[broken.id] = annotations();
    mock.unreadableCounts = [broken.id];

    await installApi(page, mock);
    await page.goto("/");

    const card = (name: string) =>
      page.locator(".board-list").getByRole("button", { name, exact: true });
    // 3 状態とも 1 件ずつあるボード。並びは未作成・作成済み・変更あり。
    await expect(card(BOARD_NAME).locator(".badge:not(.badge-role)")).toHaveText([
      "未作成 1",
      "作成済み 1",
      "変更あり 1",
    ]);
    await expect(card(partial.name).locator(".badge:not(.badge-role)")).toHaveText([
      "未作成 1",
    ]);
    // すべて 0 件なら「注釈なし」。何も出さないと、読めなかったのと区別できない。
    await expect(card(empty.name).locator(".badge:not(.badge-role)")).toHaveText([
      "注釈なし",
    ]);
    // 読めなかったボードは 0 件に化けさせない。一覧のほかのボードは出る（#207）。
    await expect(card(broken.name)).toContainText("注釈の状態を読めません");
    await expect(card(broken.name).locator(".badge:not(.badge-role)")).toHaveCount(0);
  });

  // 自分が何をできるのかは、開く前から見えている（ADR 0017、#200）。
  test("カードには自分のロールと更新時刻が出る", async ({ page }) => {
    const mock = baseMock();
    for (const role of ["editor", "viewer"] as const) {
      const b = { ...board(), id: `board-${role}`, name: `${role} のブレスト`, role };
      mock.boards.push(summarize(b));
      mock.details[b.id] = b;
    }

    await installApi(page, mock);
    await page.goto("/");

    const card = (name: string) =>
      page.locator(".board-list").getByRole("button", { name, exact: true });
    await expect(card(BOARD_NAME).locator(".badge-role")).toHaveText("オーナー");
    await expect(card("editor のブレスト").locator(".badge-role")).toHaveText(
      "編集できる",
    );
    await expect(card("viewer のブレスト").locator(".badge-role")).toHaveText("読むだけ");

    // 更新時刻は経過で書き、機械が読める形も添える。経過の書き方の境目は
    // vitest（`updatedLabel.test.ts`）が見ている。
    const updated = card(BOARD_NAME).locator("time");
    await expect(updated).toHaveAttribute("datetime", board().updatedAt);
    await expect(updated).toHaveText(/^更新: /);
    // 名前はボード名だけ。ロール・更新時刻・状態は説明として結ぶ。
    await expect(card(BOARD_NAME)).toHaveAccessibleName(BOARD_NAME);
    await expect(card(BOARD_NAME)).toHaveAccessibleDescription(
      /オーナー 更新: .* 未作成 1/,
    );
  });

  // ボードで作成してから戻ると、件数が変わっている。読み直さないと「未作成 1」が
  // 残り、作ったのに作っていないように見える（#200）。
  test("ボードから一覧へ戻ると、一覧を読み直す", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());

    // 開いているあいだに状態が変わった（作成した）ことにする。
    mock.annotations[BOARD_ID] = annotations().map((a) => ({
      ...a,
      state: "created" as const,
    }));

    const listed = page.waitForRequest(
      (r) => r.method() === "GET" && new URL(r.url()).pathname === "/api/boards",
    );
    await backToList(page);
    await listed;

    await expect(
      page
        .locator(".board-list")
        .getByRole("button", { name: BOARD_NAME, exact: true })
        .locator(".badge:not(.badge-role)"),
    ).toHaveText(["作成済み 3"]);
  });

  // 一覧の読み込みは、戻るたびと改名・削除の引き直しで並走しうる（#200）。
  // 遅れて届いた古い応答で上書きすると、件数が巻き戻る。
  test("追い越された一覧の応答で、新しい一覧を上書きしない", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());

    // 最初の戻りでは読み込みを止め、止めた時点の（古い）件数を返させる。
    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    let first = true;
    await page.route(
      (url) => url.pathname === "/api/boards",
      async (route) => {
        if (route.request().method() !== "GET" || !first) {
          await route.fallback();
          return;
        }
        first = false;
        const stale = mock.boards.map((b) => listEntry(b, mock.annotations[b.id] ?? []));
        await held;
        await route.fulfill({ json: stale });
      },
    );
    await backToList(page);

    // 開いているあいだに作成したことにして、もう一度開いて戻る。こちらの
    // 読み込みは止めないので、先に届く。
    mock.annotations[BOARD_ID] = annotations().map((a) => ({
      ...a,
      state: "created" as const,
    }));
    await openBoard(page, BOARD_NAME);
    await backToList(page);
    const badges = page
      .locator(".board-list")
      .getByRole("button", { name: BOARD_NAME, exact: true })
      .locator(".badge:not(.badge-role)");
    await expect(badges).toHaveText(["作成済み 3"]);

    // 止めていた古い応答を、ここで届ける。
    const late = page.waitForResponse(
      (r) =>
        r.request().method() === "GET" && new URL(r.url()).pathname === "/api/boards",
    );
    release();
    await late;
    // 届いた応答を画面が受け取るまで 1 回だけ描画を待つ。
    await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 100)));
    await expect(badges).toHaveText(["作成済み 3"]);
  });

  // 打ち間違えたボードがそのまま残らないようにする。**改名でキャンバスは
  // 外れない。** 外すと、名前を直すたびに未保存の確認を通ることになる。
  test("ボードの名前を変えると、見出しと一覧の両方が変わる", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await chooseFromMenu(page, "名前を変更");
    await page.getByLabel("ボードの名前").fill("認証の設計会");
    await page.getByRole("button", { name: "名前を保存" }).click();

    await expect(
      page.getByRole("heading", { name: "認証の設計会", level: 1 }),
    ).toBeVisible();
    // キャンバスは外れない。
    await expect(page.locator(".excalidraw canvas").first()).toBeVisible();

    // 一覧は作成先でまとめて見せる（ADR 0019）。一覧が古い名前のままだと、
    // 開くまでどれがどれか分からない。
    await backToList(page);
    await expect(
      page.locator(".board-list").getByRole("button", { name: "認証の設計会" }),
    ).toBeVisible();
  });

  // **描いている途中に改名しても、描いたものは残り、そのまま保存できる。**
  //
  // 2 つのことを同時に見ている。改名でキャンバスを作り直していないこと（作り
  // 直すと未保存の絵がその場で消える）と、改名が版（updatedAt）を動かして
  // いないこと（動かすと、次の保存が誰もシーンを触っていないのに 409 になる、
  // ADR 0020）。どちらが切れてもここが落ちる。
  test("描いている途中に改名しても、描いたものは残って保存できる", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());

    await drawRectangle(page);
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    await chooseFromMenu(page, "名前を変更");
    await page.getByLabel("ボードの名前").fill("会議中に改名");
    await page.getByRole("button", { name: "名前を保存" }).click();
    await expect(
      page.getByRole("heading", { name: "会議中に改名", level: 1 }),
    ).toBeVisible();

    // 描いたものは未保存のまま残っている。消えていれば「未保存」が下りる。
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "保存", exact: true }).click();

    // 409 なら「他の人がこのボードを保存しました」が出る。出ないことを見る。
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    // 描いた矩形が保存に載っている。「保存できた」だけでは、空のシーンを
    // 送っていても緑になる。
    const saved = JSON.parse(mock.details[BOARD_ID]?.scene ?? "{}") as {
      elements: { type: string }[];
    };
    expect(saved.elements.filter((el) => el.type === "rectangle")).toHaveLength(1);
  });

  test("名前を空にしたままでは保存できない", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await chooseFromMenu(page, "名前を変更");
    await page.getByLabel("ボードの名前").fill("   ");

    await expect(page.getByRole("button", { name: "名前を保存" })).toBeDisabled();
  });

  // **押しただけでは消えない。** 削除は取り消せず、GitHub に作った draft issue も
  // 消せないので、何が失われるのかを見せてから確認させる（ADR 0042、中核思想 3）。
  test("削除は、GitHub 側に残るものを見せてから確認させる", async ({ page }) => {
    const mock = baseMock();
    mock.deletion = { [BOARD_ID]: { status: 200, body: { recordedItemCount: 3 } } };
    await openBoardWithMock(page, mock);

    await chooseFromMenu(page, "ボードを削除");

    // 件数と、GitHub 側が残ることの両方を出す。片方だけだと「消えるのか
    // 残るのか」が読めない。
    const confirm = page.getByRole("alertdialog");
    await expect(confirm).toContainText("draft issue が 3 件記録されています");
    await expect(confirm).toContainText("GitHub 側の draft issue は削除されません");

    // やめれば何も起きない。ボードは開いたまま。
    await confirm.getByRole("button", { name: "やめる" }).click();
    await expect(page.getByRole("alertdialog")).toHaveCount(0);
    await expect(page.getByRole("heading", { name: BOARD_NAME, level: 1 })).toBeVisible();
    expect(mock.details[BOARD_ID]).toBeDefined();
  });

  test("削除すると一覧から消え、キャンバスが閉じる", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());

    await chooseFromMenu(page, "ボードを削除");
    await page.getByRole("alertdialog").getByRole("button", { name: "削除する" }).click();

    // 一覧は作成先でまとめて見せる（ADR 0019）。消したボードが残っていると、
    // 開けない行が並ぶ。
    await expect(
      page.locator(".board-list").getByRole("button", { name: BOARD_NAME }),
    ).toHaveCount(0);
    // 閉じた先は一覧の画面。1 枚しか無かったので空の案内になる。**案内が出る
    // こと自体が、一覧から外れた証拠。** 節は 1 件でもあれば描かれる。
    await expect(
      page.getByText("まだボードがありません。「新しいボード」から作成してください。"),
    ).toBeVisible();
    expect(mock.details[BOARD_ID]).toBeUndefined();
  });

  // 引き直しに任せきりにしない。失敗すると一覧は削除前の値のまま残り、開くと
  // 404 になる行が並ぶ。消えたことは 204 で確かめてあるので、外すのは推測では
  // ない。
  test("引き直しに失敗しても、消したボードは一覧に残らない", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());

    // 削除そのものは通し、そのあとの引き直しだけを落とす。
    mock.boardsError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };

    await chooseFromMenu(page, "ボードを削除");
    await page.getByRole("alertdialog").getByRole("button", { name: "削除する" }).click();

    await expect(
      page.locator(".board-list").getByRole("button", { name: BOARD_NAME }),
    ).toHaveCount(0);
    // 引き直しに失敗したことは黙らない。一覧が古いままかもしれないと伝える。
    await expect(page.getByRole("alert")).toContainText(
      "ボード一覧を取得できませんでした",
    );
  });

  test("一覧の取得に失敗したらエラーを出し、閉じられる", async ({ page }) => {
    const mock = baseMock();
    mock.boardsError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    await installApi(page, mock);

    await page.goto("/");

    const alert = page.getByRole("alert");
    await expect(alert).toContainText("ボード一覧を取得できませんでした");
    await alert.getByRole("button", { name: "閉じる" }).click();
    await expect(alert).toBeHidden();
  });
});
