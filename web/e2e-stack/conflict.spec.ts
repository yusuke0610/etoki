import type { BoardWithFiles } from "../src/api/types";

import {
  drawRectangle,
  importBoardFile,
  saveScene,
  waitForBoard,
} from "../e2e/helpers/board";
import { brainstormFile, expect, newBoard, test, uniqueWord } from "./stack";

/**
 * 2 つのタブで同じボードを保存したときの衝突（ADR 0020、ADR 0097）。
 *
 * 版の照合そのものは Go のテスト（実 SQLite）が、帯の見せ方はモックの E2E が
 * 押さえている。ここで見るのは、**ブラウザが持ち回る版の文字列とサーバーが比べる
 * 版が、本物どうしで噛み合っていること。** 形がずれると、衝突を見逃して後勝ちで
 * 上書きするか、衝突していないのに断られる。
 */
test("同じボードを 2 つのタブで開き、後から保存した側には衝突を出して上書きしない", async ({
  page,
  context,
  request,
}, testInfo) => {
  const word = uniqueWord(testInfo);
  const name = `衝突 ${word}`;
  await newBoard(page, name);

  // もう 1 つのタブで同じボードを開く。ボードは URL で開ける（ADR 0059）。
  const other = await context.newPage();
  await other.goto(page.url());
  await waitForBoard(other, name);

  // 先に保存する側。
  await importBoardFile(page, brainstormFile(`${word} 先`));
  await saveScene(page);
  await expect(page.getByText("未保存", { exact: true })).toBeHidden();

  // 後から保存する側は、先の保存より前の版の上に描いている。
  await drawRectangle(other);
  await saveScene(other);
  await expect(other.getByText("他の人がこのボードを保存しました")).toBeVisible();
  // 描いたものは未保存のまま手元に残る。
  await expect(other.getByText("未保存", { exact: true })).toBeVisible();

  // **サーバーに残っているのは先に保存した側。** 画面の帯だけでは、上書きした
  // うえで帯を出す実装でも通る。開く口（`GET /api/boards/{id}`）で直接見る。
  const id = new URL(page.url()).searchParams.get("board");
  expect(id).not.toBeNull();
  const response = await request.get(`/api/boards/${id ?? ""}`);
  expect(response.status()).toBe(200);
  const { scene } = (await response.json()) as BoardWithFiles;
  const elements = (JSON.parse(scene) as { elements: { type: string; text?: string }[] })
    .elements;
  expect(elements.find((element) => element.type === "text")?.text).toBe(`${word} 先`);
  expect(elements.filter((element) => element.type === "rectangle")).toEqual([]);
});
