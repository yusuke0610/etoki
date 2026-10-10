import { expect, test, type Page } from "@playwright/test";

import type { ApiMock } from "./helpers/api";
import {
  openBoardWithMock,
  openMermaidPaste,
  openPanelTab,
  saveScene,
} from "./helpers/board";
import { BOARD_ID, baseMock, board } from "./helpers/fixtures";

/**
 * 既存の設計（mermaid）を写しとして貼り、図形にして置く（ADR 0062）。
 *
 * **変換器と mermaid は本物を通す。** 種類ごとに図形になるかはブラウザでしか
 * 分からない（`web/CLAUDE.md`）。LLM もサーバーも通らない機能なので、モックが
 * 返すのはボードまわりだけ。
 */

type SavedElement = {
  id: string;
  type: string;
  x: number;
  width: number;
  text?: string;
  groupIds?: string[];
  containerId?: string | null;
  startArrowhead?: string | null;
  endArrowhead?: string | null;
  customData?: unknown;
  isDeleted?: boolean;
};

/** パネルを開く。 */
async function openPaste(page: Page): Promise<void> {
  await openMermaidPaste(page);
  await expect(page.getByRole("heading", { name: "mermaid を貼る" })).toBeVisible();
}

/** 貼って「キャンバスに置く」を押す。 */
async function paste(page: Page, text: string): Promise<void> {
  await page.getByLabel("貼る mermaid").fill(text);
  await page.getByRole("button", { name: "キャンバスに置く" }).click();
}

/** 保存して、送られたシーンの要素を返す。 */
async function saveAndRead(page: Page, mock: ApiMock): Promise<SavedElement[]> {
  await saveScene(page);
  await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  const scene = JSON.parse(mock.details[BOARD_ID]?.scene ?? "{}") as {
    elements: SavedElement[];
  };
  return scene.elements.filter((el) => !el.isDeleted);
}

/** 失敗したとき、キャンバスに何も足していないことを見る。 */
async function expectCanvasUntouched(page: Page, mock: ApiMock): Promise<void> {
  await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  expect(mock.details[BOARD_ID]?.scene).toBe(board().scene);
}

