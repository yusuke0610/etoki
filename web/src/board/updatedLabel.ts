const MINUTE = 60 * 1000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;

const relative = new Intl.RelativeTimeFormat("ja", { numeric: "auto" });

/**
 * ボードのカードに出す更新時刻（#200）。`更新: 2 時間前` のように、いまからの
 * 経過で書く。
 *
 * **単位は経過時間で選ぶ。** 1 分未満は「たった今」、1 時間未満は分、1 日未満は
 * 時間、7 日未満は日、5 週未満は週、1 年未満は月、それ以上は年。`numeric: "auto"`
 * なので、1 日前は「昨日」、1 週前は「先週」になる。
 *
 * **未来の時刻は「たった今」にする。** サーバーと手元の時計がずれると、保存した
 * 直後のボードが「1 分後」になる。ずれは利用者に関係が無いので見せない。
 *
 * **`now` は引数で受ける。** 画面を開いたまま時刻を進め直すことはしない（一覧を
 * 読み直したときに変わる）ので、呼ぶ側が描いた時点の時刻を渡す。
 */
export function updatedLabel(updatedAt: string, now: Date): string {
  const elapsed = now.getTime() - new Date(updatedAt).getTime();
  return `更新: ${elapsedLabel(elapsed)}`;
}

function elapsedLabel(elapsed: number): string {
  if (elapsed < MINUTE) return "たった今";
  if (elapsed < HOUR) return relative.format(-Math.floor(elapsed / MINUTE), "minute");
  if (elapsed < DAY) return relative.format(-Math.floor(elapsed / HOUR), "hour");
  if (elapsed < WEEK) return relative.format(-Math.floor(elapsed / DAY), "day");
  if (elapsed < 5 * WEEK) return relative.format(-Math.floor(elapsed / WEEK), "week");

  // 月と年は日数から数える。暦の月は長さが揃わないので、境目をテストで
  // 固定できる形にする。
  const days = Math.floor(elapsed / DAY);
  if (days < 365) return relative.format(-Math.floor(days / 30), "month");
  return relative.format(-Math.floor(days / 365), "year");
}
