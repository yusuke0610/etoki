import { expect, test } from "@playwright/test";

import { holdInterpret, holdSave, installApi } from "./helpers/api";
import {
  annotationCard,
  annotationDetail,
  interpret,
  openAnnotationDetail,
  openBoard,
  openBoardWithMock,
  openPanelTab,
  saveScene,
  ekidokiButton,
} from "./helpers/board";
import { BOARD_ID, BOARD_NAME, annotations, baseMock } from "./helpers/fixtures";

/**
 * 注釈の詳細（`AnnotationDetail`）。カードを押すと開き、粒度と種別、GitHub に
 * あるもの、実行の履歴、解釈の結果と下書きの手直しを、キャンバスの上に広く出す
 * （#201）。
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
    await interpret(card);
    await detail.getByLabel("e1 のタイトル").fill("ログイン基盤（手直し済み）");

    await detail.getByRole("button", { name: "閉じる" }).click();
    await expect(detail).toHaveCount(0);
    await card.locator(".annotation-open").click();

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
    await interpret(card);
    await detail.getByLabel("e1 のタイトル").fill("ログイン基盤（手直し済み）");

    // 開くのは 1 つずつ。別の注釈を解釈すると、そちらに切り替わる。
    await interpret(annotationCard(page, "パスワード再設定"));
    const other = annotationDetail(page, "パスワード再設定");
    await expect(other.getByLabel("e1 のタイトル")).toHaveValue("ログイン基盤");
    await expect(detail).toHaveCount(0);

    await card.locator(".annotation-open").click();
    await expect(detail.getByLabel("e1 のタイトル")).toHaveValue(
      "ログイン基盤（手直し済み）",
    );
  });

  // 開いたことに気づけないと、キーボードの利用者は結果を探しに行けない。閉じた
  // あとに焦点が隠れた面に残ると、次の Tab が画面の先頭から始まる。
  test("開くと焦点が詳細へ移り、Escape で閉じるとカードへ戻る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const card = annotationCard(page, "ログイン");
    const detail = await openAnnotationDetail(page, "ログイン");
    await expect(detail).toBeFocused();

    await page.keyboard.press("Escape");

    await expect(detail).toHaveCount(0);
    // カードのボタンの名前は注釈の見出しなので、読み上げでどの注釈か区別できる。
    await expect(
      card.getByRole("button", { name: "ログイン", exact: true }),
    ).toBeFocused();
  });

  // 帯の「解釈する」は押すと解釈中になって押せなくなり、焦点が body に落ちる。
  // 落ちたままだと Escape が詳細に届かず、キーボードの利用者が面の中で迷子になる。
  test("解釈を押しても焦点は詳細に残り、Escape で閉じられる", async ({ page }) => {
    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    await installApi(page, baseMock());
    await holdInterpret(page, held);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const detail = await openAnnotationDetail(page, "ログイン");
    await detail.getByRole("button", { name: "絵解く" }).press("Enter");
    await expect(detail.getByRole("button", { name: "絵解き中…" })).toBeDisabled();
    await expect(detail).toBeFocused();

    await page.keyboard.press("Escape");
    await expect(detail).toHaveCount(0);
    release();
  });

  // 粒度と種別を選ぶ場所が詳細なので、解釈の前でも開ける（#201）。粒度は
  // `content_hash` の入力で、変えると未保存になる。**未保存でも「絵解く」は押せる。**
  // 押した操作の中で保存してから読む（#247）。保存が要るのは etoki の都合
  // （テキストは保存済みシーンから取る、ADR 0018）で、押す人が知らなくてよい。
  //
  // **読むのは保存が届いてから。** 先に読むと、保存が引いた解釈を捨てる
  // （`discardAfterSave`）ので、出た結果が直後に消える。保存を止めて確かめる。
  test("詳細で粒度を変えても絵解ける。押すと保存してから読む", async ({ page }) => {
    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    const mock = await installApi(page, baseMock());
    await holdSave(page, held);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const detail = await openAnnotationDetail(page, "ログイン");
    // 解釈するまで作成のボタンは出さない。
    await expect(detail.getByRole("button", { name: "GitHub に作成する" })).toHaveCount(
      0,
    );

    await detail.getByLabel("粒度").selectOption("epic");
    // 保存済みの粒度は保存するまで古い。選んだ値が選択欄に残っていること（#214）。
    await expect(detail.getByLabel("粒度")).toHaveValue("epic");
    const button = detail.getByRole("button", { name: "絵解く" });
    await expect(button).toBeEnabled();
    await expect(
      detail.getByText("押すと保存してから読みます", { exact: false }),
    ).toBeVisible();

    // 止めた保存はモックまで届かないので、送ったことはリクエストで見る。
    const saving = page.waitForRequest(
      (r) =>
        r.method() === "PUT" &&
        new URL(r.url()).pathname === `/api/boards/${BOARD_ID}/scene`,
    );
    await button.click();
    await saving;
    await expect(detail.getByRole("button", { name: "絵解き中…" })).toBeDisabled();
    // 保存が届くまで読まない。
    expect(mock.interpretRequests).toHaveLength(0);

    release();
    await expect(detail.getByRole("button", { name: "GitHub に作成する" })).toBeVisible();
    expect(mock.interpretRequests).toHaveLength(1);
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    await expect(
      detail.getByText("押すと保存してから読みます", { exact: false }),
    ).toHaveCount(0);
  });

  // 保存できなかったら読まない。読むテキストは保存済みシーンから取るので、
  // 書けていないシーンを前提に読むと、画面の画像と食い違う（ADR 0018）。
  // 理由は保存の失敗として通知が言う。
  test("絵解くときの保存に失敗したら、読まない", async ({ page }) => {
    const mock = baseMock();
    mock.saveSceneError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    const installed = await openBoardWithMock(page, mock);

    const detail = await openAnnotationDetail(page, "ログイン");
    await detail.getByLabel("粒度").selectOption("epic");
    await detail.getByRole("button", { name: "絵解く" }).click();

    await expect(page.getByRole("alert")).toContainText("保存できませんでした");
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    // 「読まなかった」は、読まれたあとにしか出ない表示を待ってからでないと
    // 確かめられない（読む前に数えると、遅れて出るリクエストを見逃す）。保存を
    // 直してもう一度押し、結果が出てから数える。1 回目で読んでいたら 2 件になる。
    delete installed.saveSceneError;
    await detail.getByRole("button", { name: "絵解く" }).click();
    await expect(detail.getByRole("button", { name: "GitHub に作成する" })).toBeVisible();
    expect(installed.interpretRequests).toHaveLength(1);
  });

  // 粒度と種別の選択欄が出すのはキャンバスに書いた値。保存済みの値に「選んだ
  // 値」を重ねて出す作りだと、保存中に元の値へ選び直したところで重ねた値を
  // 手放し、遅れて届いた前の保存（epic）を出してしまう（#214）。キャンバスは
  // 「指定なし」なのに選択欄だけが epic になる。
  test("保存中に元の粒度へ選び直したら、前の保存が届いても選び直した値を出す", async ({
    page,
  }) => {
    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    const mock = await installApi(page, baseMock());
    await holdSave(page, held);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const detail = await openAnnotationDetail(page, "ログイン");
    const select = detail.getByLabel("粒度");
    await select.selectOption("epic");
    await saveScene(page);
    // 保存が epic のシーンを送ったあとで、元の「指定なし」へ選び直す。
    await select.selectOption({ label: "指定なし" });
    await expect(select).toHaveValue("");

    // 届くのは epic で保存した結果。
    mock.annotations[BOARD_ID] = annotations().map((a) =>
      a.name === "ログイン" ? { ...a, granularity: "epic" } : a,
    );
    release();
    await expect(ekidokiButton(page)).toBeEnabled();
    await expect(
      annotationCard(page, "ログイン").getByText("粒度 epic", { exact: false }),
    ).toBeVisible();

    await expect(select).toHaveValue("");
    // 保存したあとで選び直したので、キャンバスは保存済みと違う。
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
  });

  // 「元に戻す」はキャンバスの要素を戻すだけで、保存済みの値は変わらない。
  // 選択欄がキャンバスを見ていないと、戻したあとも選んだ値を出し続ける。
  test("粒度を選んで元に戻したら、選択欄も戻る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const detail = await openAnnotationDetail(page, "ログイン");
    const select = detail.getByLabel("粒度");
    await select.selectOption("epic");
    await expect(select).toHaveValue("epic");

    await page.getByRole("button", { name: "元に戻す" }).click();
    await expect(select).toHaveValue("");
  });

  // 状態は保存済みシーンが基準なので、フレームを消して保存すると注釈ごと一覧から
  // 消える。開いていた詳細も面ごと外れるので、**焦点が行き場を失う。** 一覧の
  // 見出しへ移さないと、次の Tab が画面の先頭から始まる。
  test("開いていた注釈が保存で消えたら詳細を閉じ、焦点を一覧へ移す", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());

    const detail = await openAnnotationDetail(page, "ログイン");
    // 保存のあとに引き直す一覧には、もうこの注釈が無い。
    mock.annotations[BOARD_ID] = annotations().filter((a) => a.name !== "ログイン");
    // 詳細の中に焦点を置いたまま保存する。
    await detail.getByLabel("粒度").selectOption("epic");
    await page.keyboard.press("ControlOrMeta+s");
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    await expect(detail).toHaveCount(0);
    await expect(page.getByRole("heading", { name: /^注釈 2 件/ })).toBeFocused();
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
    await interpret(card);
    await expect(detail.getByText("絵解きしています…")).toBeVisible();

    await detail.getByRole("button", { name: "閉じる" }).click();
    await card.locator(".annotation-open").click();
    await expect(detail.getByText("絵解きしています…")).toBeVisible();

    release();
    await expect(detail.getByLabel("e1 のタイトル")).toHaveValue("ログイン基盤");
  });

  // 切れると: 詳細が開いているあいだに同じ注釈のカードをキーボードで押しても、
  // 焦点がカードに残る。開いたことに気づけない。注釈 ID だけを見ていると、
  // 開いたままの注釈では effect が走り直さない。
  test("開いたままの注釈のカードを押し直しても、焦点が詳細へ移る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const card = annotationCard(page, "ログイン");
    const detail = await openAnnotationDetail(page, "ログイン");
    await expect(detail).toBeFocused();

    await card.getByRole("button", { name: "ログイン", exact: true }).focus();
    await page.keyboard.press("Enter");

    await expect(detail).toBeFocused();
  });

  // 開いたまま保存すると解釈は捨てられる。カードは解釈の有無に関わらず残るので、
  // 閉じたらそこへ戻る（#198 のころは戻り先のボタンごと消えた）。
  test("保存で解釈が捨てられても、閉じるとカードへ焦点が戻る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const card = annotationCard(page, "ログイン");
    const detail = annotationDetail(page, "ログイン");
    await interpret(card);
    await expect(detail.getByLabel("e1 のタイトル")).toHaveValue("ログイン基盤");

    // 詳細はキャンバスの上に開いているので、開いたまま描き足せない。保存は未保存
    // でなくても押せ、どの保存でも解釈は捨てられる。
    await saveScene(page);
    await expect(
      detail.getByText("まだ絵解きしていません", { exact: false }),
    ).toBeVisible();

    await detail.focus();
    await page.keyboard.press("Escape");

    await expect(detail).toHaveCount(0);
    await expect(
      card.getByRole("button", { name: "ログイン", exact: true }),
    ).toBeFocused();
  });

  // 切れると: 開いたまま別のタブへ切り替えると、カードは DOM に残ったまま隠れる。
  // 隠れたボタンは焦点を受けられず、閉じても焦点が隠した面の中に取り残される。
  test("別のタブへ切り替えてから閉じると、選んでいるタブへ焦点が戻る", async ({
    page,
  }) => {
    await openBoardWithMock(page, baseMock());

    const detail = await openAnnotationDetail(page, "ログイン");
    await openPanelTab(page, "メンバー");
    await detail.focus();
    await page.keyboard.press("Escape");

    await expect(detail).toHaveCount(0);
    await expect(page.getByRole("tab", { name: "メンバー", exact: true })).toBeFocused();
  });

  // 切れると: パネルを畳むとタブの列ごと隠れる（#202）。カードにもタブにも戻れず、
  // 焦点が隠した面の中に取り残される。
  test("パネルを畳んでから閉じると、畳んだ帯のいまのタブへ焦点が戻る", async ({
    page,
  }) => {
    await openBoardWithMock(page, baseMock());

    const detail = await openAnnotationDetail(page, "ログイン");
    await page.getByRole("button", { name: "パネルを閉じる" }).click();
    const rail = page.getByRole("navigation", { name: "パネル" });
    await expect(rail).toBeVisible();

    await detail.focus();
    await page.keyboard.press("Escape");

    await expect(detail).toHaveCount(0);
    await expect(rail.getByRole("button", { name: /^注釈/ })).toBeFocused();
  });
});
