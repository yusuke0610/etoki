import type { Page } from "@playwright/test";

import {
  annotationCard,
  annotationDetail,
  importBoardFile,
  interpret,
  saveScene,
} from "../e2e/helpers/board";
import {
  ANNOTATION_NAME,
  brainstormFile,
  expect,
  fakeItems,
  newBoard,
  test,
  uniqueWord,
  type FakeItem,
} from "./stack";

/**
 * 作成 → 変更あり → 更新（ADR 0097、ADR 0026）。
 *
 * **モックでは状態が動かない流れ。** モックの E2E は作成後に注釈の状態を書き換え
 * ないので、作ったあとに「作成済み」になり、直して保存すると「変更あり」になり、
 * 絵解き直すと前に作ったものの書き換えになる、という並びを画面で通したことが
 * 無かった（#139）。ここではサーバーが記録した run とハッシュから状態が決まり、
 * 偽の GitHub に実際に書かれたもので結果を見る。
 */
test("作って、文字を直して保存すると変更ありになり、絵解き直すと同じ draft issue を書き換える", async ({
  page,
  request,
}, testInfo) => {
  const word = uniqueWord(testInfo);
  await newBoard(page, `作成と更新 ${word}`);

  await importBoardFile(page, brainstormFile(`${word} 前`));
  await saveScene(page);
  await expect(page.getByText("未保存", { exact: true })).toBeHidden();

  const card = annotationCard(page, ANNOTATION_NAME);
  const detail = annotationDetail(page, ANNOTATION_NAME);
  await expect(card).toContainText("未作成");

  // 1 回目: 絵解いて作る。
  await interpret(card);
  await detail.getByRole("button", { name: "GitHub に作成する" }).click();
  await expect(detail.locator(".creation-result")).toContainText("3 件を作成しました");
  await expect(card).toContainText("作成済み");

  const created = await fakeItems(request, word);
  // 偽の LLM は epic 1 件とその子の issue 2 件を返す（`internal/fakeupstream/llm.go`）。
  expect(created.map((item) => item.kind)).toEqual(["epic", "issue", "issue"]);
  const epic = created[0] as FakeItem;
  expect(epic.title).toContain(`${word} 前`);
  expect(epic.parent).toBe("");
  // 親は epic のタイトルで指す（ADR 0006）。
  expect(created.slice(1).map((item) => item.parent)).toEqual([epic.title, epic.title]);
  expect(created.map((item) => item.updates)).toEqual([0, 0, 0]);

  // 詳細はキャンバスを覆うので、閉じてから文字を直す。
  await detail.getByRole("button", { name: "閉じる" }).click();
  await editAnnotationText(page, `${word} 後`);
  await saveScene(page);
  await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  await expect(card).toContainText("変更あり");

  // 2 回目: 絵解き直すと、前に作ったものが更新の候補として出る（ADR 0026）。
  await interpret(card);
  await expect(detail.getByText("更新", { exact: true })).toHaveCount(3);
  await detail.getByRole("button", { name: "GitHub に作成する" }).click();
  await expect(detail.locator(".creation-result")).toContainText("3 件を更新しました");
  await expect(card).toContainText("作成済み");

  // **増えずに、同じ 3 件が書き換わっている。** 新しく作ると、消せない draft issue
  // が重複する（ADR 0009）。
  const updated = await fakeItems(request, word);
  expect(updated.map((item) => item.id)).toEqual(created.map((item) => item.id));
  expect(updated.map((item) => item.updates)).toEqual([1, 1, 1]);
  const newEpic = updated[0] as FakeItem;
  expect(newEpic.title).toContain(`${word} 後`);
  expect(newEpic.title).not.toContain(`${word} 前`);
  // 子の Parent も新しい epic のタイトルへ張り直される。
  expect(updated.slice(1).map((item) => item.parent)).toEqual([
    newEpic.title,
    newEpic.title,
  ]);
});

/**
 * 注釈の frame の中の文字を書き換える。
 *
 * 取り込んだ文字は frame の中央を横切っている（`brainstormFile`）。印
 * （`.annotation-overlay-frame`）は frame に重ねてあるので、その中央を
 * ダブルクリックすると文字の編集に入る。
 */
async function editAnnotationText(page: Page, text: string): Promise<void> {
  const box = await page.locator(".annotation-overlay-frame").first().boundingBox();
  if (!box) throw new Error("注釈の印が表示されていない");
  await page.mouse.dblclick(box.x + box.width / 2, box.y + box.height / 2);
  const editor = page.locator("textarea.excalidraw-wysiwyg");
  await expect(editor).toBeVisible();
  await editor.fill(text);
  await page.keyboard.press("Escape");
  await expect(editor).toBeHidden();
}
