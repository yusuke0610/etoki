import { describe, expect, it, vi } from "vitest";

import { ApiError } from "./boards";
import {
  canvasMermaidPasteFailure,
  describeFailure,
  ERROR_MESSAGES,
  mermaidPasteFailure,
  PROJECT_LIST_MESSAGES,
  partialCreationFailure,
  sceneFileUnreadableFailure,
  sceneUnreadableFailure,
} from "./errorMessage";

describe("describeFailure", () => {
  it("既知の code は表の文言を出し、サーバーの本文は畳んだ側に回す", () => {
    const e = new ApiError(409, "scene_conflict", "etoki: board scene was updated");

    expect(describeFailure("保存できませんでした", e)).toEqual({
      message: `保存できませんでした: ${ERROR_MESSAGES.scene_conflict}`,
      detail: "etoki: board scene was updated",
    });
  });

  // 同じ 409 でも打ち手が違う。ここが畳まれると、画面は「衝突しました」としか
  // 言えなくなる（#86 の出発点）。
  it("同じステータスでも code が違えば違う打ち手を出す", () => {
    const conflict = describeFailure(
      "保存できませんでした",
      new ApiError(409, "scene_conflict", "hint"),
    );
    const locked = describeFailure(
      "設定できませんでした",
      new ApiError(409, "target_locked", "hint"),
    );

    expect(conflict.message).not.toBe(locked.message);
  });

  // サーバーのほうが新しいと、まだ知らない code が来る。畳んで隠すと画面は
  // 何も言えなくなるので、そのときだけ本文を前に出す。
  it("知らない code は本文をそのまま見せる", () => {
    const e = new ApiError(418, "brand_new_code", "サーバーからの説明");

    expect(describeFailure("できませんでした", e)).toEqual({
      message: "できませんでした: サーバーからの説明",
      detail: "",
    });
  });

  // 場面ごとの上書き（ADR 0034、#293）。作成先の選択で Project を引いて拒まれた
  // ときは、リポジトリの権限だけを案内すると直らない場合がある。
  it("場面の上書きに載っている code は、表より上書きの文を出す", () => {
    const e = new ApiError(403, "forbidden_project", "github graphql: FORBIDDEN");

    expect(
      describeFailure("プロジェクトを取得できませんでした", e, PROJECT_LIST_MESSAGES),
    ).toEqual({
      message: `プロジェクトを取得できませんでした: ${PROJECT_LIST_MESSAGES.forbidden_project}`,
      detail: "github graphql: FORBIDDEN",
    });
    // 上書きは表を書き換えない。作成で拒まれた場面は元の文のまま。
    expect(describeFailure("作成できませんでした", e).message).toBe(
      `作成できませんでした: ${ERROR_MESSAGES.forbidden_project}`,
    );
  });

  // 上書きに無い code まで消すと、その場面で起きたほかの失敗が何も言えなくなる。
  it("場面の上書きに無い code は、表の文を出す", () => {
    const e = new ApiError(502, "github_unavailable", "hint");

    expect(
      describeFailure("プロジェクトを取得できませんでした", e, PROJECT_LIST_MESSAGES)
        .message,
    ).toBe(`プロジェクトを取得できませんでした: ${ERROR_MESSAGES.github_unavailable}`);
  });

  // 原因を決めつけない（#293）。書く権限と、GitHub App で個人アカウントの
  // Project に触れないことの両方を並べる。
  it("Project の取得を拒まれた場面では、権限と個人アカウントの両方を言う", () => {
    expect(PROJECT_LIST_MESSAGES.forbidden_project).toContain("書く権限");
    expect(PROJECT_LIST_MESSAGES.forbidden_project).toContain("個人アカウント");
  });

  // 例外の中身は画面に出さない（`web/CLAUDE.md`）。畳んだ側にも置かない。
  // 置くと、`<details>` を開いた利用者に例外の文字列が届く。
  it("応答が返ってこなかったときは、例外の中身を画面に載せない", () => {
    const e = new TypeError("Failed to fetch http://127.0.0.1:8080/api/boards");

    const failure = describeFailure("保存できませんでした", e);

    expect(failure.detail).toBe("");
    expect(failure.message).not.toContain("Failed to fetch");
    expect(failure.message).toContain("保存できませんでした");
  });

  // 消さずに console へ回す。行き先が無くなると、通信の失敗はどこにも残らない。
  it("例外そのものは logger に渡す", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    const e = new TypeError("Failed to fetch");

    describeFailure("保存できませんでした", e);

    expect(spy).toHaveBeenCalledWith(expect.stringContaining("保存できませんでした"), e);
    spy.mockRestore();
  });
});

