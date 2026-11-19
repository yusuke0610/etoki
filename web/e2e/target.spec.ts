import { expect, test, type Page } from "@playwright/test";

import type { BoardDetail } from "../src/api/types";
import { holdSetTarget, installApi, type ApiMock } from "./helpers/api";
import { summarize } from "./helpers/boardData";
import {
  annotationCard,
  annotationDetail,
  backToList,
  chooseFromMenu,
  chooseTarget,
  confirmProject,
  createNewBoard,
  drawRectangle,
  interpret,
  openBoard,
  openBoardMenu,
  openBoardWithMock,
  openTargetPicker,
  picker,
  saveScene,
} from "./helpers/board";
import {
  BOARD_ID,
  BOARD_NAME,
  baseMock,
  board,
  repositories,
  unselectedBoard,
} from "./helpers/fixtures";

const UNSELECTED_NAME = "作成先未選択のブレスト";
const UNSELECTED_ID = unselectedBoard().id;
const OTHER_NAME = "決済まわりのブレスト";

/** 作成先が未選択のボードを 1 枚足したモック。 */
function withUnselected() {
  const mock = baseMock();
  const board = unselectedBoard();

  mock.boards = [summarize(board), ...mock.boards];
  mock.details[board.id] = board;
  mock.annotations[board.id] = [];

  return mock;
}

/**
 * いつものボード（注釈「ログイン」を持つ）から作成先を外したモック。描いて
 * 絵解いてから作成先を選ぶ流れ（ADR 0100）を、解釈のモックごと使うため。
 */
function withoutTarget(): ApiMock {
  const mock = baseMock();
  const detail: BoardDetail = {
    ...board(),
    repositoryOwner: "",
    repositoryName: "",
    projectId: "",
    projectNumber: 0,
    projectTitle: "",
    projectUrl: "",
  };
  mock.details[BOARD_ID] = detail;
  mock.boards = mock.boards.map((b) => (b.id === BOARD_ID ? summarize(detail) : b));
  return mock;
}

/** 開いているボードを取り直す要求か。開き直していないことを見るのに使う。 */
function isBoardDetail(r: { method(): string; url(): string }): boolean {
  return r.method() === "GET" && new URL(r.url()).pathname === `/api/boards/${BOARD_ID}`;
}

/** 作成先が未選択のボードを一覧から開き、作成先を選ぶダイアログを開く。 */
async function openUnselectedPicker(page: Page): Promise<void> {
  await openBoard(page, UNSELECTED_NAME);
  await openTargetPicker(page);
}

/**
 * 作成先はボードの上のダイアログで選ぶ（ADR 0100）。**ボードの画面は外さない。**
 * 外すと絵解きの結果と手直しが消え、選んだあとに絵解き直すことになる。
 */
