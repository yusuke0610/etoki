import { expect, test, type Locator, type Page } from "@playwright/test";

import { expectBlockedReason, expectNoAxeViolations } from "./helpers/a11y";
import { holdCreate, holdSave, installApi } from "./helpers/api";
import {
  annotationCard,
  annotationDetail,
  chooseFromMenu,
  drawRectangle,
  interpret,
  newBoardDialog,
  openAnnotationDetail,
  openBoard,
  openBoardMenu,
  openBoardWithMock,
  openMermaidPaste,
  openPanelTab,
  saveScene,
  ekidokiButton,
} from "./helpers/board";
import {
  ANNOTATION_IDS,
  BOARD_ID,
  BOARD_NAME,
  annotations,
  authRequiredMock,
  baseMock,
  board,
  createdRun,
  historyRuns,
  signedIn,
} from "./helpers/fixtures";

/**
 * アクセシビリティの判断が壊れたことに気づくための spec（#80、ADR 0039）。
 *
 * **形の規約は eslint（jsx-a11y）が見る。ここが見るのは判断の規約。**
 * 「押せない理由を `title` に隠さず本文として出し、ボタンから
 * `aria-describedby` で指す」は etoki 固有の判断なので、既製のルールでは
 * 検知できない。**そしてこちらのほうが重い。**
 *
 * **押せないボタンは 1 箇所ずつ数える。** ここに並んでいないボタンは守られて
 * いない。押せないボタンを足したら、ここにも足す。
 */

