/**
 * 保存に送るシーンの大きさ。
 *
 * **上限は持たない。** 保存できるかどうかを決めるのはサーバーだけで、両方に
 * 判定を置くと、片方だけ変えたときに「フロントは通すがサーバーが弾く」が
 * どちらの言い分か分からなくなる（ADR 0018 と同じ理由）。ここが出すのは
 * 判定ではなく、いまの大きさという状態だけ（中核思想 3）。
 *
 * 上限との比（「8 MiB 中 6.2 MiB」）を出さないのもそのため。比を出すには
 * 上限をフロントが知る必要があり、それは判定を持たせるのと同じことになる。
 */

/**
 * 保存に送る文字列のバイト数。
 *
 * `String.length` ではなく UTF-8 のバイト数で数える。ボードの中身は日本語が
 * 大半で、文字数で数えると実際に送る量の 1/3 になる。サーバーが見るのも
 * バイト数（`len(scene)`）なので、そちらに揃える。
 */
export function sceneBytes(scene: string): number {
  return new TextEncoder().encode(scene).length;
}

/**
 * バイト数を読める形にする。
 *
 * 単位は 1024 進で、KiB / MiB と書く。ここだけ 1000 進の KB / MB にすると、
 * サーバー側の上限（ADR 0038 の 8 MiB）と数え方が違うものを同じ字面で
 * 並べることになる。
 */
export function formatSceneSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;

  const kib = bytes / 1024;
  if (kib < 1024) return `${round(kib)} KiB`;

  return `${round(kib / 1024)} MiB`;
}

/** 小数第 1 位まで。末尾の .0 は落とす。 */
function round(value: number): string {
  return (Math.round(value * 10) / 10).toString();
}
