import { describe, expect, it } from "vitest";

import type { Interpretation, InterpretedItem } from "../api/types";
import {
  blockingReasons,
  buildInterpretation,
  createDraft,
  type Draft,
  orphanedLocalIds,
  setBody,
  setKind,
  setTitle,
  toggleItem,
} from "./interpretationDraft";

function epic(localId: string, title = localId): InterpretedItem {
  return { localId, kind: "epic", title, body: "" };
}

function issue(localId: string, parentLocalId?: string): InterpretedItem {
  return { localId, kind: "issue", title: localId, body: "", parentLocalId };
}

/** epic e1 に issue i1 / i2 がぶら下がり、i3 は単独。 */
function sample(): Interpretation {
  return {
    summary: "ログイン基盤の整理",
    contentHash: "sha256:x",
    items: [epic("e1"), issue("i1", "e1"), issue("i2", "e1"), issue("i3")],
  };
}

function selectedIds(draft: Draft): string[] {
  return draft.items.filter((d) => d.selected).map((d) => d.item.localId);
}

function sentIds(draft: Draft): string[] {
  return buildInterpretation(draft).items.map((it) => it.localId);
}

function sentItem(draft: Draft, localId: string): InterpretedItem {
  const item = buildInterpretation(draft).items.find((it) => it.localId === localId);
  if (!item) throw new Error(`${localId} が送信対象に無い`);
  return item;
}

describe("createDraft", () => {
  // いまの「押せば全部作られる」を初期状態として保つ。既定を未選択にすると、
  // 何も変えていない人に選び直しを強いる。
  it("既定は全件を作る", () => {
    expect(selectedIds(createDraft(sample()))).toEqual(["e1", "i1", "i2", "i3"]);
  });
});

describe("toggleItem", () => {
  it("issue を外すと、その 1 件だけが外れる", () => {
    const draft = toggleItem(createDraft(sample()), "i1");

    expect(selectedIds(draft)).toEqual(["e1", "i2", "i3"]);
  });

  // 親だけ消えて子が残ると、開発者が選んだつもりのない「親なしの issue」が
  // GitHub にできる。
  it("epic を外すと配下の issue も外れる", () => {
    const draft = toggleItem(createDraft(sample()), "e1");

    expect(selectedIds(draft)).toEqual(["i3"]);
  });

  // 外したものを勝手に戻すと、外した操作がどこまで効いているのか分からなくなる。
  it("epic を戻しても配下は戻らない", () => {
    const draft = toggleItem(toggleItem(createDraft(sample()), "e1"), "e1");

    expect(selectedIds(draft)).toEqual(["e1", "i3"]);
  });

  it("外れた issue は 1 件ずつ戻せる", () => {
    const draft = toggleItem(toggleItem(createDraft(sample()), "e1"), "i1");

    expect(selectedIds(draft)).toEqual(["i1", "i3"]);
  });

  it("知らない localId は何も変えない", () => {
    const draft = createDraft(sample());

    expect(toggleItem(draft, "none")).toEqual(draft);
  });
});

