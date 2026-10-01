import { describe, expect, it } from "vitest";

import type { AnnotationStatus } from "../api/types";
import {
  annotationLabel,
  annotationLabels,
  annotationSummary,
  frameLabel,
  ITEM_KIND_LABEL,
  itemKinds,
} from "./annotationLabel";

describe("itemKinds", () => {
  // 作るものの種別の選択肢は表から並べる。手書きの <option> にすると、契約に
  // 種別を足したときに tsc が気づかない（#156）。
  it("表の種別を 1 つずつ、表の並びで返す", () => {
    expect(itemKinds()).toEqual(["epic", "issue"]);
    expect(itemKinds()).toEqual(Object.keys(ITEM_KIND_LABEL));
  });
});

function status(id: string, name: string): AnnotationStatus {
  return { id, name, granularity: "", state: "uncreated" };
}

describe("annotationLabel", () => {
  it("名前があればそのまま使う", () => {
    expect(annotationLabel("ログイン", 0)).toBe("ログイン");
  });

  it("名前が無ければ一覧上の位置で採番する", () => {
    expect(annotationLabel("", 1)).toBe("注釈 2");
  });

  // Excalidraw は空白だけの名前も受け取る。見た目が「名前なし」と同じものを
  // 名前として扱うと、見出しが空欄のカードが並ぶ。
  it("空白だけの名前は名前なしとして扱う", () => {
    expect(annotationLabel("   ", 2)).toBe("注釈 3");
  });
});

describe("annotationLabels", () => {
  // 採番は名前の有無ではなく一覧上の位置で振る。名前ありを飛ばして数えると、
  // 「注釈 2」が 2 番目のカードを指さなくなる。
  it("名前ありを飛ばさずに位置で採番する", () => {
    const got = annotationLabels([
      status("a", "ログイン"),
      status("b", ""),
      status("c", ""),
    ]);

    expect(got.get("a")).toBe("ログイン");
    expect(got.get("b")).toBe("注釈 2");
    expect(got.get("c")).toBe("注釈 3");
  });

  it("空の一覧では何も引けない", () => {
    expect(annotationLabels([]).size).toBe(0);
  });
});

describe("frameLabel", () => {
  it("名前があればそのまま使う", () => {
    expect(frameLabel("ただの枠")).toBe("ただの枠");
  });

  // まだ注釈でない frame は一覧に並んでいないので番号を持たない。
  // 番号を振ると、状態欄の「注釈 n」と食い違う番号が同じ画面に 2 種類出る。
  it("名前が無ければ番号を振らない", () => {
    expect(frameLabel("")).toBe("名前のないフレーム");
  });
});

describe("annotationSummary", () => {
  const base: AnnotationStatus = {
    id: "a1",
    name: "ログイン",
    granularity: "",
    state: "uncreated",
  };

  it("選んでいない粒度と種別は「未指定」と書く", () => {
    expect(annotationSummary(base)).toBe("粒度 未指定 · 種別 未指定");
  });

  it("選んだ粒度と種別は表示名で書く", () => {
    expect(annotationSummary({ ...base, granularity: "epic", kind: "sequence" })).toBe(
      "粒度 epic · 種別 シーケンス図",
    );
  });

  // 0 件を並べると、未作成のカードがどれも同じ長さの行で埋まる。
  it("GitHub の件数は 1 件以上のときだけ添える", () => {
    const item = {
      itemId: "PVTI_1",
      kind: "issue",
      title: "t",
      body: "",
      localId: "i1",
      action: "created",
      confirmed: true,
    } as const;
    expect(annotationSummary({ ...base, items: [] })).toBe("粒度 未指定 · 種別 未指定");
    expect(
      annotationSummary({ ...base, items: [item, { ...item, itemId: "PVTI_2" }] }),
    ).toBe("粒度 未指定 · 種別 未指定 · GitHub に 2 件");
  });
});