test.describe("押せない理由が本文として読める", () => {
  // 未保存は押せない理由にならない。「絵解く」は押した操作の中で保存してから
  // 読む（#247）。止めていた頃の理由（ADR 0018）は、保存を挟むことで満たす。
  // LLM が未設定の構成（ADR 0030）。理由は詳細の帯に出して、ボタンがそこを指す。
  // パネルの上の文を指さないのは、パネルが畳まれていることがあるから（#202）。
  test("絵解く：LLM が未設定のとき", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: { interpretation: false, diagramDraft: false, creation: true, sharing: true },
    };
    await openBoardWithMock(page, mock);

    const detail = await openAnnotationDetail(page, "ログイン");
    await expectBlockedReason(
      detail.getByRole("button", { name: "絵解く" }),
      "ETOKI_LLM_API_KEY",
    );
  });

  // 図のドラフトも同じ LLM の設定で決まるが、答えている問いは解釈と別
  // （ADR 0041）。**diagramChat.spec.ts は id の値を見ている。** こちらが見るのは、
  // その先が実在して読めること。
  test("生成する：LLM が未設定のとき", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: { interpretation: false, diagramDraft: false, creation: true, sharing: true },
    };
    await openBoardWithMock(page, mock);
    await openPanelTab(page, "図のドラフト");

    await expectBlockedReason(
      page.getByRole("button", { name: "生成", exact: true }),
      "ETOKI_LLM_API_KEY",
    );
  });

  // 作成は取り消せない（ADR 0009）。何が足りなくて押せないのかが読めないと、
  // 開発者は選び直しようがない。
  test("GitHub に作成する：作るものが 1 件も無いとき", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const card = annotationCard(page, "ログイン");
    const detail = annotationDetail(page, "ログイン");
    await interpret(card);
    await detail.getByRole("button", { name: "GitHub に作成する" }).waitFor();

    // epic を外すと、それを親に持つ issue も一緒に外れる。3 件とも外れる。
    await detail.getByLabel("e1 を作成する").uncheck();

    await expectBlockedReason(
      detail.getByRole("button", { name: "GitHub に作成する" }),
      "作るものが 1 件も選ばれていません。",
    );
  });

  // 選択画面に移るとキャンバスごと外れ、未保存の編集は失われる（ADR 0021）。
  // GitHub が未設定ならボードを作れない（作成先を選べない、ADR 0030）。名前と
  // ひな形を決めたあとで行き止まりにしないよう、押す前に止めて理由を出す（#200）。
  test("新しいボード：GitHub が未設定のとき", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: { interpretation: true, diagramDraft: true, creation: false, sharing: true },
    };
    await installApi(page, mock);
    await page.goto("/");

    await expectBlockedReason(
      page.getByRole("button", { name: "新しいボード", exact: true }),
      "ETOKI_GITHUB_TOKEN",
    );
  });

  test("作成先を変更：未保存のとき", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await drawRectangle(page);

    // メニューの中の理由も本文で読める（ADR 0066）。開かないと DOM に無い。
    const menu = await openBoardMenu(page);
    await expectBlockedReason(
      menu.getByRole("button", { name: "作成先を変更" }),
      "保存してから作成先を変更できます",
    );
  });

  // 取り込みはキャンバスを置き換える（ADR 0044）。作成中に置き換えると、作られた
  // 内容と記録されるハッシュが食い違いうるので、保存と同じ理由で止める。
  test("取り込み：作成中のとき", async ({ page }) => {
    await installApi(page, baseMock());
    // **応答を返さない。** 作成中の画面はこうしないと作れない。`installApi` の
    // あとに登録して、こちらを先に当てる。
    await page.route(
      (url) => /^\/api\/boards\/[^/]+\/annotations\/[^/]+\/items$/.test(url.pathname),
      (route) => {
        // 契約のメソッド以外は捕まえない。何でも受けると、フロントが違う
        // メソッドで叩いていても作成中の画面になる（`.claude/rules/e2e-mocks.md`）。
        if (route.request().method() !== "POST") {
          void route.fallback();
        }
        // POST は応答しないまま握る。それがこのテストの入力。
      },
    );

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    const detail = annotationDetail(page, "ログイン");
    await interpret(card);
    await detail.getByRole("button", { name: "GitHub に作成する" }).click();

    const menu = await openBoardMenu(page);
    await expectBlockedReason(
      menu.getByRole("button", { name: "取り込み", exact: true }),
      "作成が終わるまで取り込めません",
    );
  });

  // 状態は保存済みシーンが基準なので、未保存で消したフレームの注釈が一覧に
  // 残る（ADR 0022）。寄せる先が無い。**カードは押せる**（詳細で GitHub に
  // あるものを読むため）ので、押せないのは詳細の「キャンバスで見る」（#201）。
  test("キャンバスで見る：フレームがキャンバスに無いとき", async ({ page }) => {
    const mock = baseMock();
    mock.annotations[BOARD_ID] = [
      ...annotations(),
      // シーンにこの ID の frame は無い。保存前に消したフレームの注釈。
      { id: "frame-gone", name: "消したフレーム", granularity: "", state: "uncreated" },
    ];
    await openBoardWithMock(page, mock);

    const detail = await openAnnotationDetail(page, "消したフレーム");
    await expectBlockedReason(
      detail.getByRole("button", { name: "キャンバスで見る" }),
      "このフレームはキャンバスにありません。",
    );
  });

  // 読むだけの参加者（ADR 0017）。粒度と種別は見えるが変えられない。何も
  // 言わずに灰色にすると、壊れているのか権限なのかが分からない。
  test("粒度と種別：読むだけの権限のとき", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), role: "viewer" };
    mock.boards = mock.boards.map((b) => ({ ...b, role: "viewer" }));
    await openBoardWithMock(page, mock);

    const detail = await openAnnotationDetail(page, "ログイン");
    for (const name of ["粒度", "種別"]) {
      await expectBlockedReason(detail.getByLabel(name), "読むだけの権限で開いています");
    }
  });

  // 取り消せない操作と保存は相互に排他する（`.claude/rules/async-ui.md`）。
  // **一時的でも押せない理由。** 待てば押せるようになることは、待てると分かって
  // いる人にしか分からない。
  test("GitHub に作成する：保存中のとき", async ({ page }) => {
    await installApi(page, baseMock());
    let release = () => {};
    await holdSave(
      page,
      new Promise<void>((resolve) => {
        release = resolve;
      }),
    );

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // 解釈してからでないと作成ボタンが出ない。保存は解釈結果を捨てるが、
    // 捨てるのは応答が返ってからなので、止めているあいだは並んでいる。
    const card = annotationCard(page, "ログイン");
    const detail = annotationDetail(page, "ログイン");
    await interpret(card);
    await detail.getByRole("button", { name: "GitHub に作成する" }).waitFor();

    await saveScene(page);

    await expectBlockedReason(
      detail.getByRole("button", { name: "GitHub に作成する" }),
      "保存が終わるまで作成できません",
    );

    // **理由が消えることまで見る。** 出しっぱなしの文でも上の検査は通る。
    release();
    await expect(page.getByText("保存が終わるまで作成できません")).toBeHidden();
  });

  // 「作成先を変更」は未保存でも保存中でも押せない。**未保存のほうだけ
  // 見ていると、変更なしで保存を押したあいだが空く。** ボタンは押せないのに
  // 理由が消える、という状態が残る。
  test("作成先を変更：保存中のとき", async ({ page }) => {
    await installApi(page, baseMock());
    let release = () => {};
    await holdSave(
      page,
      new Promise<void>((resolve) => {
        release = resolve;
      }),
    );

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // **描かない。** 描くと未保存の理由のほうが出て、保存中の経路を通らない。
    await saveScene(page);

    // **保存を押してから開く。** 開いてから外を押すと、メニューは閉じる。
    const menu = await openBoardMenu(page);
    const change = menu.getByRole("button", { name: "作成先を変更" });
    await expectBlockedReason(change, "保存が終わるまで作成先を変更できません");

    // 開いたまま待つ。閉じた後に見ると、理由が消えていなくても通る。
    release();
    await expect(change).toBeEnabled();
  });

  test("取り込み：保存中のとき", async ({ page }) => {
    await installApi(page, baseMock());
    let release = () => {};
    await holdSave(
      page,
      new Promise<void>((resolve) => {
        release = resolve;
      }),
    );

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await saveScene(page);

    const menu = await openBoardMenu(page);
    const importButton = menu.getByRole("button", { name: "取り込み", exact: true });
    await expectBlockedReason(importButton, "保存が終わるまで取り込めません");

    // 開いたまま待つ。閉じた後に見ると、理由が消えていなくても通る。
    release();
    await expect(importButton).toBeEnabled();
    await expect(menu.getByText("保存が終わるまで取り込めません")).toBeHidden();
  });

  test("絵解き：取り込み中のとき", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    // loadFromBlob が使う FileReader を止め、ファイルを読んでいる状態を作る。
    await page.evaluate(() => {
      const readAsText = FileReader.prototype.readAsText;
      FileReader.prototype.readAsText = function (blob, encoding) {
        Reflect.set(window, "releaseImport", () => readAsText.call(this, blob, encoding));
      };
    });
    await page.getByLabel("取り込む .excalidraw ファイル").setInputFiles({
      name: "board.excalidraw",
      mimeType: "application/json",
      buffer: Buffer.from(
        JSON.stringify({
          type: "excalidraw",
          version: 2,
          source: "e2e",
          elements: [],
          appState: {},
          files: {},
        }),
      ),
    });

    await expectBlockedReason(
      ekidokiButton(page),
      "取り込みが終わるまで絵解きを始められません",
    );

    await page.evaluate(() => {
      const release = Reflect.get(window, "releaseImport") as unknown;
      if (typeof release === "function") release();
    });
    await expect(
      page.getByText("取り込みが終わるまで絵解きを始められません"),
    ).toBeHidden();
  });

  test("GitHub に作成する：取り込み中のとき", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());

    const card = annotationCard(page, "ログイン");
    const detail = annotationDetail(page, "ログイン");
    await interpret(card);
    const createButton = detail.getByRole("button", { name: "GitHub に作成する" });
    await createButton.waitFor();

    // loadFromBlob が使う FileReader を止め、ファイルを読んでいる状態を作る。
    await page.evaluate(() => {
      const readAsText = FileReader.prototype.readAsText;
      FileReader.prototype.readAsText = function (blob, encoding) {
        Reflect.set(window, "releaseImport", () => readAsText.call(this, blob, encoding));
      };
    });
    await page.getByLabel("取り込む .excalidraw ファイル").setInputFiles({
      name: "board.excalidraw",
      mimeType: "application/json",
      buffer: Buffer.from(
        JSON.stringify({
          type: "excalidraw",
          version: 2,
          source: "e2e",
          elements: [],
          appState: {},
          files: {},
        }),
      ),
    });

    await expectBlockedReason(createButton, "取り込みが終わるまで作成できません");

    // disabled は表示の約束。DOM を直接操作されても処理の入口で拒否する。
    await createButton.evaluate((button: HTMLButtonElement) => {
      button.disabled = false;
      button.click();
    });

    await page.evaluate(() => {
      const release = Reflect.get(window, "releaseImport") as unknown;
      if (typeof release === "function") release();
    });
    await expect(page.getByText("取り込みが終わるまで作成できません")).toBeHidden();
    expect(mock.createRequests).toHaveLength(0);
  });

  // 逆向き。作成中は保存させない。**押せない理由を `title` に置くと、この
  // テストが落ちる。** `disabled` なボタンはフォーカスも当たらないので、
  // ホバーできない利用者には届かない。
  test("絵解き：作成中のとき", async ({ page }) => {
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
    const detail = annotationDetail(page, "ログイン");
    await interpret(card);
    await detail.getByRole("button", { name: "GitHub に作成する" }).click();

    await expectBlockedReason(
      ekidokiButton(page),
      "作成が終わるまで絵解きを始められません",
    );

    release();
    await expect(page.getByText("作成が終わるまで絵解きを始められません")).toBeHidden();
  });

  // 理由を出す側が壊れたら落ちること自体を確かめる。**ここが落ちなければ、
  // 上のどれも何も守っていない。**
  // 届いたか分からない項目は選び直させない（ADR 0056、#170）。受理されていた
  // 場合、もう一度送ると消せない draft issue が重複する。
  //
  // **止めているのはチェックボックス。** ボタンと同じで、押せない理由が画面に
  // 無い状態は許されない。
  test("作成する：届いたか分からない項目のとき", async ({ page }) => {
    const mock = baseMock();
    const [confirmed, lost] = createdRun().items.slice(0, 2);
    if (confirmed === undefined || lost === undefined) throw new Error("fixture");
    mock.createItems = {
      status: 201,
      body: {
        ...createdRun(),
        items: [confirmed, { ...lost, itemId: "", confirmed: false }],
        incomplete: true,
        error: "Post ...: EOF",
      },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    const detail = annotationDetail(page, "ログイン");
    await interpret(card);
    await detail.getByRole("button", { name: "GitHub に作成する" }).click();
    await detail.locator(".unconfirmed-items").waitFor();

    await expectBlockedReason(
      detail.getByLabel("i1 を作成する"),
      /この下書きからは送り直せません/,
    );
  });

  test("理由の要素が消えたら落ちる", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await drawRectangle(page);

    const menu = await openBoardMenu(page);
    const button = menu.getByRole("button", { name: "作成先を変更" });
    await expectBlockedReason(button, "保存してから作成先を変更できます");

    // 理由の本文だけを消す。ボタンは押せないまま、指す先が無くなる。
    await page.locator("#target-change-blocked").evaluate((el) => el.remove());

    await expect(
      expectBlockedReason(button, "保存してから作成先を変更できます"),
    ).rejects.toThrow();
  });
});