describe("buildInterpretation", () => {
  it("summary と contentHash をそのまま渡す", () => {
    const built = buildInterpretation(createDraft(sample()));

    expect(built.summary).toBe("ログイン基盤の整理");
    expect(built.contentHash).toBe("sha256:x");
  });

  it("選んだものだけを送る", () => {
    const draft = toggleItem(createDraft(sample()), "i2");

    expect(sentIds(draft)).toEqual(["e1", "i1", "i3"]);
  });

  // 残すとサーバーの Validate が「parentLocalId に対応する localId がありません」
  // で弾く。画面から 400 になる組み合わせを作らせない。
  it("親を外して戻した issue は親なしで送る", () => {
    const draft = toggleItem(toggleItem(createDraft(sample()), "e1"), "i1");

    expect(sentIds(draft)).toEqual(["i1", "i3"]);
    expect(sentItem(draft, "i1").parentLocalId).toBeUndefined();
  });

  it("親が選ばれていれば parentLocalId を残す", () => {
    expect(sentItem(createDraft(sample()), "i1").parentLocalId).toBe("e1");
  });

  // epic は親を持てない（ADR 0006）。送るとその項目自体が弾かれる。
  it("epic に変えた項目は親を送らない", () => {
    const draft = setKind(createDraft(sample()), "i1", "epic");

    expect(sentItem(draft, "i1").parentLocalId).toBeUndefined();
  });

  // 階層は epic ← issue の 1 本だけ。issue を親にはできない。
  it("親を issue に変えると、その子は親なしで送る", () => {
    const draft = setKind(createDraft(sample()), "e1", "issue");

    expect(sentItem(draft, "i1").parentLocalId).toBeUndefined();
    expect(sentItem(draft, "i2").parentLocalId).toBeUndefined();
  });

  // 送るときに落とすだけで draft からは消さないので、戻せば親も戻る。
  it("kind を戻すと親も戻る", () => {
    const draft = setKind(setKind(createDraft(sample()), "e1", "issue"), "e1", "epic");

    expect(sentItem(draft, "i1").parentLocalId).toBe("e1");
  });

  it("編集した title と body を送る", () => {
    const draft = setBody(
      setTitle(createDraft(sample()), "i1", "OAuth の設定画面"),
      "i1",
      "認可コードフローで受ける",
    );

    expect(sentItem(draft, "i1").title).toBe("OAuth の設定画面");
    expect(sentItem(draft, "i1").body).toBe("認可コードフローで受ける");
    // 直したのは 1 件だけ。他の項目まで書き換わっていない。
    expect(sentItem(draft, "i2").title).toBe("i2");
  });
});

describe("orphanedLocalIds", () => {
  it("親が選ばれていれば載らない", () => {
    expect(orphanedLocalIds(createDraft(sample())).size).toBe(0);
  });

  // もともと親を持たない issue は「親を失った」わけではない。出すと、
  // 開発者の操作と無関係な警告が常時並ぶ。
  it("もともと親のない issue は載らない", () => {
    const draft = createDraft({ ...sample(), items: [issue("i3")] });

    expect(orphanedLocalIds(draft).size).toBe(0);
  });

  it("親を外して戻した issue が載る", () => {
    const draft = toggleItem(toggleItem(createDraft(sample()), "e1"), "i1");

    expect([...orphanedLocalIds(draft)]).toEqual(["i1"]);
  });

  it("親を issue に変えると配下が載る", () => {
    const draft = setKind(createDraft(sample()), "e1", "issue");

    expect([...orphanedLocalIds(draft)]).toEqual(["i1", "i2"]);
  });

  it("選ばれていない issue は載らない", () => {
    const draft = toggleItem(createDraft(sample()), "e1");

    expect(orphanedLocalIds(draft).size).toBe(0);
  });

  // epic に変えた項目は親を持てないのであって、親を失ったのではない。
  it("epic に変えた項目は載らない", () => {
    const draft = setKind(createDraft(sample()), "i1", "epic");

    expect(orphanedLocalIds(draft).size).toBe(0);
  });
});

describe("blockingReasons", () => {
  it("既定では止めない", () => {
    expect(blockingReasons(createDraft(sample()), "")).toEqual([]);
  });

  it("1 件も選ばれていなければ止める", () => {
    const draft: Draft = { ...createDraft(sample()), items: [] };

    expect(blockingReasons(draft, "")).toHaveLength(1);
  });

  // サーバーは空白のみの title も弾く。
  it("選んだ項目の title が空白だけなら止める", () => {
    const draft = setTitle(createDraft(sample()), "i1", "  ");

    expect(blockingReasons(draft, "")).toHaveLength(1);
  });

  it("外した項目の title が空でも止めない", () => {
    const draft = toggleItem(setTitle(createDraft(sample()), "i1", ""), "i1");

    expect(blockingReasons(draft, "")).toEqual([]);
  });

  it("粒度 epic で epic を全部外すと止める", () => {
    const draft = toggleItem(createDraft(sample()), "e1");

    expect(blockingReasons(draft, "epic")).toHaveLength(1);
    expect(blockingReasons(draft, "")).toEqual([]);
  });

  it("粒度 epic でも epic が残っていれば止めない", () => {
    expect(blockingReasons(createDraft(sample()), "epic")).toEqual([]);
  });
});
