import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test, type Page, type TestInfo } from "@playwright/test";

import { installApi, type ApiMock } from "./helpers/api";
import { annotationsOfScene, emptyScene, summarize } from "./helpers/boardData";
import { ekidokiButton, openBoard } from "./helpers/board";
import { BOARD_ID, BOARD_NAME, baseMock, board, createdRun } from "./helpers/fixtures";

/**
 * 使い方の動画を録る（ADR 0081）。**テストではなく生成器。** `make
 * tutorial-capture` が `playwright.tutorial.config.ts` で走らせ、
 * `web/src/tutorial/media/<テーマ>/page<n>.mp4` を書き換える。`make test-e2e` には
 * 入らない（ffmpeg が要り、結果は動画のファイルなので）。
 *
 * **ページごとに別のテストで録る。** 1 本を秒数で切り分けると、操作の速さが
 * 変わるたびに区切りがずれる。2・3 ページ目は、1 ページ目が保存したシーンから
 * 始める。
 *
 * **画面を変えたら撮り直す。** 古い動画のままでも、どのテストも落ちない。
 */

const HERE = path.dirname(fileURLToPath(import.meta.url));
const MEDIA = path.join(HERE, "..", "src", "tutorial", "media");
const WORK = path.join(HERE, "..", "e2e-output", "tutorial");

test.describe.configure({ mode: "serial" });

/**
 * ポインタを DOM に重ねる。**ヘッドレスの録画にはポインタが映らない。**
 * 押しているあいだは輪を出し、どこを押したかを見せる。録るときだけ使うもので、
 * 製品のコードには入れない（ADR 0081）。
 */
const CURSOR = `(() => {
  const install = () => {
    if (document.getElementById("tutorial-cursor")) return;
    const cursor = document.createElement("div");
    cursor.id = "tutorial-cursor";
    cursor.innerHTML = '<svg width="22" height="22" viewBox="0 0 24 24"><path d="M4 2 L4 20 L9 15 L12.5 22 L15.5 20.5 L12 13.8 L19 13.8 Z" fill="#111" stroke="#fff" stroke-width="1.5" stroke-linejoin="round"/></svg>';
    Object.assign(cursor.style, { position: "fixed", left: "0", top: "0", zIndex: "2147483647", pointerEvents: "none", transform: "translate(-100px, -100px)" });
    const ring = document.createElement("div");
    Object.assign(ring.style, { position: "absolute", left: "-9px", top: "-9px", width: "18px", height: "18px", borderRadius: "50%", background: "rgba(255, 170, 0, 0.45)", opacity: "0", transition: "opacity 0.15s" });
    cursor.prepend(ring);
    document.documentElement.appendChild(cursor);
    const move = (e) => { cursor.style.transform = "translate(" + e.clientX + "px, " + e.clientY + "px)"; };
    window.addEventListener("pointermove", move, true);
    window.addEventListener("pointerdown", (e) => { move(e); ring.style.opacity = "1"; }, true);
    window.addEventListener("pointerup", () => { ring.style.opacity = "0"; }, true);
  };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", install);
  else install();
})();`;

/** 録った区間。動画の頭からの秒数。 */
type Span = { start: number; end: number };

/**
 * 録画の頭（ボードを開くまで）を測るための時計。**録画はページを作った時点で
 * 始まる**ので、テストの最初に読めば録画の頭とほぼ揃う。
 */
function clock() {
  const t0 = Date.now();
  return () => (Date.now() - t0) / 1000;
}

function themeOf(testInfo: TestInfo): "light" | "dark" {
  return testInfo.project.name === "dark" ? "dark" : "light";
}

/** 1 ページ目が保存したシーン。2・3 ページ目はここから始める。 */
function scenePath(testInfo: TestInfo): string {
  return path.join(WORK, themeOf(testInfo), "scene.json");
}

/** ボタンの真ん中まで、見える速さでポインタを運んでから押す。 */
async function moveAndClick(page: Page, target: ReturnType<Page["locator"]>) {
  const box = await target.boundingBox();
  if (box === null) throw new Error("押す先が見えていない");
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2, { steps: 25 });
  await page.waitForTimeout(250);
  await target.click();
}

/** シーンを持ったボードを開く。注釈の一覧は保存したシーンから組み立てる。 */
async function openWithScene(page: Page, scene: string): Promise<ApiMock> {
  const mock = baseMock();
  const detail = { ...board(), scene };
  mock.details[BOARD_ID] = detail;
  mock.annotations[BOARD_ID] = annotationsOfScene(scene);
  mock.annotationsFollowSave = true;
  mock.boards = [summarize(detail)];
  await page.addInitScript(CURSOR);
  const installed = await installApi(page, mock);
  await page.goto("/");
  await openBoard(page, BOARD_NAME);
  return installed;
}

/**
 * 録画を閉じ、区間を切り出して MP4 にする。**Chromium の録画は WebM** なので
 * ffmpeg で H.264 に変える。音は無い。
 */
async function encode(page: Page, testInfo: TestInfo, n: number, span: Span) {
  const video = page.video();
  if (video === null) throw new Error("録画が有効になっていない");
  await page.close();
  const source = await video.path();
  const out = path.join(MEDIA, themeOf(testInfo), `page${n}.mp4`);
  mkdirSync(path.dirname(out), { recursive: true });
  // 頭は少しだけ余白を残す。押す瞬間から始まると、何を押したのかを追えない。
  const start = Math.max(0, span.start - 0.3);
  execFileSync("ffmpeg", [
    "-hide_banner",
    "-loglevel",
    "error",
    "-y",
    "-ss",
    start.toFixed(2),
    "-i",
    source,
    "-t",
    (span.end + 0.5 - start).toFixed(2),
    "-an",
    "-c:v",
    "libx264",
    "-crf",
    "28",
    "-preset",
    "slow",
    "-pix_fmt",
    "yuv420p",
    "-movflags",
    "+faststart",
    out,
  ]);
}

