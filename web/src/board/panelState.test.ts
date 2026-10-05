import { describe, expect, it } from "vitest";

import type { AnnotationStatus } from "../api/types";
import { railBadgesOf, readPanelCollapsed, writePanelCollapsed } from "./panelState";

function memoryStorage(initial: Record<string, string> = {}): Storage {
  const data = new Map(Object.entries(initial));
  return {
    get length() {
      return data.size;
    },
    clear: () => data.clear(),
    getItem: (k) => data.get(k) ?? null,
    key: (i) => [...data.keys()][i] ?? null,
    removeItem: (k) => void data.delete(k),
    setItem: (k, v) => void data.set(k, v),
  };
}

describe("readPanelCollapsed / writePanelCollapsed", () => {
  it("畳んだことと開いたことを読み戻せる", () => {
    const storage = memoryStorage();

    writePanelCollapsed(true, storage);
    expect(readPanelCollapsed(storage)).toBe(true);

    writePanelCollapsed(false, storage);
    expect(readPanelCollapsed(storage)).toBe(false);
  });

  // 覚えていないときに畳んだまま始めると、初めての人は状態を探すことになる。
  it("覚えていなければ開いた状態で始める", () => {
    expect(readPanelCollapsed(memoryStorage())).toBe(false);
    expect(readPanelCollapsed(undefined)).toBe(false);
  });

  // 別の版が別の形で書いたときも、畳んだと読み違えない。
  it("知らない値は開いた状態として読む", () => {
    expect(
      readPanelCollapsed(memoryStorage({ "etoki.sidePanel.collapsed": "true" })),
    ).toBe(false);
  });

  // サイトデータを遮断した環境では、読み書きそのものが投げる。開閉は好みで
  // しかないので、そのせいで画面を落とさない。
  it("保存先が使えなくても投げず、開いた状態で始める", () => {
    const broken = {
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("denied");
      },
    } as unknown as Storage;

    expect(readPanelCollapsed(broken)).toBe(false);
    expect(() => writePanelCollapsed(true, broken)).not.toThrow();
  });
});

describe("railBadgesOf", () => {
  const annotation = (state: AnnotationStatus["state"]): AnnotationStatus =>
    ({ id: state, name: state, granularity: "", state }) as AnnotationStatus;

  // 件数は状態ごとに変えて、取り違えたら別の欄がずれるようにする。
  it("未作成と変更ありを数え、作成済みは数えない", () => {
    const badges = railBadgesOf([
      annotation("uncreated"),
      annotation("changed"),
      annotation("changed"),
      annotation("created"),
      annotation("created"),
      annotation("created"),
    ]);

    expect(badges).toEqual([
      { label: "未作成", state: "uncreated", count: 1 },
      { label: "変更あり", state: "changed", count: 2 },
    ]);
  });

  // 境界の外で数えるので、投げると画面ごと落ちる（ADR 0027）。
  it("壊れた一覧でも投げず、壊れた要素は数えない", () => {
    const broken = [null, annotation("changed")] as unknown as AnnotationStatus[];
    expect(railBadgesOf(broken)).toEqual([
      { label: "未作成", state: "uncreated", count: 0 },
      { label: "変更あり", state: "changed", count: 1 },
    ]);
    expect(() => railBadgesOf(undefined as unknown as AnnotationStatus[])).not.toThrow();
  });
});
