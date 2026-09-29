import { expect, test } from "@playwright/test";

import { holdInterpret, installApi } from "./helpers/api";
import {
  annotationCard,
  annotationDetail,
  openBoard,
  openBoardWithMock,
} from "./helpers/board";
import { BOARD_NAME, baseMock } from "./helpers/fixtures";

/**
 * 注釈の詳細（`AnnotationDetail`）。解釈の結果と下書きの手直しを、キャンバスの
 * 上に広く開く。
 *
 * **守りたいのは「閉じても手直しが消えない」こと。** 閉じるたびに外すと、直した
 * ぶんが解釈の出したままに戻る。作成は取り消せない（ADR 0009）ので、戻ったことに
 * 気づかずに押すと、直す前の中身で作られる。
 */
test.describe("注釈の詳細", () => {
  test("閉じて開き直しても、手直し中の下書きは消えない", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const card = annotationCard(page, "ログイン");
    const detail = annotationDetail(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await detail.getByLabel("e1 のタイトル").fill("ログイン基盤（手直し済み）");

    await detail.getByRole("button", { name: "閉じる" }).click();
    await expect(detail).toHaveCount(0);
    await card.getByRole("button", { name: "解釈結果を開く" }).click();

    await expect(detail.getByLabel("e1 のタイトル")).toHaveValue(
      "ログイン基盤（手直し済み）",
    );
  });

  // 1 枠を使い回す作りだと、閉じて開き直すのは通っても、別の注釈を開いた時点で
  // 前の注釈の手直しが消える。
  test("別の注釈の詳細を開いてから戻っても、手直しは残る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const card = annotationCard(page, "ログイン");
    const detail = annotationDetail(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await detail.getByLabel("e1 のタイトル").fill("ログイン基盤（手直し済み）");

    // 開くのは 1 つずつ。別の注釈を解釈すると、そちらに切り替わる。
    await annotationCard(page, "パスワード再設定")
      .getByRole("button", { name: "解釈する" })
      .click();
    const other = annotationDetail(page, "パスワード再設定");
    await expect(other.getByLabel("e1 のタイトル")).toHaveValue("ログイン基盤");
    await expect(detail).toHaveCount(0);

    await card.getByRole("button", { name: "解釈結果を開く" }).click();
    await expect(detail.getByLabel("e1 のタイトル")).toHaveValue(
      "ログイン基盤（手直し済み）",
    );
  });

  // 開いたことに気づけないと、キーボードの利用者は結果を探しに行けない。閉じた
  // あとに焦点が隠れた面に残ると、次の Tab が画面の先頭から始まる。
  test("開くと焦点が詳細へ移り、Escape で閉じると開くボタンへ戻る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const card = annotationCard(page, "ログイン");
    const detail = annotationDetail(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await expect(detail).toBeFocused();
    await expect(detail.getByLabel("e1 のタイトル")).toHaveValue("ログイン基盤");

    await page.keyboard.press("Escape");

    await expect(detail).toHaveCount(0);
    const reopen = card.getByRole("button", { name: "解釈結果を開く" });
    await expect(reopen).toBeFocused();
    // カードは注釈の数だけ並ぶ。名前に注釈を含めないと、読み上げではどの注釈の
    // ボタンか区別できない。
    // 隠した注釈名との間の空白はブラウザが挟む（位置を外した要素を区切る）ので
    // 見ない。
    await expect(reopen).toHaveAccessibleName(/^解釈結果を開く\s*（ログイン）$/);
    await expect(card.getByRole("button", { name: "解釈する" })).toHaveAccessibleName(
      /^解釈する\s*（ログイン）$/,
    );
  });

  // 解釈は LLM を待つので時間がかかる。待つあいだに閉じてキャンバスへ戻れるが、
  // 開き直す口が結果が返るまで出ないと、返ってきたことに気づけない。
  test("解釈中に閉じても、開き直して結果を待てる", async ({ page }) => {
    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    await installApi(page, baseMock());
    await holdInterpret(page, held);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    const detail = annotationDetail(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await expect(detail.getByText("解釈しています…")).toBeVisible();

    await detail.getByRole("button", { name: "閉じる" }).click();
    await card.getByRole("button", { name: "解釈結果を開く" }).click();
    await expect(detail.getByText("解釈しています…")).toBeVisible();

    release();
    await expect(detail.getByLabel("e1 のタイトル")).toHaveValue("ログイン基盤");
  });
});