test("1. 描いて、囲む", async ({ page }, testInfo) => {
  const now = clock();
  const mock = await openWithScene(page, emptyScene());
  const canvas = page.locator(".excalidraw canvas").first();
  const box = await canvas.boundingBox();
  if (box === null) throw new Error("キャンバスが見えていない");
  // 座標はキャンバスの比で取る。右のパネルの下は避ける。
  const at = (fx: number, fy: number) => ({
    x: box.x + box.width * fx,
    y: box.y + box.height * fy,
  });
  const idle = at(0.85, 0.9);
  await page.mouse.click(idle.x, idle.y);
  await page.waitForTimeout(500);

  const start = now();
  const notes: [number, number, string][] = [
    [0.3, 0.2, "ログイン画面"],
    [0.53, 0.2, "メールと\nパスワード"],
    [0.415, 0.53, "失敗回数を\n数える"],
  ];
  for (const [fx, fy, text] of notes) {
    await page.keyboard.press("r");
    const from = at(fx, fy);
    const to = at(fx + 0.16, fy + 0.2);
    await page.mouse.move(from.x, from.y, { steps: 12 });
    await page.mouse.down();
    await page.mouse.move(to.x, to.y, { steps: 18 });
    await page.mouse.up();
    await page.keyboard.press("Enter");
    for (const [i, line] of text.split("\n").entries()) {
      if (i > 0) await page.keyboard.press("Shift+Enter");
      await page.keyboard.type(line, { delay: 70 });
    }
    await page.keyboard.press("Escape");
    // 何も無いところを押して選択を外し、キャンバスに焦点を戻す。
    await page.mouse.click(idle.x, idle.y);
    await page.waitForTimeout(300);
  }

  // フレームツール（F）で囲む。
  await page.keyboard.press("f");
  const from = at(0.26, 0.12);
  const to = at(0.73, 0.8);
  await page.mouse.move(from.x, from.y, { steps: 15 });
  await page.waitForTimeout(250);
  await page.mouse.down();
  await page.mouse.move(to.x, to.y, { steps: 30 });
  await page.mouse.up();
  await page.waitForTimeout(500);

  // 選んだフレームを注釈にする。
  await moveAndClick(page, page.getByRole("button", { name: /を注釈にする/ }));
  await page.waitForTimeout(1200);
  const end = now();

  // 2・3 ページ目のために保存する。録る区間の外。
  await page.keyboard.press("ControlOrMeta+s");
  await expect.poll(() => mock.saveRequests.length).toBe(1);
  // 保存のあとの一覧の読み直しを待つ。待たずに閉じると、読み直しがモックの
  // 外へ漏れて dev サーバーのプロキシに届く。
  await expect(page.getByRole("heading", { name: /^注釈 1 件/ })).toBeVisible();
  mkdirSync(path.dirname(scenePath(testInfo)), { recursive: true });
  writeFileSync(scenePath(testInfo), mock.details[BOARD_ID]?.scene ?? "");

  await encode(page, testInfo, 1, { start, end });
});

test("2. 絵解く", async ({ page }, testInfo) => {
  const now = clock();
  await openWithScene(page, readFileSync(scenePath(testInfo), "utf8"));
  await page.mouse.move(640, 500);
  await page.waitForTimeout(500);

  const start = now();
  await moveAndClick(page, ekidokiButton(page));
  const detail = page.locator("section.annotation-detail:not([hidden])");
  await expect(detail).toBeVisible();
  await page.waitForTimeout(600);
  await moveAndClick(page, detail.getByRole("button", { name: /^絵解(く|き直す)$/ }));
  await expect(detail.getByRole("button", { name: "GitHub に作成する" })).toBeVisible();
  // 読み解きを読ませる。
  await page.mouse.move(420, 260, { steps: 30 });
  await page.waitForTimeout(2500);
  const end = now();

  await encode(page, testInfo, 2, { start, end });
});

test("3. GitHub に作る", async ({ page }, testInfo) => {
  const now = clock();
  const scene = readFileSync(scenePath(testInfo), "utf8");
  const mock = await openWithScene(page, scene);
  // 絵解いたところまでは録る区間の外で進める。2 ページ目で見せた。
  await ekidokiButton(page).click();
  const detail = page.locator("section.annotation-detail:not([hidden])");
  await detail.getByRole("button", { name: /^絵解(く|き直す)$/ }).click();
  const create = detail.getByRole("button", { name: "GitHub に作成する" });
  await expect(create).toBeVisible();
  await page.mouse.move(500, 300);
  await page.waitForTimeout(800);

  const start = now();
  // 作るものの種別（epic / issue）を指でなぞってから作る。
  await page.mouse.move(300, 330, { steps: 20 });
  await page.waitForTimeout(400);
  await page.mouse.move(320, 432, { steps: 15 });
  await page.waitForTimeout(600);
  // 作ったあとの読み直しで「作成済み」を返す。返さないと、作ったのに一覧の
  // 状態が「未作成」のまま録られ、使い方が実物と違う画面を見せる。
  mock.annotations[BOARD_ID] = annotationsOfScene(scene).map((a) => ({
    ...a,
    state: "created",
    lastSyncedAt: createdRun().createdAt,
    items: createdRun().items,
  }));
  await moveAndClick(page, create);
  await expect(detail.getByText("作成した").first()).toBeVisible();
  await expect(page.locator("li.annotation").getByText("作成済み")).toBeVisible();
  await page.waitForTimeout(2500);
  const end = now();

  await encode(page, testInfo, 3, { start, end });
});
