import { readFile } from "node:fs/promises";

import {
  expect,
  test as base,
  type APIRequestContext,
  type Page,
  type TestInfo,
} from "@playwright/test";

import {
  chooseFromMenu,
  chooseTarget,
  startNewBoard,
  waitForBoard,
} from "../e2e/helpers/board";
import { FAKE_ADDR } from "./addresses";

/**
 * 通しの E2E（ADR 0097）の土台。**`page.route` を使わない。** 相手は
 * `scripts/fake-stack.sh` が立てた実物の etoki と偽の GitHub / LLM で、
 * 画面とサーバーが本物どうしで往復できることだけを見る。
 *
 * 画面の操作は `web/e2e/helpers/board.ts` を使い回す。モックの口
 * （`installApi` / `openBoardWithMock`）を持ち込むと eslint が落とす。
 */

export { expect };

/**
 * spec が使う `test`。**どの spec でも、配信元の外へ出た通信が無いことを見る**
 * （#114）。ビルドした画面を etoki が配る構成でしか、配り漏れたフォントを外の
 * CDN へ取りに行くことは起きない。モックの E2E は Vite の dev サーバーに対して
 * 走るので、そちら（`web/e2e/assets.spec.ts`）では見えない。
 *
 * ページではなくブラウザの文脈で見る。2 つのタブを開く spec のぶんも拾う。
 * **止めずに記録する。** 外へ出られる環境では取れてしまい、画面は普通に描ける。
 */
export const test = base.extend<{ staysInside: void }>({
  staysInside: [
    async ({ context, baseURL }, use) => {
      if (baseURL === undefined) throw new Error("baseURL が設定されていない");
      const own = new URL(baseURL).origin;
      const outside: string[] = [];
      context.on("request", (request) => {
        const url = new URL(request.url());
        if (/^https?:$/.test(url.protocol) && url.origin !== own) {
          outside.push(request.url());
        }
      });
      await use();
      expect(outside, "配信元の外へ出た通信").toEqual([]);
    },
    { auto: true },
  ],
});

/** 偽の GitHub が持つ作成先（`internal/fakeupstream/github.go` の固定値）。 */
const REPOSITORY = "fake-owner/fake-repo";
const PROJECT = "#1 Fake Project";

/**
 * このテストだけが使う語。ボード名と囲みの文字に入れる。
 *
 * **偽の GitHub の記録は 1 回の実行のあいだ全部の spec で共有される**ので、
 * 読むときはこの語で絞る（`fakeItems`）。やり直し（retry）や `--repeat-each` でも
 * 記録は残るので、回ごとに変える。
 */
export function uniqueWord(testInfo: TestInfo): string {
  return `w${testInfo.testId.slice(0, 8)}e${testInfo.repeatEachIndex}r${testInfo.retry}`;
}

/** 一覧から新しいボードを作り、偽の GitHub の Project を作成先に選んで開く。 */
export async function newBoard(page: Page, name: string): Promise<void> {
  await page.goto("/");
  await startNewBoard(page, name);
  await chooseTarget(page, REPOSITORY, PROJECT);
  await waitForBoard(page, name);
}

/** 取り込むシーンの注釈の名前（frame の `name`）。カードと詳細はこの名前で引く。 */
export const ANNOTATION_NAME = "通しの範囲";

/** 1x1 の透明な PNG。往復で同じものが戻ることだけを見る。 */
export const IMAGE_DATA_URL =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

/**
 * 取り込ませる `.excalidraw` の中身。注釈の frame の中に、文字と貼った画像を置く。
 *
 * **画像も入れる。** 画像はシーンと分けて送る（ADR 0074）ので、往復で崩れる
 * としたらまずここ。**文字は frame の中央を横切るように大きく置く。** 書き換える
 * spec は frame の印の中央をダブルクリックして文字を編集に入る。
 * 足りないフィールドはライブラリの復元が埋める。
 */
export function brainstormFile(text: string): string {
  return JSON.stringify({
    type: "excalidraw",
    version: 2,
    source: "e2e-stack",
    elements: [
      {
        id: "frame-stack",
        type: "frame",
        x: 0,
        y: 0,
        width: 480,
        height: 320,
        name: ANNOTATION_NAME,
        customData: { etoki: { granularity: "epic" } },
      },
      {
        id: "text-stack",
        type: "text",
        x: 20,
        y: 130,
        width: 440,
        height: 60,
        text,
        originalText: text,
        fontSize: 40,
        // **書体を名指しする。** 省くと復元は書体を埋めず、キャンバスはブラウザの
        // 代わりの書体で描くので、配信元からフォントを 1 つも取りに行かない
        // （save.spec.ts の「取りに行ったか」が見るもの）。6 は Nunito で、etoki が
        // 置く文字と同じ書体（ADR 0095、`src/excalidraw/drawingStyle.ts`）。
        fontFamily: 6,
        frameId: "frame-stack",
      },
      {
        id: "image-stack",
        type: "image",
        x: 400,
        y: 20,
        width: 60,
        height: 60,
        fileId: "file-stack",
        frameId: "frame-stack",
        status: "saved",
      },
    ],
    appState: { viewBackgroundColor: "#ffffff" },
    files: {
      "file-stack": {
        id: "file-stack",
        mimeType: "image/png",
        dataURL: IMAGE_DATA_URL,
        created: 1700000000000,
        lastRetrieved: 1700000000000,
      },
    },
  });
}

/** 書き出したファイルのうち、spec が見るところ。 */
export type ExportedScene = {
  elements: {
    id: string;
    type: string;
    text?: string;
    fileId?: string;
    frameId?: string | null;
    customData?: { etoki?: { granularity?: string } };
  }[];
  files?: Record<string, { dataURL?: string }>;
};

/**
 * メニューの「書き出し」で、いまキャンバスにあるものをファイルとして受け取る。
 * 書き出しは画像込みで、シーンと画像が両方そろっていないと開けない形になる
 * （ADR 0045）。
 */
export async function exportScene(page: Page): Promise<ExportedScene> {
  const [download] = await Promise.all([
    page.waitForEvent("download"),
    chooseFromMenu(page, "書き出し"),
  ]);
  return JSON.parse(await readFile(await download.path(), "utf-8")) as ExportedScene;
}

/**
 * 偽の GitHub が持つ draft issue 1 件（`internal/fakeupstream/github.go` の `Item`）。
 * 契約ではなく、偽物が中身を見せる口の形。
 */
export type FakeItem = {
  id: string;
  title: string;
  body: string;
  kind: string;
  parent: string;
  updates: number;
};

/**
 * 偽の GitHub に作られた draft issue のうち、タイトルに `word` を含むものを、
 * 作られた順に返す。**ほかの spec が作ったものは混ぜない**（`uniqueWord`）。
 */
export async function fakeItems(
  request: APIRequestContext,
  word: string,
): Promise<FakeItem[]> {
  const response = await request.get(`http://${FAKE_ADDR}/_fake/items`);
  expect(response.status()).toBe(200);
  const items = (await response.json()) as FakeItem[];
  return items.filter((item) => item.title.includes(word));
}