/**
 * 形の規約のうち、静的解析では見えないもの。
 *
 * **jsx-a11y と重ならない。** あちらは JSX の属性しか見ないので、実際に描いた
 * 色のコントラストは見えない。入れた時点で `.hint` が 4.48:1（AA は 4.5:1）で
 * 落ちていた。**押せない理由を出しているのが、その `.hint` だった。**
 *
 * **ライトとダークの両方で掛ける**（ADR 0055）。色は変数を差し替えるだけなので、
 * 片方で通っても、もう片方の文字色が AA を切っていることは見えない。
 */
for (const colorScheme of ["light", "dark"] as const) {
  test.describe(`axe（etoki が書いた DOM・${colorScheme}）`, () => {
    test.use({ colorScheme });

    /*
     * ログイン画面。**ボードを開く経路からは一度も通らない**ので、他の検査に
     * ついでに掛かることがない。主となる操作のボタン（`button.primary`）と、
     * 絵に添えた説明が出るのもこの画面だけ。
     */
    test("ログイン画面", async ({ page }) => {
      await installApi(page, authRequiredMock());

      await page.goto("/");
      await page.getByRole("button", { name: "GitHub でログイン" }).waitFor();

      await expectNoAxeViolations(page);
    });

    /*
     * キャンバスのメニュー（ADR 0065）。**開かないと DOM に出ない**ので、他の
     * 検査では一度も掛かっていない。etoki の項目と、押せない理由の文
     * （ADR 0066）が両方出る状態にして掛ける。未保存にしておけば「作成先を変更」
     * が押せなくなり、理由の文がメニューの中に出る。
     */
    test("メニューを開いた状態", async ({ page }) => {
      await openBoardWithMock(page, baseMock());
      await drawRectangle(page);
      const menu = await openBoardMenu(page);
      await menu.getByText("保存してから作成先を変更できます").waitFor();

      await expectNoAxeViolations(page);
    });

    test("ボードの一覧", async ({ page }) => {
      await installApi(page, baseMock());

      await page.goto("/");
      await page.locator(".board-list").waitFor();

      await expectNoAxeViolations(page);
    });

    /*
     * 新しいボードのダイアログと、利用者のメニュー（#200）。**どちらも開かないと
     * 見えない**ので、一覧の検査には中身が掛かっていない。メニューはログインした
     * 構成にしか出ない。
     */
    test("新しいボードのダイアログを開いた状態", async ({ page }) => {
      await installApi(page, baseMock());
      await page.goto("/");
      await newBoardDialog(page);

      await expectNoAxeViolations(page);
    });

    test("利用者のメニューを開いた状態", async ({ page }) => {
      const mock = baseMock();
      mock.session = { status: 200, body: signedIn() };
      await installApi(page, mock);
      await page.goto("/");
      await page.getByRole("button", { name: "Octo Cat" }).click();
      await page.getByRole("button", { name: "ログアウト" }).waitFor();

      await expectNoAxeViolations(page);
    });

    test("ボードを開いた状態", async ({ page }) => {
      await openBoardWithMock(page, baseMock());

      await expectNoAxeViolations(page);
    });

    // 図のドラフトのチャットは、右のパネルのタブの 1 つ（ADR 0041 / 0065）。
    // **開かないと DOM に出ない**ので、上の 2 つでは一度も掛かっていない。
    // 生成結果を出したところまで開けて、`.diagram-mermaid` と
    // 「ここまでのやりとり」まで含めて見る。
    test("図のドラフトを生成した状態", async ({ page }) => {
      await openBoardWithMock(page, baseMock());

      await openPanelTab(page, "図のドラフト");
      await page.getByLabel("図への指示").fill("注文から出荷までの流れ");
      await page.getByRole("button", { name: "生成", exact: true }).click();
      await page.locator(".diagram-mermaid").waitFor();

      await expectNoAxeViolations(page);
    });

    // mermaid の貼り付けは、図のドラフトのタブの中で切り替えて出す（ADR 0062、
    // `DiagramTab`）。**開かないと DOM に出ない。** 構文エラーまで出して、失敗の
    // 帯と畳んだ本文を含めて見る。
    test("mermaid の貼り付けで構文エラーを出した状態", async ({ page }) => {
      await openBoardWithMock(page, baseMock());

      await openMermaidPaste(page);
      await page.getByLabel("貼る mermaid").fill("flowchart TD\n  A[[[[ -->");
      await page.getByRole("button", { name: "キャンバスに置く" }).click();
      await page.locator(".mermaid-paste .error").waitFor();

      await expectNoAxeViolations(page);
    });

    // 作った項目の説明は、選択の外れた（薄く描く）行の中に出る（ADR 0052）。
    // **作成が済まないと DOM に出ない**ので、上の 2 つでは一度も掛かっていない。
    test("作成が済んだ下書き", async ({ page }) => {
      await installApi(page, baseMock());

      await page.goto("/");
      await openBoard(page, BOARD_NAME);

      const card = annotationCard(page, "ログイン");
      const detail = annotationDetail(page, "ログイン");
      await interpret(card);
      await detail.getByRole("button", { name: "GitHub に作成する" }).click();
      await detail.getByText("3 件を作成しました。").waitFor();
      // 検査したいのは作成済みの印が付いた下書き。完了の文言は作成結果だけで
      // 出るので、下書きへの反映まで待たないと通常の下書きを検査して通る。
      await expect(detail.locator(".badge-created", { hasText: "作成した" })).toHaveCount(
        3,
      );
      await expect(
        detail.getByText(
          "作成しました。選び直すと、作成した draft issue を書き換えます。",
        ),
      ).toHaveCount(3);

      await expectNoAxeViolations(page);
    });

    // 届いたか分からない書き込みの帯（ADR 0056）。**「作れた」とも「失敗した」とも
    // 見えない色**に寄せてあるので、コントラストは描いて測るしかない。
    // **作成がこけないと DOM に出ない**ので、上の検査では一度も掛かっていない。
    test("届いたか分からない書き込みが出ている状態", async ({ page }) => {
      const mock = baseMock();
      mock.annotations[BOARD_ID] = annotations().map((a) =>
        a.id === ANNOTATION_IDS.created
          ? {
              ...a,
              unconfirmedItems: [
                {
                  itemId: "",
                  kind: "issue" as const,
                  title: "確認できていないほう",
                  body: "本文",
                  localId: "i9",
                  action: "created" as const,
                  confirmed: false,
                },
              ],
            }
          : a,
      );
      await installApi(page, mock);

      await page.goto("/");
      await openBoard(page, BOARD_NAME);
      // カードには件数を、詳細には一覧を出す（#201）。両方が描かれた状態で掛ける。
      await annotationCard(page, "パスワード再設定")
        .locator(".annotation-warning")
        .waitFor();
      const detail = await openAnnotationDetail(page, "パスワード再設定");
      await detail.locator(".unconfirmed-items").waitFor();

      await expectNoAxeViolations(page);
    });

    // 削除の確認は etoki が自前で `role` を書いている唯一の場所（ADR 0042）。
    // **開かないと DOM に出ない**ので、上の 2 つでは一度も掛かっていない。
    test("削除の確認を開いた状態", async ({ page }) => {
      const mock = baseMock();
      // 件数は文言そのもの。0 件だと分岐の片方しか描かれない。
      mock.deletion = { [BOARD_ID]: { status: 200, body: { recordedItemCount: 3 } } };
      await openBoardWithMock(page, mock);

      await chooseFromMenu(page, "ボードを削除");
      await page.getByRole("alertdialog").waitFor();

      await expectNoAxeViolations(page);
    });

    // 右のパネルを畳んだ帯（#202）。開くまで DOM に出ない。件数を添えた縦書きの
    // ボタンが並ぶ唯一の場所。
    test("右のパネルを畳んだ状態", async ({ page }) => {
      await openBoardWithMock(page, baseMock());

      await page.getByRole("button", { name: "パネルを閉じる" }).click();
      await page.getByRole("navigation", { name: "パネル" }).waitFor();

      await expectNoAxeViolations(page);
    });

    // 注釈の詳細（#201）。**GitHub にあるものと実行の履歴は押すまで DOM の上で
    // 隠れていて**、解釈の検査ではどちらも開かない。両方を開き、詳細を開いた
    // 直後の焦点（面そのもの）も含めて掛ける。
    test("注釈の詳細で GitHub にあるものと実行の履歴を開いた状態", async ({ page }) => {
      const mock = baseMock();
      mock.runs = { [ANNOTATION_IDS.created]: { status: 200, body: historyRuns() } };
      await openBoardWithMock(page, mock);

      const detail = await openAnnotationDetail(page, "パスワード再設定");
      await detail.getByRole("button", { name: "GitHub にある 2 件" }).click();
      await detail.getByRole("button", { name: "実行の履歴" }).click();
      await detail.getByRole("button", { name: "履歴を読み込む" }).click();
      await detail.locator(".run-history").getByText("再設定メールを送る").waitFor();

      await expectNoAxeViolations(page);
    });

    // メンバーのパネルも独立した領域で、開くまで DOM に出ない。行ごとのボタンが
    // 並ぶ唯一の画面でもある（`.claude/rules/async-ui.md` の「行固有の
    // accessible name」）。
    test("メンバーを開いた状態", async ({ page }) => {
      const mock = baseMock();
      mock.members = {
        [BOARD_ID]: [
          {
            userId: "user-alice",
            login: "alice",
            displayName: "Alice",
            role: "owner",
            createdAt: "2026-08-01T09:00:00Z",
          },
          {
            userId: "user-bob",
            login: "bob",
            displayName: "Bob",
            role: "editor",
            createdAt: "2026-08-03T09:00:00Z",
          },
        ],
      };
      await openBoardWithMock(page, mock);

      await openPanelTab(page, "メンバー");
      await page.getByText("Bob").waitFor();

      await expectNoAxeViolations(page);

      // 招待する前の確認（ADR 0053）も、確認を押すまで DOM に出ない。
      await page.getByLabel("招待する login").fill("carol");
      await page.getByRole("button", { name: "確認する" }).click();
      await page.getByRole("group", { name: "招待する相手の確認" }).waitFor();

      await expectNoAxeViolations(page);
    });

    // 解釈結果は画面の中でいちばん要素が多い。作る前に読ませる場所なので
    // （ADR 0024）、読めないものが混じっていないかをここで見る。
    test("解釈結果を出した状態", async ({ page }) => {
      await openBoardWithMock(page, baseMock());

      const card = annotationCard(page, "ログイン");
      const detail = annotationDetail(page, "ログイン");
      await interpret(card);
      await detail.getByRole("button", { name: "GitHub に作成する" }).waitFor();
      // 畳んだままでは中を見られない。作成前に読ませる本文まで含めて掛ける。
      for (const summary of await detail.getByText("本文", { exact: true }).all()) {
        await summary.click();
      }

      await expectNoAxeViolations(page);
    });

    /*
     * スマホの置き方（#199）。右上と下の帯の中身は Excalidraw の外の帯に移り、
     * パネルと詳細は全面に開く。**どれも広い画面の検査には一度も掛からない。**
     * 帯は拡張点の外なので、`ETOKI_INSIDE_CANVAS` に足さなくても検査に入る。
     */
    test.describe("スマホの大きさ", () => {
      test.use({ viewport: { width: 375, height: 812 } });

      test("ボード", async ({ page }) => {
        await openBoardWithMock(page, baseMock());
        await drawRectangle(page);
        await page.locator(".board-bar-status .dirty").waitFor();

        await expectNoAxeViolations(page);
      });

      test("全面のパネルと詳細", async ({ page }) => {
        await openBoardWithMock(page, baseMock());
        await openPanelTab(page, "注釈");

        await expectNoAxeViolations(page);

        await interpret(annotationCard(page, "ログイン"));
        await annotationDetail(page, "ログイン")
          .getByRole("button", { name: "GitHub に作成する" })
          .waitFor();

        await expectNoAxeViolations(page);
      });
    });

    // 通知は**失敗しないと DOM に出ない**（ADR 0058）。出ている状態で axe を
    // 通さないと、role や名前が崩れても気づく経路が無い。通知には「再試行」と
    // 「閉じる」が並ぶので、名前の衝突もここで見る。
    test("通知が出ている状態で違反が無い", async ({ page }) => {
      const mock = baseMock();
      mock.saveSceneError = {
        status: 500,
        body: { code: "internal", error: "internal error" },
      };
      await openBoardWithMock(page, mock);
      await drawRectangle(page);
      await saveScene(page);
      await expect(page.getByRole("alert")).toContainText("保存できませんでした");

      await expectNoAxeViolations(page);
    });
  });
}