test.describe("作成先の選択", () => {
  // 切れると: 描く前に draft issue の行き先を決めさせる（中核思想 1）。
  test("作成先が未選択でも、キャンバスをそのまま開き、未選択であることを下の帯に出す", async ({
    page,
  }) => {
    await installApi(page, withUnselected());
    await page.goto("/");
    await openBoard(page, UNSELECTED_NAME);

    await expect(picker(page)).toHaveCount(0);
    // 作成先の名前を出している場所に、選んでいないことも状態として出す。
    await expect(page.locator(".badge-target")).toHaveText("作成先: 未選択");
    expect(new URL(page.url()).search).toBe(`?board=${UNSELECTED_ID}`);
    // 未選択のボードの口は「作成先を選ぶ」と名乗る。
    const menu = await openBoardMenu(page);
    await expect(
      menu.getByRole("button", { name: "作成先を選ぶ", exact: true }),
    ).toBeVisible();
    await expect(menu.getByRole("button", { name: "作成先を変更" })).toHaveCount(0);
  });

  test("作成先を選ぶダイアログは、選ばずに閉じられ、ボードはそのまま", async ({
    page,
  }) => {
    const mock = await installApi(page, withUnselected());
    await page.goto("/");
    await openUnselectedPicker(page);
    // 開いていることは URL に載る（読み込み直しで戻せる、ADR 0059）。
    expect(new URL(page.url()).search).toBe(`?board=${UNSELECTED_ID}&picking=1`);
    // 切れると: 焦点が最初の押せるもの（候補が届く前は「やめる」）へ行き、Enter
    // 1 回で閉じる。
    await expect(
      picker(page).getByRole("heading", { name: "作成先を選ぶ", level: 2 }),
    ).toBeFocused();

    await picker(page).getByRole("button", { name: "やめる" }).click();

    await expect(picker(page)).toBeHidden();
    await expect(page.locator(".excalidraw canvas").first()).toBeVisible();
    // 開いた項目はメニューごと消えているので、メニューの口へ戻す。
    await expect(page.locator('[data-testid="main-menu-trigger"]')).toBeFocused();
    // URL を書き換えるのは `close` イベントで、閉じた直後の次のタスクに届く。
    await expect.poll(() => new URL(page.url()).search).toBe(`?board=${UNSELECTED_ID}`);
    // 引き返しただけで作成先は決まっていない。
    expect(mock.details[UNSELECTED_ID]?.projectId).toBe("");

    // Esc でも閉じる（ブラウザの `<dialog>`）。
    await openTargetPicker(page);
    await page.keyboard.press("Escape");
    await expect(picker(page)).toBeHidden();
    await expect.poll(() => new URL(page.url()).search).toBe(`?board=${UNSELECTED_ID}`);
  });

  // 利用者が選ぶのはリポジトリだが、保存するのはそこに紐づく Project。
  // draft issue はリポジトリではなく Project に属するため 2 段になる。
  test("リポジトリを選ぶと、そのリポジトリのプロジェクトが出る", async ({ page }) => {
    await installApi(page, withUnselected());
    await page.goto("/");
    await openUnselectedPicker(page);

    await picker(page)
      .getByRole("button", { name: /acme\/web/ })
      .click();

    await expect(
      picker(page).getByRole("heading", { name: "acme/web のプロジェクト" }),
    ).toBeVisible();
    await expect(
      picker(page).getByRole("button", { name: "#1 ロードマップ" }),
    ).toBeVisible();
    await expect(
      picker(page).getByRole("button", { name: "#4 技術的負債" }),
    ).toBeVisible();
  });

  test("プロジェクトを決めるとダイアログが閉じ、作成先が見えている", async ({ page }) => {
    await installApi(page, withUnselected());
    await page.goto("/");
    await openUnselectedPicker(page);

    await chooseTarget(page, /acme\/web/, "#1 ロードマップ");

    await expect(picker(page)).toBeHidden();
    // どこに作られるのかは常に見えている必要がある。作った draft issue は
    // 取り消せない。
    // **Project まで書く**（#217）。リポジトリ名だけでは作る先が決まらない。
    await expect(page.locator(".badge-target")).toHaveText("acme/web › #1 ロードマップ");
    await expect.poll(() => new URL(page.url()).search).toBe(`?board=${UNSELECTED_ID}`);
  });

  // #450 で守りたいもの。切れると: 選んだあとに絵解き直す（課金される）ことになり、
  // 手直しも消える。
  test("描いて絵解いてから作成先を選んでも、結果と手直しが残り、そのまま作れる", async ({
    page,
  }) => {
    const mock = await openBoardWithMock(page, withoutTarget());
    const detail = annotationDetail(page, "ログイン");
    await interpret(annotationCard(page, "ログイン"));
    await detail.getByLabel("1 件目のタイトル").fill("ログイン基盤（手直し済み）");

    // 作る口の代わりに、選ぶ口を出す。押しても target_not_selected で断られる
    // だけなので、先に言う。
    await expect(detail.getByRole("button", { name: "GitHub に作成する" })).toHaveCount(
      0,
    );
    const choose = detail.getByRole("button", { name: "作成先を選ぶ" });
    await expect(choose).toHaveAccessibleDescription(
      "作成先がまだ決まっていません。選ぶと、この結果のまま GitHub に作れます。",
    );

    // **開き直さない。** 開き直すとキャンバスが作り直され、絵解きの結果が消える。
    const reopened: string[] = [];
    page.on("request", (r) => {
      if (isBoardDetail(r)) reopened.push(r.url());
    });

    await choose.click();
    await chooseTarget(page, /acme\/web/, "#1 ロードマップ");
    await expect(picker(page)).toBeHidden();

    const create = detail.getByRole("button", { name: "GitHub に作成する" });
    await expect(create).toBeEnabled();
    // 開いた元へ焦点が戻る。選ぶ口と作る口は同じボタンなので、次に押すものに載る。
    await expect(create).toBeFocused();
    await expect(detail.getByLabel("1 件目のタイトル")).toHaveValue(
      "ログイン基盤（手直し済み）",
    );
    expect(reopened).toEqual([]);
    expect(mock.interpretRequests).toHaveLength(1);

    await create.click();
    await expect(detail.getByText("3 件を作成しました。")).toBeVisible();
    expect(mock.createRequests).toHaveLength(1);
    expect(mock.createRequests[0]?.items[0]?.title).toBe("ログイン基盤（手直し済み）");
  });

  // 切れると: 未選択のあいだの答え（unknown）が残り、書けない Project に作らせる
  // （逆に、書けるのに止める）。作成先はボードを開いたまま選ぶので、開いたときに
  // 1 度訊くだけでは足りない。
  test("作成先を選ぶと、その Project に書けるかを訊き直す", async ({ page }) => {
    const mock = withoutTarget();
    mock.access = {
      [BOARD_ID]: { status: 200, body: { role: "owner", projectAccess: "denied" } },
    };
    await openBoardWithMock(page, mock);
    const detail = annotationDetail(page, "ログイン");
    await interpret(annotationCard(page, "ログイン"));

    await detail.getByRole("button", { name: "作成先を選ぶ" }).click();
    await chooseTarget(page, /acme\/web/, "#1 ロードマップ");

    await expect(
      detail.getByText("この Project に書き込む権限がありません。", { exact: false }),
    ).toBeVisible();
    await expect(detail.getByRole("button", { name: "GitHub に作成する" })).toHaveCount(
      0,
    );
    await expect(page.locator(".board-context")).toContainText("書けません");
  });

  // 切れると: 設定の途中で閉じられ、設定できなかったときに理由を出す先が無くなる
  // （新しいボードのダイアログと同じ形）。
  test("設定しているあいだは閉じられず、設定中であることを名乗る", async ({ page }) => {
    await installApi(page, withUnselected());
    await page.goto("/");
    await openUnselectedPicker(page);
    let release = (): void => {};
    await holdSetTarget(
      page,
      new Promise<void>((resolve) => {
        release = resolve;
      }),
    );

    await picker(page)
      .getByRole("button", { name: /acme\/web/ })
      .click();
    await picker(page).getByRole("button", { name: "#1 ロードマップ" }).click();
    await confirmProject(page);

    await expect(picker(page).getByRole("button", { name: "設定中…" })).toBeDisabled();
    await expect(picker(page).getByRole("button", { name: "やめる" })).toBeDisabled();
    await page.keyboard.press("Escape");
    await expect(picker(page)).toBeVisible();

    release();
    await expect(picker(page)).toBeHidden();
    await expect(page.locator(".badge-target")).toHaveText("acme/web › #1 ロードマップ");
  });

  // 作成に要るフィールドは、選んだときに 1 度だけ確かめて見せる（ADR 0088）。
  // 切れると: 足りないと分かるのが作成を押したとき（いちばん手間をかけた後）に
  // 戻る（#292）。
  test.describe("選んだ Project のフィールド", () => {
    test("一覧を出しただけでは確かめず、選んだ Project だけを確かめる", async ({
      page,
    }) => {
      const mock = withUnselected();
      await installApi(page, mock);
      await page.goto("/");
      await openUnselectedPicker(page);
      await picker(page)
        .getByRole("button", { name: /acme\/web/ })
        .click();
      await expect(
        picker(page).getByRole("button", { name: "#4 技術的負債" }),
      ).toBeVisible();
      expect(mock.projectFieldsRequests ?? []).toEqual([]);

      const requested = page.waitForRequest(
        (r) =>
          r.method() === "GET" &&
          new URL(r.url()).pathname === "/api/github/projects/PVT_1/fields",
      );
      await picker(page).getByRole("button", { name: "#1 ロードマップ" }).click();
      await requested;

      await expect(
        picker(page).getByText("作成に要るフィールドが揃っています"),
      ).toBeVisible();
      await expect(
        picker(page).getByRole("button", { name: "#1 ロードマップ" }),
      ).toHaveAttribute("aria-pressed", "true");
      // 選んだだけでは確定しない。確定は別のボタン。
      await expect(picker(page)).toBeVisible();
      expect(mock.details[UNSELECTED_ID]?.projectId).toBe("");
      expect(mock.projectFieldsRequests).toEqual(["PVT_1"]);
    });

    test("足りないものだけを並べ、名前は出さず、そのまま確定できる", async ({ page }) => {
      const mock = withUnselected();
      mock.projectFields = {
        PVT_1: {
          status: 200,
          body: {
            projectId: "PVT_1",
            status: "missing",
            missingFields: ["kind_issue", "parent"],
          },
        },
      };
      await installApi(page, mock);
      await page.goto("/");
      await openUnselectedPicker(page);
      await picker(page)
        .getByRole("button", { name: /acme\/web/ })
        .click();
      await picker(page).getByRole("button", { name: "#1 ロードマップ" }).click();

      const notice = picker(page).getByRole("status");
      await expect(notice).toContainText(
        "種別を表す単一選択のフィールド（選択肢 issue）と、親を表すテキストのフィールドがありません",
      );
      // 足りているもの（選択肢 epic）は並べない。
      await expect(notice).not.toContainText("epic");
      // 名前は設定で変わるので写さない（ADR 0083 と同じ理由）。
      await expect(notice).not.toContainText("Kind");
      await expect(notice).not.toContainText("Parent");

      // **結果で確定を止めない**（中核思想 3）。
      const confirm = picker(page).getByRole("button", { name: "この Project にする" });
      await expect(confirm).toBeEnabled();
      await confirm.click();
      await expect(page.locator(".badge-target")).toHaveText(
        "acme/web › #1 ロードマップ",
      );
    });

    test("確かめられなければ、どちらとも言わず、そのまま確定できる", async ({ page }) => {
      const mock = withUnselected();
      mock.projectFields = {
        PVT_1: {
          status: 200,
          body: { projectId: "PVT_1", status: "unknown", missingFields: [] },
        },
      };
      await installApi(page, mock);
      await page.goto("/");
      await openUnselectedPicker(page);
      await picker(page)
        .getByRole("button", { name: /acme\/web/ })
        .click();
      await picker(page).getByRole("button", { name: "#1 ロードマップ" }).click();

      const notice = picker(page).getByRole("status");
      await expect(notice).toContainText("確かめられませんでした");
      await expect(notice).not.toContainText("揃っています");
      await expect(notice).not.toContainText("ありません");

      await confirmProject(page);
      await expect(page.locator(".badge-target")).toHaveText(
        "acme/web › #1 ロードマップ",
      );
    });

    // 切れると: 問い合わせが返らない（通信の失敗）だけで確定できなくなるか、
    // 確かめていないのに揃っている・足りないと出る。
    test("問い合わせ自体が失敗しても、確かめられなかったと出して確定できる", async ({
      page,
    }) => {
      await installApi(page, withUnselected());
      await page.route(
        (url) => url.pathname === "/api/github/projects/PVT_1/fields",
        (route) => route.abort(),
      );
      await page.goto("/");
      await openUnselectedPicker(page);
      await picker(page)
        .getByRole("button", { name: /acme\/web/ })
        .click();
      await picker(page).getByRole("button", { name: "#1 ロードマップ" }).click();

      await expect(picker(page).getByRole("status")).toContainText(
        "確かめられませんでした",
      );
      await confirmProject(page);
      await expect(page.locator(".badge-target")).toHaveText(
        "acme/web › #1 ロードマップ",
      );
    });

    // 切れると: 選び直す前の Project の結果が遅れて着き、いま選んでいる Project に
    // 「足りない」と出る。
    test("選び直したら、前に選んだ Project の結果を出さない", async ({ page }) => {
      const mock = withUnselected();
      mock.projectFields = {
        PVT_1: {
          status: 200,
          body: { projectId: "PVT_1", status: "missing", missingFields: ["parent"] },
        },
      };
      await installApi(page, mock);
      let release = (): void => {};
      const held = new Promise<void>((resolve) => {
        release = resolve;
      });
      await page.route(
        (url) => url.pathname === "/api/github/projects/PVT_1/fields",
        async (route) => {
          await held;
          await route.fallback();
        },
      );
      await page.goto("/");
      await openUnselectedPicker(page);
      await picker(page)
        .getByRole("button", { name: /acme\/web/ })
        .click();

      await picker(page).getByRole("button", { name: "#1 ロードマップ" }).click();
      await picker(page).getByRole("button", { name: "#4 技術的負債" }).click();
      await expect(picker(page).getByRole("status")).toContainText("揃っています");

      const arrived = page.waitForResponse(
        (r) => new URL(r.url()).pathname === "/api/github/projects/PVT_1/fields",
      );
      release();
      await arrived;
      await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 200)));

      await expect(picker(page).getByRole("status")).toContainText("揃っています");
      await expect(picker(page).getByRole("heading", { level: 3 }).last()).toContainText(
        "#4 技術的負債",
      );
    });
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
    await openUnselectedPicker(page);
    await picker(page)
      .getByRole("button", { name: /acme\/web/ })
      .click();

    await expect(
      picker(page).getByRole("button", { name: "#1 唯一のプロジェクト" }),
    ).toHaveAttribute("aria-pressed", "false");
    await expect(
      picker(page).getByRole("button", { name: "この Project にする" }),
    ).toHaveCount(0);
    expect(mock.details[UNSELECTED_ID]?.projectId).toBe("");
  });

  // 権限不足と「本当に 1 つも無い」は API からは区別できない。両方書く。
  test("リポジトリが 0 件なら、権限を確かめるよう案内する", async ({ page }) => {
    const mock = withUnselected();
    mock.repositories = { status: 200, body: { repositories: [], truncated: false } };

    await installApi(page, mock);
    await page.goto("/");
    await openUnselectedPicker(page);

    await expect(
      picker(page).getByText("リポジトリが 1 つも見つかりませんでした"),
    ).toBeVisible();
  });

  // 切れると: 見ていない範囲が残っているのに「権限を確認してください」と案内し、
  // 確認しても何も出てこないところへ送ることになる（ADR 0054）。打ち切りの空と
  // 取り切った空は、打ち手が違う。
  test("打ち切ったうえで 0 件なら、権限ではなく範囲の話にする", async ({ page }) => {
    const mock = withUnselected();
    mock.repositories = { status: 200, body: { repositories: [], truncated: true } };

    await installApi(page, mock);
    await page.goto("/");
    await openUnselectedPicker(page);

    await expect(
      picker(page).getByText("見た範囲にはリポジトリがありませんでした"),
    ).toBeVisible();
    // 権限の案内は出さない。
    await expect(
      picker(page).getByText("リポジトリが 1 つも見つかりませんでした"),
    ).toBeHidden();
  });

  // 候補を取り切っていないことを画面に出す（ADR 0054）。黙って切ると、目当てが
  // 出ない利用者は「権限が無い」「インストールしていない」「見た範囲の外」を
  // 区別できず、見当違いの設定を疑うことになる（中核思想 3）。
  test("一覧を打ち切ったら、その旨を出す", async ({ page }) => {
    const mock = withUnselected();
    mock.repositories = { status: 200, body: { ...repositories(), truncated: true } };

    await installApi(page, mock);
    await page.goto("/");
    await openUnselectedPicker(page);

    await expect(picker(page).getByText("一覧を打ち切っています")).toBeVisible();
  });

  // 打ち切っていないときに出すと、常に出しているのと変わらない。
  test("取り切っていれば、打ち切りの知らせは出さない", async ({ page }) => {
    await installApi(page, withUnselected());
    await page.goto("/");
    await openUnselectedPicker(page);

    await expect(picker(page).getByRole("button", { name: /acme\/web/ })).toBeVisible();
    await expect(picker(page).getByText("一覧を打ち切っています")).toBeHidden();
  });

  // 500 件の平らなリストから目で探すのはつらい（#150）。**絞るのは手元だけ。**
  // 打ち切りの外は問い合わせ直しても出てこない。
  //
  // ダイアログの中だけを見る。ボードの上に開くので、下の帯の作成先のように同じ
  // 語が画面の別の場所にもある。
  test("名前の一部でリポジトリを絞れる", async ({ page }) => {
    await installApi(page, withUnselected());
    await page.goto("/");
    await openUnselectedPicker(page);

    await expect(picker(page).getByRole("button", { name: /acme\/web/ })).toBeVisible();
    await expect(picker(page).getByRole("button", { name: /acme\/api/ })).toBeVisible();

    await picker(page).getByLabel("名前で絞り込む").fill("api");

    await expect(picker(page).getByRole("button", { name: /acme\/api/ })).toBeVisible();
    await expect(picker(page).getByRole("button", { name: /acme\/web/ })).toHaveCount(0);

    // 当てはまるものが無いときは、権限の案内と言い分ける。直す場所が違う。
    await picker(page).getByLabel("名前で絞り込む").fill("存在しない");
    await expect(
      picker(page).getByText("に当てはまるリポジトリはありません"),
    ).toBeVisible();
    await expect(
      picker(page).getByText("リポジトリが 1 つも見つかりませんでした"),
    ).toBeHidden();
  });

  // 切れると: 候補は書ける Project に絞っているのに「無い」とだけ言い、読める
  // だけの Project を持つ人に作る必要の無いものを作りにいかせる（#293）。
  // 無いのか書けないのかは区別できないので、両方を言う（ADR 0017）。
  test("Project が 0 件なら、無いか書く権限が無いかの両方を言う", async ({ page }) => {
    const mock = withUnselected();
    mock.projects["acme/web"] = { status: 200, body: [] };

    await installApi(page, mock);
    await page.goto("/");
    await openUnselectedPicker(page);
    await picker(page)
      .getByRole("button", { name: /acme\/web/ })
      .click();

    await expect(picker(page).getByText("紐づく Project が無いか")).toBeVisible();
    await expect(picker(page).getByText("書く権限がありません")).toBeVisible();
  });

  // 切れると: GitHub App で個人アカウントの Project を辿って拒まれたのに、
  // リポジトリの権限を見直すよう案内する。見直しても直らない（#293）。
  test("Project の取得を拒まれたら、権限と個人アカウントの両方を言う", async ({
    page,
  }) => {
    const mock = withUnselected();
    mock.projects["acme/web"] = {
      status: 403,
      body: {
        code: "forbidden_project",
        error:
          "etoki: forbidden: github graphql: FORBIDDEN: Resource not accessible by integration",
      },
    };

    await installApi(page, mock);
    await page.goto("/");
    await openUnselectedPicker(page);
    await picker(page)
      .getByRole("button", { name: /acme\/web/ })
      .click();

    const alert = picker(page).getByRole("alert");
    await expect(alert).toContainText("プロジェクトを取得できませんでした");
    await expect(alert).toContainText("書く権限が無いか");
    await expect(alert).toContainText("個人アカウントの Project には触れません");
    // 作成で拒まれたときの文（リポジトリの権限だけを言う）は出さない。
    await expect(alert).not.toContainText("リポジトリの権限を確かめてください");
    // 取れなかったのに読み込み中と出し続けない。
    await expect(picker(page).getByText("読み込み中…")).toHaveCount(0);
  });

  test("リポジトリの取得に失敗したら、ダイアログにその旨を出す", async ({ page }) => {
    const mock = withUnselected();
    mock.repositories = {
      status: 502,
      body: { code: "github_unavailable", error: "github api: 401: Bad credentials" },
    };

    await installApi(page, mock);
    await page.goto("/");
    await openUnselectedPicker(page);

    await expect(picker(page).getByRole("alert")).toContainText(
      "リポジトリを取得できませんでした",
    );
  });

  // 切れると: 未保存のあいだ作成先を選べず、選ぶために保存を挟むか、描いたものを
  // 捨てて選ぶことになる。以前は選ぶとキャンバスごと外れたので止めていた。
  test("未保存でも作成先を選べ、描いたものはそのまま保存できる", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());

    await drawRectangle(page);
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    // 止めない（ADR 0100）。押せない理由の文も出さない。
    const menu = await openBoardMenu(page);
    const change = menu.getByRole("button", { name: "作成先を変更" });
    await expect(change).toBeEnabled();
    await expect(menu.getByText("保存してから作成先を変更できます")).toHaveCount(0);
    await change.click();
    await chooseTarget(page, /acme\/web/, "#4 技術的負債");
    await expect(page.locator(".badge-target")).toHaveText("acme/web › #4 技術的負債");

    // 描いたものは未保存のまま残っていて、そのまま保存できる。作成先の設定は
    // 版を進めない（ADR 0092）ので、409 にもならない。
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
    await saveScene(page);
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    await expect(page.getByRole("alert")).toHaveCount(0);
    // **選ぶ前に描いた矩形が、保存したシーンに載っている。** 「保存できた」だけを
    // 見ると、選んだときにキャンバスを作り直して描いたものを捨てる実装でも通る。
    const saved = JSON.parse(mock.details[BOARD_ID]?.scene ?? "{}") as {
      elements: { type: string }[];
    };
    expect(saved.elements.filter((el) => el.type === "rectangle")).toHaveLength(1);
  });

  // 固定済みでも API を直接叩けば 409 が返る。画面はその理由を黙って
  // 飲み込まず、ダイアログに出す。
  test("設定が拒否されたら、ダイアログに理由を出し、選んだことにしない", async ({
    page,
  }) => {
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
    await openUnselectedPicker(page);

    await chooseTarget(page, /acme\/web/, "#1 ロードマップ");

    await expect(picker(page).getByRole("alert")).toContainText(
      "作成先を設定できませんでした",
    );
    await expect(picker(page)).toBeVisible();
    await expect(page.locator(".badge-target")).toHaveText("作成先: 未選択");
  });

  test("固定前は作成先を選び直せ、やめれば前の作成先のまま", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await chooseFromMenu(page, "作成先を変更");
    await expect(picker(page).getByRole("heading", { name: "リポジトリ" })).toBeVisible();

    await picker(page)
      .getByRole("button", { name: /acme\/api/ })
      .click();
    await expect(
      picker(page).getByText("書き込める Projects v2 が紐づいていません"),
    ).toBeVisible();

    await picker(page).getByRole("button", { name: "やめる" }).click();
    await expect(picker(page)).toBeHidden();
    await expect(page.locator(".badge-target")).toHaveText("acme/web › #1 ロードマップ");
  });

  // draft issue を 1 件でも作ると固定される。押せるのに 409 で断るより、
  // 押せないことを見せるほうが状態として正しい。
  test("固定済みなら変更ボタンを出さない", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), targetLocked: true };

    await openBoardWithMock(page, mock);

    // 口はメニューの中にある。**開いてから「無い」を見る。** 閉じたままだと
    // 出していても通る。
    const menu = await openBoardMenu(page);
    await expect(menu.getByRole("button", { name: "作成先を変更" })).toHaveCount(0);
    // 確定していることだけでなく、なぜ確定なのかも本文で読める必要がある。
    await expect(menu.getByText("作成先は確定（draft issue を作成済み）")).toBeVisible();
    await page.keyboard.press("Escape");
    // **確定しているという状態は、メニューを閉じていても読める**（#62）。
    // メニューの文は「作成先を変更」が無い理由で、閉じると消える。
    await expect(page.locator(".board-context")).toContainText("作成先は確定");
  });

  // 固定するのは作成先そのものであって、表示用のスナップショットではない
  // （ADR 0037）。GitHub 側で改名されたら、固定済みでも取り直せる。
  test("固定済みでも作成先の名前を取り直せる", async ({ page }) => {
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

    await openBoardWithMock(page, mock);

    await chooseFromMenu(page, "作成先の名前を取り直す");

    // 作成先そのものは固定されたまま。変更の口は出ない。押すとメニューは
    // 閉じるので、開き直してから見る。
    const menu = await openBoardMenu(page);
    await expect(menu.getByRole("button", { name: "作成先を変更" })).toHaveCount(0);
    await page.keyboard.press("Escape");

    // 一覧は作成先でまとめて見せる（ADR 0019）。取り直した名前が節の見出しに出る。
    // 一覧は別の画面なので戻って見る（ADR 0064）。
    await backToList(page);
    await expect(
      page
        .locator(".board-list")
        .getByRole("heading", { name: "acme/web › #1 改名後のロードマップ", level: 3 }),
    ).toBeVisible();
  });

  // 取り直しは GitHub と etoki の 2 往復ある。そのあいだに一覧へ戻って別の
  // ボードを開けるので、遅れて届いた応答をそのまま入れると、今開いているボードが
  // 外れる。移るときは confirmDiscard を通ってきているのに、その先で確認なしに
  // キャンバスが作り直される形になる。
  test("取り直しの応答が遅れて届いても、切り替えた先のボードを外さない", async ({
    page,
  }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), targetLocked: true };
    // 改名しておく。取り直しが届いたことを、一覧に出る名前で見分けるため。
    // 初めから出ている名前で見ると、一覧を引き直さない実装でも通る。
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

    const other: BoardDetail = { ...board(), id: "board-2", name: OTHER_NAME };
    mock.boards = [...mock.boards, summarize(other)];
    mock.details[other.id] = other;
    mock.annotations[other.id] = [];

    await installApi(page, mock);

    // 取り直しの応答を握っておく。installApi の後に登録したこちらが先に
    // 当たり、fallback で本来の応答に落ちる。
    let release = (): void => {};
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    await page.route(
      (url) => /^\/api\/boards\/[^/]+\/target\/display$/.test(url.pathname),
      async (route) => {
        if (route.request().method() === "PUT") await held;
        await route.fallback();
      },
    );

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await chooseFromMenu(page, "作成先の名前を取り直す");

    // 応答を握ったまま、一覧へ戻って別のボードへ移る。
    await backToList(page);
    await openBoard(page, OTHER_NAME);

    // **応答が反映されたところまで待ってから見る。** 着く前に見ると、反映
    // される前の画面を見て通ってしまう。反映すると一覧を引き直すので、その
    // 取得の応答を合図にする（ボードを開いても一覧は引き直さない）。
    const reloaded = page.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === "/api/boards" && r.request().method() === "GET",
    );
    release();
    await reloaded;

    // 開いているボードのほうは動かない。
    await expect(page.getByRole("heading", { name: OTHER_NAME, level: 1 })).toBeVisible();
    await expect(page.getByRole("heading", { name: BOARD_NAME, level: 1 })).toHaveCount(
      0,
    );

    // 一覧を引き直したことは、取り直した後の名前が節の見出しに出ることで見る
    // （ADR 0019）。
    await backToList(page);
    await expect(
      page
        .locator(".board-list")
        .getByRole("heading", { name: "acme/web › #1 改名後のロードマップ", level: 3 }),
    ).toBeVisible();
  });

  // GitHub 側から消えた（あるいは見えなくなった）。作成先は固定なので選び直しでは
  // 直せない。分かったことをそのまま出す（中核思想 3）。
  test("作成先の Project が見つからなければ、その旨を出す", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), targetLocked: true };
    mock.projects["acme/web"] = { status: 200, body: [] };

    await openBoardWithMock(page, mock);

    await chooseFromMenu(page, "作成先の名前を取り直す");

    await expect(page.getByRole("alert")).toContainText(
      "作成先の Project が GitHub 側で見つかりませんでした",
    );
  });

  // 固定（変えられない）と食い違い（開き直せば解ける）は別の打ち手なので、
  // code で分けてある（ADR 0034 / 0036）。
  test("作成先が食い違ったら、開き直すよう案内する", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), targetLocked: true };
    mock.refreshTargetDisplayError = {
      status: 409,
      body: { code: "target_mismatch", error: "etoki: board target does not match" },
    };

    await openBoardWithMock(page, mock);

    await chooseFromMenu(page, "作成先の名前を取り直す");

    await expect(page.getByRole("alert")).toContainText(
      "作成先が変わっています。ボードを開き直してください。",
    );
  });

  // 切れると: 描く前に draft issue の行き先を決めさせる（中核思想 1）。
  // **送ったリクエストに作成先が無いことまで見る。** 画面が同じでも、選んだ体の
  // 値を送っていれば、選びかけのボードができる。
  test("新しいボードは作成先を選ばずに作り、そのままキャンバスを開く", async ({
    page,
  }) => {
    await installApi(page, baseMock());
    await page.goto("/");

    const created = page.waitForRequest(
      (r) => r.method() === "POST" && new URL(r.url()).pathname === "/api/boards",
    );
    await createNewBoard(page, "決済まわり");
    const body = (await created).postDataJSON() as Record<string, unknown>;

    expect(Object.keys(body)).toEqual(["name"]);
    await expect(picker(page)).toHaveCount(0);
    await expect(page.locator(".badge-target")).toHaveText("作成先: 未選択");
  });
});
