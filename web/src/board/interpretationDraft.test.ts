import { describe, expect, it } from "vitest";

import type { Interpretation, InterpretedItem, SyncItem } from "../api/types";
import {
  blockingReasons,
  buildInterpretation,
  canResend,
  createDraft,
  type Draft,
  leftBehindItemIds,
  markCreated,
  orphanedLocalIds,
  setBody,
  setKind,
  setTitle,
  setUpdatesPrevious,
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

describe("leftBehindItemIds", () => {
  const previous: SyncItem[] = [
    {
      itemId: "PVTI_a",
      kind: "epic",
      title: "epic",
      body: "",
      localId: "e1",
      action: "created",
      confirmed: true,
    },
    {
      itemId: "PVTI_b",
      kind: "issue",
      title: "issue",
      body: "",
      localId: "i1",
      action: "created",
      confirmed: true,
    },
  ];

  function draftWith(items: InterpretedItem[]) {
    return createDraft({ summary: "s", contentHash: "h", items });
  }

  it("どの項目からも指されていないものが取り残される", () => {
    const draft = draftWith([
      { localId: "n1", kind: "issue", title: "t", body: "", previousItemId: "PVTI_a" },
    ]);

    expect([...leftBehindItemIds(draft, previous)]).toEqual(["PVTI_b"]);
  });

  it("全部が対応づいていれば取り残しは無い", () => {
    const draft = draftWith([
      { localId: "n1", kind: "epic", title: "t", body: "", previousItemId: "PVTI_a" },
      { localId: "n2", kind: "issue", title: "t", body: "", previousItemId: "PVTI_b" },
    ]);

    expect(leftBehindItemIds(draft, previous).size).toBe(0);
  });

  // 外した項目は作られないので、その更新先は取り残しに戻る。**選択を外した
  // ことで何が起きるかを、押す前に見せる**のがこの表示の目的（中核思想 3）。
  it("選択を外すと、その更新先は取り残しに戻る", () => {
    let draft = draftWith([
      { localId: "n1", kind: "issue", title: "t", body: "", previousItemId: "PVTI_a" },
      { localId: "n2", kind: "issue", title: "t", body: "", previousItemId: "PVTI_b" },
    ]);
    expect(leftBehindItemIds(draft, previous).size).toBe(0);

    draft = toggleItem(draft, "n1");
    expect([...leftBehindItemIds(draft, previous)]).toEqual(["PVTI_a"]);
  });

  it("前回ぶんが無ければ取り残しも無い", () => {
    const draft = draftWith([{ localId: "n1", kind: "issue", title: "t", body: "" }]);

    expect(leftBehindItemIds(draft, []).size).toBe(0);
  });

  // 新しく作るに倒した項目も、そこへは書かない。選択を外したときと同じ扱いに
  // する。片方だけ取り残しに出さないと、押す前に見せている数が実際と食い違う。
  it("新しく作るに倒すと、その更新先は取り残しに戻る", () => {
    let draft = draftWith([
      { localId: "n1", kind: "issue", title: "t", body: "", previousItemId: "PVTI_a" },
      { localId: "n2", kind: "issue", title: "t", body: "", previousItemId: "PVTI_b" },
    ]);
    expect(leftBehindItemIds(draft, previous).size).toBe(0);

    draft = setUpdatesPrevious(draft, "n1", false);
    expect([...leftBehindItemIds(draft, previous)]).toEqual(["PVTI_a"]);
  });
});

// 対応づけを解釈させるのは LLM でも、決めるのは開発者（ADR 0026）。指す先が
// GitHub から消えていると、更新のままでは作成が必ず失敗する。
describe("setUpdatesPrevious", () => {
  function draftWith(previousItemId?: string): Draft {
    return createDraft({
      summary: "s",
      contentHash: "h",
      items: [{ localId: "n1", kind: "issue", title: "t", body: "", previousItemId }],
    });
  }

  it("既定は LLM の答えのまま", () => {
    expect(draftWith("PVTI_a").items[0]?.updatesPrevious).toBe(true);
    expect(draftWith().items[0]?.updatesPrevious).toBe(false);
  });

  it("新しく作るに倒すと previousItemId を送らない", () => {
    const draft = setUpdatesPrevious(draftWith("PVTI_a"), "n1", false);

    expect(buildInterpretation(draft).items[0]?.previousItemId).toBeUndefined();
  });

  // previousItemId を消して表すと戻せない。LLM が何と答えたのかも読めなくなる。
  it("倒しても LLM の答えは残っていて、戻せる", () => {
    let draft = setUpdatesPrevious(draftWith("PVTI_a"), "n1", false);
    expect(draft.items[0]?.item.previousItemId).toBe("PVTI_a");

    draft = setUpdatesPrevious(draft, "n1", true);
    expect(buildInterpretation(draft).items[0]?.previousItemId).toBe("PVTI_a");
  });

  // LLM が「新しく作る」と答えた項目には指す先が無い。倒せても送るものは無い。
  it("指す先の無い項目を更新に倒しても、送るものは増えない", () => {
    const draft = setUpdatesPrevious(draftWith(), "n1", true);

    expect(buildInterpretation(draft).items[0]?.previousItemId).toBeUndefined();
  });
});

// 対応づけは開発者が確かめたまま送り返す（ADR 0026）。
describe("buildInterpretation と previousItemId", () => {
  it("選んだ項目の previousItemId が載る", () => {
    const draft = createDraft({
      summary: "s",
      contentHash: "h",
      items: [
        { localId: "n1", kind: "issue", title: "t", body: "", previousItemId: "PVTI_a" },
        { localId: "n2", kind: "issue", title: "t", body: "" },
      ],
    });

    const built = buildInterpretation(draft);
    expect(built.items[0]?.previousItemId).toBe("PVTI_a");
    expect(built.items[1]?.previousItemId).toBeUndefined();
  });
});

/** 作成の応答に載る 1 件。 */
function created(localId: string, itemId: string): SyncItem {
  return {
    itemId,
    kind: localId.startsWith("e") ? "epic" : "issue",
    title: localId,
    body: "",
    localId,
    action: "created",
    confirmed: true,
  };
}

// 作れた項目を、同じ下書きから新規に作らせない（ADR 0052、#139）。
// draft issue は削除できないので、押し直しの重複は取り消せない。
describe("markCreated", () => {
  it("作れた項目は選択が外れ、残りは選ばれたまま", () => {
    const draft = markCreated(createDraft(sample()), [
      created("e1", "PVTI_e1"),
      created("i1", "PVTI_i1"),
    ]);

    expect(selectedIds(draft)).toEqual(["i2", "i3"]);
  });

  it("全部作れたら、作るものが 1 件も選ばれていない理由で止まる", () => {
    const draft = markCreated(
      createDraft(sample()),
      ["e1", "i1", "i2", "i3"].map((id) => created(id, `PVTI_${id}`)),
    );

    expect(blockingReasons(draft, "")).toContain("作るものが 1 件も選ばれていません。");
  });

  // **選び直しても新規には戻らない。** 作った ID を更新先として送るので、
  // 押し直しは書き換えになり、重複は増えない。
  it("選び直した作成済みの項目は、作った ID の更新として送る", () => {
    let draft = markCreated(createDraft(sample()), [created("e1", "PVTI_e1")]);
    draft = toggleItem(draft, "e1");

    expect(sentItem(draft, "e1").previousItemId).toBe("PVTI_e1");
  });

  it("作成済みの項目は「新しく作る」に倒せない", () => {
    let draft = markCreated(createDraft(sample()), [created("e1", "PVTI_e1")]);
    draft = toggleItem(draft, "e1");
    draft = setUpdatesPrevious(draft, "e1", false);

    expect(sentItem(draft, "e1").previousItemId).toBe("PVTI_e1");
  });

  // LLM が対応づけた先があっても、作った ID を優先する。LLM の答えは消さない。
  it("LLM の答えは残したまま、作った ID を更新先にする", () => {
    const result = sample();
    result.items[0] = { ...epic("e1"), previousItemId: "PVTI_old" };
    // LLM の対応づけと、実際に作った ID を別の値にする。同じ値だと、どちらを
    // 送っているのかをテストが見分けられない。
    let draft = markCreated(createDraft(result), [created("e1", "PVTI_new")]);
    draft = toggleItem(draft, "e1");

    expect(draft.items[0]?.item.previousItemId).toBe("PVTI_old");
    expect(sentItem(draft, "e1").previousItemId).toBe("PVTI_new");
  });

  // 部分失敗のいちばん多い形。epic は先に作られるので、epic だけ作れて子が
  // 残る。子だけ送ると親に紐づけられないので、epic を選び直せば
  // 「epic の更新 + 子の作成」で親子がつながる。
  it("部分失敗のあと、epic を選び直すと子は親つきで作られる", () => {
    let draft = markCreated(createDraft(sample()), [created("e1", "PVTI_e1")]);

    // epic が外れたままでは、子は親なしになることを見せる。
    expect(orphanedLocalIds(draft)).toEqual(new Set(["i1", "i2"]));

    draft = toggleItem(draft, "e1");

    expect(orphanedLocalIds(draft).size).toBe(0);
    expect(sentItem(draft, "e1").previousItemId).toBe("PVTI_e1");
    expect(sentItem(draft, "i1").previousItemId).toBeUndefined();
    expect(sentItem(draft, "i1").parentLocalId).toBe("e1");
  });

  it("下書きに無い localId は無視する", () => {
    const draft = markCreated(createDraft(sample()), [created("x9", "PVTI_x9")]);

    expect(selectedIds(draft)).toEqual(["e1", "i1", "i2", "i3"]);
  });

  // 作ったばかりの item は畳み込みに入ってくる。どの項目からも指されていない
  // ので、数えると「今回の作成では書き換わらない」一覧に自分が作ったものが並ぶ。
  it("作った item は選択を外していても取り残しに数えない", () => {
    const draft = markCreated(createDraft(sample()), [created("e1", "PVTI_e1")]);
    const previous = [created("e1", "PVTI_e1"), created("zz", "PVTI_other")];

    expect(leftBehindItemIds(draft, previous)).toEqual(new Set(["PVTI_other"]));
  });
});

/** 応答を失った作成 1 件。**item ID は分からない**（ADR 0056）。 */
function lostCreate(localId: string): SyncItem {
  return { ...created(localId, ""), confirmed: false };
}

/** 届いたか分からない更新 1 件。相手の ID は分かっている。 */
function lostUpdate(localId: string, itemId: string): SyncItem {
  return { ...created(localId, itemId), action: "updated", confirmed: false };
}

// 届いたか分からない書き込みを、押し直しの対象から外す（ADR 0056、#170）。
//
// **受理されていたかどうかを etoki は知らない。** もう一度送って重複するより、
// 開発者に GitHub を見てもらうほうを選ぶ（中核思想 3）。
describe("markCreated（届いたか分からない項目）", () => {
  it("応答を失った作成は選択が外れ、選び直せない", () => {
    const draft = markCreated(createDraft(sample()), [lostCreate("e1")]);
    const e1 = draft.items.find((d) => d.item.localId === "e1");

    expect(e1?.selected).toBe(false);
    expect(e1?.unconfirmed).toBe(true);
    // **`createdItemId` を空文字で埋めない。** 埋めると targetItemIdOf が
    // 落として新規作成に戻り、押し直しで重複する道が開く。
    expect(e1?.createdItemId).toBeUndefined();
    expect(canResend(e1!)).toBe(false);
  });

  it("選び直そうとしても選択は変わらない", () => {
    const draft = markCreated(createDraft(sample()), [lostCreate("e1")]);

    expect(selectedIds(toggleItem(draft, "e1"))).not.toContain("e1");
  });

  it("送り先が分からない項目は、送信対象にも入らない", () => {
    const draft = markCreated(createDraft(sample()), [lostCreate("e1")]);

    expect(sentIds(draft)).not.toContain("e1");
  });

  it("届いたか分からない更新は、相手の ID が分かるので選び直せる", () => {
    const draft = markCreated(createDraft(sample()), [lostUpdate("e1", "PVTI_e1")]);
    const e1 = draft.items.find((d) => d.item.localId === "e1");

    expect(e1?.selected).toBe(false);
    expect(e1?.unconfirmed).toBe(true);
    expect(e1?.createdItemId).toBe("PVTI_e1");
    expect(canResend(e1!)).toBe(true);

    // 選び直すと、同じ item への書き直しとして送る。重複は作らない。
    const again = toggleItem(draft, "e1");
    expect(sentItem(again, "e1").previousItemId).toBe("PVTI_e1");
  });

  it("確定した項目は、これまでどおり作成済みとして扱う", () => {
    const draft = markCreated(createDraft(sample()), [created("e1", "PVTI_e1")]);
    const e1 = draft.items.find((d) => d.item.localId === "e1");

    expect(e1?.unconfirmed).toBeFalsy();
    expect(canResend(e1!)).toBe(true);
  });
});
