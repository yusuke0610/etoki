/**
 * `localStorage`。使えなければ undefined。
 *
 * **アクセスそのものが投げる環境がある**（サイトデータを遮断した設定や、保存を
 * 禁じたサンドボックス）ので、触る前に包む。ここに置くのは「好み」を覚える
 * ためだけで、失っても困らないもの（テーマ・パネルの開閉）に限る。
 */
export function safeLocalStorage(): Storage | undefined {
  try {
    return window.localStorage;
  } catch {
    return undefined;
  }
}