test.describe("mermaid を貼る", () => {
  // issue #181 の主な用途は ER 図。ほかに構成図（subgraph 付きの flowchart）と
  // シーケンス図。**mermaid の固定（ADR 0061）が外れて ER 図と subgraph が
  // 画像に落ちると、ここが落ちる。**
  //
  // 要素の個数は完全一致で見ない（`diagramChat.spec.ts` と同じ理由）。見るのは、
  // 書いた名前が 1 つも落ちていないことと、直せる図形になっていること。
  for (const { name, mermaid, labels } of [
    {
      name: "erDiagram",
      mermaid:
        "erDiagram\n  CUSTOMER ||--o{ ORDER : places\n  CUSTOMER {\n    int id PK\n    string email\n  }",
      labels: ["CUSTOMER", "ORDER", "places", "id", "email"],
    },
    {
      name: "sequenceDiagram",
      mermaid: "sequenceDiagram\n  画面->>API: 注文する\n  API-->>画面: 受け付けた",
      labels: ["画面", "API", "注文する", "受け付けた"],
    },
    {
      name: "subgraph 付きの flowchart",
      mermaid: "flowchart TD\n  subgraph web\n    a1[画面] --> a2[API]\n  end",
      labels: ["web", "画面", "API"],
    },
  ]) {
    test(`${name} を図形として置き、保存は人に任せる`, async ({ page }) => {
      const mock = await openBoardWithMock(page, baseMock());
      await openPaste(page);

      await paste(page, mermaid);

      // **置いても保存しない**（中核思想 3）。未保存になり、サーバーのシーンは
      // 開いたときのまま。
      await expect(page.getByText("未保存", { exact: true })).toBeVisible();
      expect(mock.details[BOARD_ID]?.scene).toBe(board().scene);
      await expect(page.locator(".mermaid-paste .error")).toHaveCount(0);
      // 置けたら入力は空に戻る。残すと同じ図を 2 度置きやすい。
      await expect(page.getByLabel("貼る mermaid")).toHaveValue("");

      const elements = await saveAndRead(page, mock);
      const types = elements.map((el) => el.type);
      // 画像 1 枚ではなく、手で直せる図形とテキストに分かれている。
      expect(types).not.toContain("image");
      expect(types.filter((t) => t === "arrow").length).toBeGreaterThan(0);
      const texts = elements.flatMap((el) =>
        el.type === "text" && el.text ? [el.text] : [],
      );
      for (const label of labels) expect(texts).toContain(label);

      // **frame を作らない**（ADR 0047）。既存の 3 つのまま。囲むのは人。
      expect(types.filter((t) => t === "frame")).toHaveLength(3);
      // 注釈にも種別にもしない。`erDiagram` を貼っても、種別を選ぶのは人。
      for (const el of elements) {
        if (el.type === "frame") continue;
        expect(el.customData ?? null).toBeNull();
      }
    });
  }

  // 変換器の ER 図の出力を固定する。多重度は線端に、属性は実体と同じ
  // グループに入る。**グループが切れると、実体を動かしても属性が置き去りに
  // なる**（ADR 0062 の論点 B で「受け入れる」とした前提）。
  test("ER 図の多重度は線端になり、属性は実体と同じグループに入る", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());
    await openPaste(page);

    await paste(
      page,
      "erDiagram\n  CUSTOMER ||--o{ ORDER : places\n  CUSTOMER {\n    int id PK\n    string email\n  }",
    );
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    const elements = await saveAndRead(page, mock);
    const heads = elements
      .filter((el) => el.type === "arrow")
      .flatMap((el) => [el.startArrowhead, el.endArrowhead]);
    expect(heads).toContain("cardinality_exactly_one");
    expect(heads).toContain("cardinality_zero_or_many");

    const email = elements.find((el) => el.type === "text" && el.text === "email");
    const group = email?.groupIds?.[0];
    expect(group).toBeDefined();
    // 実体の矩形（名前をラベルに持つもの）が同じグループにいる。
    const title = elements.find((el) => el.type === "text" && el.text === "CUSTOMER");
    const entity = elements.find((el) => el.id === title?.containerId);
    expect(entity?.type).toBe("rectangle");
    expect(entity?.groupIds).toContain(group);
  });

  // 重ねると、写しと手描きの区別が付かず、選び分けて消せない（ADR 0040）。
  test("既存の絵の右外に、重ならないように置く", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());
    const before = (
      JSON.parse(board().scene) as { elements: SavedElement[] }
    ).elements.filter((el) => !el.isDeleted);
    const right = Math.max(...before.map((el) => el.x + el.width));
    await openPaste(page);

    await paste(page, "flowchart TD\n  A[注文] --> B[出荷]");
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    const ids = new Set(before.map((el) => el.id));
    const placed = (await saveAndRead(page, mock)).filter((el) => !ids.has(el.id));
    expect(placed.length).toBeGreaterThan(0);
    for (const el of placed) expect(el.x).toBeGreaterThan(right);
  });

  // GitHub の README や issue から写すと付いてくる。全体を囲む 1 組だけ剥がす。
  test("全体を囲む ```mermaid のフェンスは剥がして置く", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await openPaste(page);

    await paste(page, "```mermaid\nflowchart TD\n  A[注文] --> B[出荷]\n```");

    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
    await expect(page.locator(".mermaid-paste .error")).toHaveCount(0);
  });

  // 貼った本人しか直せず、どこが読めなかったかはパーサの英文にしか無い
  // （ADR 0062）。前に出すのは固定文で、本文は畳んだ側に置く。
  test("構文エラーは固定文で知らせ、パーサのメッセージを畳んで添える", async ({
    page,
  }) => {
    const mock = await openBoardWithMock(page, baseMock());
    await openPaste(page);

    const broken = "flowchart TD\n  A[[[[ -->";
    await paste(page, broken);

    const notice = page.locator(".mermaid-paste .error");
    await expect(notice).toContainText("mermaid として読めませんでした");
    const detail = notice.locator("details");
    await expect(detail.locator("pre")).toBeHidden();
    await detail.locator("summary").click();
    await expect(detail.locator("pre")).toContainText("Parse error on line 2");

    // **直すはずの文字列を消さない。** キャンバスには何も足していない。
    await expect(page.getByLabel("貼る mermaid")).toHaveValue(broken);
    await expectCanvasUntouched(page, mock);
  });

  // 描いただけでタブが固まる種類（gantt など）が 11.13.0 には残る（ADR 0061）。
  // 固まると未保存のブレストが消えるので、mermaid に渡す前に拒む。
  test("受け付けない種類は、置ける種類を名指しして拒む", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());
    await openPaste(page);

    await paste(
      page,
      "gantt\n  title 計画\n  section 設計\n  下書き :a1, 2026-01-01, 3d",
    );

    const notice = page.locator(".mermaid-paste .error");
    await expect(notice).toContainText("置けるのは erDiagram");
    // 種類違いに見せるべき手掛かりは無い。畳んだ本文も出さない。
    await expect(notice.locator("details")).toHaveCount(0);
    await expectCanvasUntouched(page, mock);
  });

  // mermaid は上限を超えると投げずに「Maximum text size in diagram exceeded」の
  // 1 ノードにすり替える。前で拒まないと、それが置けたものとして置かれる。
  test("mermaid の上限を超える長さは、置かずに理由を出す", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());
    await openPaste(page);

    await paste(page, "flowchart TD\n  A --> B\n" + "%".repeat(50_000));

    await expect(page.locator(".mermaid-paste .error")).toContainText(
      "mermaid が変換できる長さ",
    );
    await expectCanvasUntouched(page, mock);
  });

  // **回帰止め。** 固定した mermaid 11.13.0 では、図の中の設定（`%%{init}%%` と
  // frontmatter の `config`）から etoki の画面に CSS を注入できる
  // （GHSA-87f9-hvmw-gh4p）。変換器に渡す設定の `secure` で塞いでいる
  // （`MERMAID_CONFIG`、ADR 0062）。**`secure` から `themeCSS` を外すと、
  // ビーコンへのリクエストが飛んでここが落ちる。**
  for (const { name, header } of [
    {
      name: "init の指示",
      header: (css: string) => `%%{init: {"themeCSS": "${css}"}}%%\n`,
    },
    {
      name: "frontmatter",
      header: (css: string) => `---\nconfig:\n  themeCSS: "${css}"\n---\n`,
    },
  ]) {
    test(`図の中の設定（${name}）から画面に CSS を差し込ませない`, async ({ page }) => {
      const beacons: string[] = [];
      await page.route(
        (url) => url.pathname.startsWith("/__beacon"),
        (route) => {
          beacons.push(route.request().url());
          return route.fulfill({ status: 204 });
        },
      );
      await openBoardWithMock(page, baseMock());
      await openPaste(page);

      const css = ":not(&) body { background-image: url(/__beacon/paste) }";
      await paste(page, `${header(css)}flowchart TD\n  A[注文] --> B[出荷]`);

      // 図そのものは置ける。拒むのは設定の書き換えだけ。
      await expect(page.getByText("未保存", { exact: true })).toBeVisible();
      // 変換のあいだだけ DOM に差し込まれる SVG の <style> が読まれる。
      // 読まれていれば、置き終わるまでに要求が出ている。
      await page.waitForTimeout(500);
      expect(beacons).toEqual([]);
    });
  }

  // **LLM を通さない。** `capabilities.diagramDraft` で止めると、LLM を設定して
  // いない人が使えなくなる。
  test("LLM が未設定でも使える", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: { interpretation: false, diagramDraft: false, creation: true, sharing: true },
    };
    await openBoardWithMock(page, mock);
    await openPaste(page);

    await paste(page, "flowchart TD\n  A[注文] --> B[出荷]");

    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
  });

  // 図のドラフトのタブの中で、作る口と貼る口はどちらか一方だけを出す
  // （`DiagramTab`）。縦に並べると、押した「置く」がどちらのものかが紛れる。
  test("図のドラフトと貼り付けは、どちらか一方だけを開く", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const tab = await openPanelTab(page, "図のドラフト");
    await expect(page.getByRole("heading", { name: "図のドラフト" })).toBeVisible();
    await expect(page.getByRole("heading", { name: "mermaid を貼る" })).toHaveCount(0);

    await openPaste(page);
    await expect(page.getByRole("heading", { name: "図のドラフト" })).toHaveCount(0);

    await tab.getByRole("button", { name: "LLM で作る", exact: true }).click();
    await expect(page.getByRole("heading", { name: "mermaid を貼る" })).toHaveCount(0);
    await expect(page.getByRole("heading", { name: "図のドラフト" })).toBeVisible();
  });

  // 構文エラーを直している途中で図のドラフトを見に行ったり、ほかのタブへ
  // 移ったりしても、入力は残る。
  test("置けなかった入力と理由は、切り替えてもタブを移っても残る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await openPaste(page);

    const broken = "flowchart TD\n  A[[[[ -->";
    await paste(page, broken);
    await expect(page.locator(".mermaid-paste .error")).toBeVisible();

    await page.getByRole("button", { name: "LLM で作る", exact: true }).click();
    await openPaste(page);
    await expect(page.getByLabel("貼る mermaid")).toHaveValue(broken);
    // **置けなかった理由も残る。** 入力は BoardPage が持つので、こちらは
    // 切り替えのたびにパネルを外す作りだと消える（`DiagramTab`）。
    await expect(page.locator(".mermaid-paste .error")).toBeVisible();

    await openPanelTab(page, "注釈");
    await openPaste(page);
    await expect(page.getByLabel("貼る mermaid")).toHaveValue(broken);
  });

  // 描けないのに置けると、置いたものを保存できずに黙って捨てる（ADR 0017）。
  test("viewer には出さない", async ({ page }) => {
    const mock = baseMock();
    const viewer = { ...board(), role: "viewer" as const };
    mock.details[BOARD_ID] = viewer;
    mock.boards = [{ ...(mock.boards[0] ?? {}), ...viewer, role: "viewer" }];
    await openBoardWithMock(page, mock);

    // 貼る口は図のドラフトのタブの中にあり、そのタブが viewer には無い。
    await expect(page.getByRole("tab", { name: "図のドラフト" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "mermaid を貼る" })).toHaveCount(0);
  });
});