/**
 * キャンバスの中に置いた etoki の部品が、etoki の色を読んでいるか（ADR 0065）。
 *
 * **axe では見えない。** ライブラリの層に重なっていて背景が決まらないので、
 * 色の検査は違反ではなく判定不能になる（`helpers/a11y.ts`）。一方でライブラリは
 * `.excalidraw` に `--color-warning` などを自前で持っていて、中に置いた部品は
 * そちらの薄い色を読む。実際に、右上の「未保存」がほぼ読めない色になっていた。
 *
 * **値ではなく、外で同じ変数を読んだ色と比べる。** 値を書き写すと、配色を
 * 変えた日にここだけが古くなる。
 */
for (const colorScheme of ["light", "dark"] as const) {
  test.describe(`キャンバスの中の etoki の部品（${colorScheme}）`, () => {
    test.use({ colorScheme });

    test("ライブラリと名前がぶつかる色も、外と同じ色で読む", async ({ page }) => {
      await openBoardWithMock(page, baseMock());
      await drawRectangle(page);

      // 右上の「未保存」は --color-warning を読む。
      const dirty = page.locator(".excalidraw .board-status .dirty");
      await expect(dirty).toBeVisible();
      expect(await colorOf(dirty)).toBe(
        await colorOutsideCanvas(page, "--color-warning"),
      );

      // メニューの「ボードを削除」は --color-danger を読む。
      const menu = await openBoardMenu(page);
      const deleteItem = menu.getByRole("button", { name: "ボードを削除" });
      expect(await colorOf(deleteItem)).toBe(
        await colorOutsideCanvas(page, "--color-danger"),
      );
    });
  });
}

async function colorOf(locator: Locator): Promise<string> {
  return locator.evaluate((el) => getComputedStyle(el).color);
}

/** キャンバスの外（body 直下）で、その変数を文字色に使ったときの色。 */
async function colorOutsideCanvas(page: Page, variable: string): Promise<string> {
  return page.evaluate((name) => {
    const probe = document.createElement("span");
    probe.style.color = `var(${name})`;
    document.body.appendChild(probe);
    const color = getComputedStyle(probe).color;
    probe.remove();
    return color;
  }, variable);
}
