import { describe, expect, it } from "vitest";

import { readStoredTheme, resolveTheme, storedChoice, writeStoredTheme } from "./theme";

describe("resolveTheme", () => {
  it("選んだテーマが無ければ OS の設定に従う", () => {
    expect(resolveTheme(null, false)).toBe("light");
    expect(resolveTheme(null, true)).toBe("dark");
  });

  it("選んだテーマは OS の設定より優先する", () => {
    expect(resolveTheme("dark", false)).toBe("dark");
    expect(resolveTheme("light", true)).toBe("light");
  });

  // 保存先はブラウザの共有領域なので、他の版や手作業で何が入っていてもおかしくない。
  it("テーマとして読めない値は、選んでいないのと同じに扱う", () => {
    expect(resolveTheme("", true)).toBe("dark");
    expect(resolveTheme("system", false)).toBe("light");
    expect(resolveTheme("DARK", false)).toBe("light");
  });
});

describe("storedChoice", () => {
  // OS と違うテーマを選んだときだけ覚える。
  it("OS と違うテーマを選んだら、それを覚える", () => {
    expect(storedChoice("dark", false)).toBe("dark");
    expect(storedChoice("light", true)).toBe("light");
  });

  // 覚えたままにすると、一度切り替えた人は二度と OS に従わなくなる。
  it("OS と同じテーマを選び直したら、覚えた選択を消して OS に従う側へ戻す", () => {
    expect(storedChoice("light", false)).toBeNull();
    expect(storedChoice("dark", true)).toBeNull();
  });
});

describe("readStoredTheme / writeStoredTheme", () => {
  function memoryStorage(): Storage {
    const data = new Map<string, string>();
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

  it("書いた選択を読み戻せ、null を書くと消える", () => {
    const storage = memoryStorage();

    writeStoredTheme("dark", storage);
    expect(readStoredTheme(storage)).toBe("dark");

    writeStoredTheme(null, storage);
    expect(readStoredTheme(storage)).toBeNull();
    expect(storage.length).toBe(0);
  });

  // プライベートウィンドウやサイトデータの遮断では、読み書きそのものが投げる。
  // テーマは見た目の好みでしかないので、そのせいで画面を落とさない。
  it("保存先が使えなくても投げない", () => {
    const broken = {
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("denied");
      },
      removeItem: () => {
        throw new Error("denied");
      },
    } as unknown as Storage;

    expect(readStoredTheme(broken)).toBeNull();
    expect(() => writeStoredTheme("dark", broken)).not.toThrow();
    expect(() => writeStoredTheme(null, broken)).not.toThrow();
  });
});
