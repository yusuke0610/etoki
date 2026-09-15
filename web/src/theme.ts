import { useCallback, useEffect, useState } from "react";

/**
 * 画面の配色。etoki のパネルと Excalidraw のキャンバスで 1 つの値を共有する
 * （ADR 0049）。
 */
export type Theme = "light" | "dark";

const STORAGE_KEY = "etoki.theme";
const DARK_QUERY = "(prefers-color-scheme: dark)";

/**
 * いま使うテーマ。選んだものがあればそれ、無ければ OS の設定。
 *
 * 保存先はブラウザの共有領域なので、テーマとして読めない値は「選んでいない」と
 * 同じに扱う。
 */
export function resolveTheme(stored: string | null, prefersDark: boolean): Theme {
  if (stored === "light" || stored === "dark") return stored;
  return prefersDark ? "dark" : "light";
}

/**
 * テーマを選んだときに覚えておく値。null は「覚えず OS に従う」。
 *
 * **OS と同じテーマなら覚えない。** 選んだものを常に覚えると、一度切り替えた
 * 人は二度と OS の設定に従わなくなり、戻す口も無い。
 */
export function storedChoice(chosen: Theme, prefersDark: boolean): Theme | null {
  return chosen === resolveTheme(null, prefersDark) ? null : chosen;
}

/**
 * 覚えた選択を読む。
 *
 * **読み書きの失敗は握りつぶす。** プライベートウィンドウやサイトデータの遮断で
 * は保存先そのものが投げる。テーマは見た目の好みでしかなく、そのせいで画面を
 * 落とす理由が無い。
 */
export function readStoredTheme(
  storage: Storage | undefined = safeLocalStorage(),
): string | null {
  try {
    return storage?.getItem(STORAGE_KEY) ?? null;
  } catch {
    return null;
  }
}

/** 覚えた選択を書く。null なら消す。失敗は `readStoredTheme` と同じく握りつぶす。 */
export function writeStoredTheme(
  theme: Theme | null,
  storage: Storage | undefined = safeLocalStorage(),
): void {
  try {
    if (theme === null) storage?.removeItem(STORAGE_KEY);
    else storage?.setItem(STORAGE_KEY, theme);
  } catch {
    // 覚えられないだけで、いまの画面のテーマは切り替わる。
  }
}

/** `localStorage` へのアクセス自体が投げる環境があるので、触る前に包む。 */
function safeLocalStorage(): Storage | undefined {
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}

function prefersDark(): boolean {
  return window.matchMedia?.(DARK_QUERY).matches ?? false;
}

/**
 * ルート要素にテーマを載せる。CSS は `:root[data-theme="dark"]` で配色を
 * 差し替える。
 */
export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
}

/** 起動した瞬間のテーマ。React が描く前に `applyTheme` へ渡して、ちらつきを防ぐ。 */
export function initialTheme(): Theme {
  return resolveTheme(readStoredTheme(), prefersDark());
}

/**
 * テーマの状態。**持つのは App の 1 箇所だけ**（ADR 0049）。
 *
 * 選んでいないあいだは OS の設定の変化にも付いていく。
 */
export function useTheme(): [Theme, (theme: Theme) => void] {
  const [stored, setStored] = useState(() => readStoredTheme());
  const [dark, setDark] = useState(prefersDark);

  useEffect(() => {
    const query = window.matchMedia?.(DARK_QUERY);
    if (!query) return;

    const onChange = (e: MediaQueryListEvent) => setDark(e.matches);
    query.addEventListener("change", onChange);
    return () => query.removeEventListener("change", onChange);
  }, []);

  const theme = resolveTheme(stored, dark);

  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  const choose = useCallback(
    (next: Theme) => {
      const choice = storedChoice(next, dark);
      writeStoredTheme(choice);
      setStored(choice);
    },
    [dark],
  );

  return [theme, choose];
}
