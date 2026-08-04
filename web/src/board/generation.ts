/**
 * キーごとのリクエスト世代。
 *
 * 解釈は往復に時間がかかるので、実行中に保存や再実行が起きうる。世代を進めて
 * おけば、古いリクエストの応答が返ってきたときにそれと分かり、捨てられる。
 *
 * 捨てないと、保存で消したはずの結果が後から復活する。それは保存前のシーンを
 * 解釈したものなので、いまの内容の解釈として読まれると誤りになる。
 */
export type Generations = {
  /** key の世代を 1 つ進め、新しい世代を返す。 */
  start(key: string): number;
  /** gen が key の最新の世代かどうかを返す。 */
  isCurrent(key: string, gen: number): boolean;
  /** すべての key の世代を進め、実行中のものをまとめて無効にする。 */
  invalidateAll(): void;
};

export function createGenerations(): Generations {
  // Record ではなく Map。添字アクセスの undefined を扱わずに済む。
  const current = new Map<string, number>();

  return {
    start(key) {
      const next = (current.get(key) ?? 0) + 1;
      current.set(key, next);
      return next;
    },

    isCurrent(key, gen) {
      return current.get(key) === gen;
    },

    invalidateAll() {
      for (const [key, gen] of current) {
        current.set(key, gen + 1);
      }
    },
  };
}
