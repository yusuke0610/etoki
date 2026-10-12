/**
 * MCP のクライアントを見せるときの表示（ADR 0076）。
 *
 * 同意の画面と接続の一覧が同じ規則で出す。片方だけ変えると、許可したときに
 * 見た名乗りと、取り消すときに見る名乗りが食い違う。
 */

/** 名前を名乗らなかったクライアントの表示。 */
export const UNNAMED_CLIENT = "名前の無いクライアント";

/** 表示する名前。空ならクライアントが名乗らなかったことを出す。 */
export function clientDisplayName(name: string): string {
  return name.trim() === "" ? UNNAMED_CLIENT : name;
}

/**
 * URL で名乗ったクライアント（Client ID Metadata Document）の出どころ。
 *
 * **名前と違って、これは etoki が取りに行った先なので確かめてある。** URL で
 * なければ null（動的登録の client_id は乱数で、出どころを表さない）。
 */
export function clientOrigin(clientId: string): string | null {
  if (!clientId.startsWith("https://")) return null;
  try {
    return new URL(clientId).host;
  } catch {
    return null;
  }
}