// code を持たない失敗も文言はこのモジュールが持つ。コンポーネントに書くと、
// 同じ言い回しがまた散る（`web/CLAUDE.md`）。
describe("code を持たない失敗", () => {
  it("シーンが読めないときは、そのあとどうなるかまで言う", () => {
    const failure = sceneUnreadableFailure();

    expect(failure.message).toContain("空のボードとして開きます");
    expect(failure.detail).toBe("");
  });

  it("取り込むファイルが読めないときは、キャンバスの状態と次の打ち手を言う", () => {
    const failure = sceneFileUnreadableFailure();

    expect(failure.message).toContain("キャンバスはそのままです");
    expect(failure.message).toContain("別の .excalidraw ファイルを選んでください");
    expect(failure.detail).toBe("");
  });

  it("キャンバスへの mermaid の貼り付けを止めたときは、止めたことと次の打ち手を言う", () => {
    const failure = canvasMermaidPasteFailure();

    expect(failure.message).toContain("キャンバスに直接貼れません");
    expect(failure.message).toContain("キャンバスはそのままです");
    expect(failure.message).toContain("テキストを入力している最中に貼り付けてください");
    expect(failure.detail).toBe("");
  });

  // 取り消せない作成の結果なので、サーバーの本文は畳んで残す（ADR 0009）。
  it("部分失敗は件数を前に、サーバーの本文を畳んだ側に置く", () => {
    expect(partialCreationFailure("2 件は作成済み", "etoki: github rejected")).toEqual({
      message: "途中で失敗しました（2 件は作成済み）",
      detail: "etoki: github rejected",
    });
  });

  // 契約上 SyncRun.error は任意。無くても detail は文字列でなければならない。
  it("本文が無ければ畳む側は空になる", () => {
    expect(partialCreationFailure("2 件は作成済み", undefined).detail).toBe("");
  });
});

describe("mermaidPasteFailure", () => {
  // どの理由でもキャンバスには触っていない。貼った人が「何か壊したか」を
  // 確かめに行かなくてよいように言い切る。
  it.each(["empty", "kind", "tooLarge", "syntax", "unsupported"] as const)(
    "キャンバスがそのままだと言う（%s）",
    (reason) => {
      expect(mermaidPasteFailure(reason, "x").message).toContain(
        "キャンバスはそのままです",
      );
    },
  );

  // 構文エラーだけは直す場所がパーサの位置つきの英文にしか無い（ADR 0062）。
  // 前に出すのは固定文で、本文は畳んだ側に置く。
  it("構文エラーのときだけパーサのメッセージを畳んで添える", () => {
    const syntax = mermaidPasteFailure("syntax", "Parse error on line 2");
    expect(syntax.detail).toBe("Parse error on line 2");
    expect(syntax.message).not.toContain("Parse error");

    for (const reason of ["empty", "kind", "tooLarge", "unsupported"] as const) {
      expect(
        mermaidPasteFailure(reason, "conversion returned a image element").detail,
      ).toBe("");
    }
  });

  // 図のドラフトの「種類を変えて」は LLM への頼み方の話。貼った人には打ち手が
  // 無いので、置ける種類を名指しする。
  it("種類で拒んだときは、置ける種類を名指しする", () => {
    const message = mermaidPasteFailure("kind", "").message;

    for (const kind of ["erDiagram", "sequenceDiagram", "flowchart"]) {
      expect(message).toContain(kind);
    }
    expect(message).not.toContain("種類を変えて");
  });
});

describe("ERROR_MESSAGES", () => {
  // 網羅は Record<ErrorCode, string> が tsc で見る。ここで見るのは中身のほう。
  it("空の文言を持たない", () => {
    for (const [code, message] of Object.entries(ERROR_MESSAGES)) {
      expect(message, code).not.toBe("");
    }
  });

  // 大きさで弾かれたときの打ち手は「貼った画像を減らす」で、入力の誤りとは
  // 違う。同じ文言に畳むと、413 に専用の code を持たせた意味が無くなる。
  it("大きさの拒否は入力の誤りと別の文言を持つ", () => {
    expect(ERROR_MESSAGES.scene_too_large).not.toBe(ERROR_MESSAGES.invalid_input);
    expect(ERROR_MESSAGES.scene_too_large).toContain("画像");
  });

  // 429 の 2 つ。片方は待つ長さの話、もう片方は「もう 1 つ走っている」話で、
  // 次にすることが違う（ADR 0044）。畳むと画面はどちらとも言えない。
  it("実行の上限の 2 つは別々の文言を持つ", () => {
    expect(ERROR_MESSAGES.rate_limited).not.toBe(ERROR_MESSAGES.concurrency_limited);
  });

  // 設定するものが違うので畳まない。畳むと「何を設定すればよいか」を言えない。
  it("未設定の 4 つは別々の文言を持つ", () => {
    const messages = [
      ERROR_MESSAGES.llm_not_configured,
      ERROR_MESSAGES.github_not_configured,
      ERROR_MESSAGES.auth_not_configured,
      ERROR_MESSAGES.sharing_not_configured,
      ERROR_MESSAGES.mcp_connections_not_configured,
    ];

    expect(new Set(messages).size).toBe(messages.length);
  });
});
