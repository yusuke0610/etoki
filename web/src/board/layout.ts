/**
 * ボードの画面の置き方（#199）。
 *
 * - `side`: 右のパネルをキャンバスの横に並べる
 * - `overlay`: 右のパネルをキャンバスに重ねる（キャンバスを押し縮めない）
 * - `phone`: パネルは既定で畳み、開くと全面。右上と下の帯の中身はキャンバスの
 *   上の etoki の帯に移す
 */
export type BoardLayout = "side" | "overlay" | "phone";

/*
 * **Excalidraw 0.18.1 の `App.isMobileBreakpoint` と、その定数の写し。** 定数は
 * `MQ_MAX_WIDTH_PORTRAIT` / `MQ_MAX_WIDTH_LANDSCAPE` / `MQ_MAX_HEIGHT_LANDSCAPE`
 * で、どれも公開されていない。
 *
 * **写すのは、切り替える境目を Excalidraw と同じにするため。** 別の境目を持つと、
 * Excalidraw だけがモバイル用 UI に切り替わった幅ができる。モバイル用 UI では
 * `Footer` が描かれないので、下の帯の中身（ロール・作成先・大きさ）が消える。
 *
 * ライブラリを上げて定数が変わると、ここだけが古くなる。気づくのは
 * `web/e2e/layout.spec.ts` の「Excalidraw と判定が揃っている」で、実物の
 * `excalidraw--mobile` を見ている。
 */
const MQ_MAX_WIDTH_PORTRAIT = 730;
const MQ_MAX_WIDTH_LANDSCAPE = 1000;
const MQ_MAX_HEIGHT_LANDSCAPE = 500;

/** Excalidraw がその大きさの枠をモバイル用 UI で描くか。 */
export function isMobileBreakpoint(width: number, height: number): boolean {
  return (
    width < MQ_MAX_WIDTH_PORTRAIT ||
    (height < MQ_MAX_HEIGHT_LANDSCAPE && width < MQ_MAX_WIDTH_LANDSCAPE)
  );
}

/**
 * 置き方を決める。`width` と `height` は**キャンバスが使える領域**の大きさで、
 * 上に出る注意の帯（衝突・上限超え・削除の確認）を除いたもの（`useBoardLayout`）。
 *
 * **並べる条件は「並べてもキャンバスがモバイル用にならないこと」。** パネルの
 * 幅のぶん押し縮めた枠で Excalidraw の式を通す。並べられなければ重ね、
 * キャンバスの枠を領域のまま残す。そうすると Excalidraw が見る大きさと
 * `phone` の判定が見る大きさが同じになり、両者が揃う。
 *
 * `panelWidth` が読めない（null）ときは並べない。重ねるならキャンバスを
 * 押し縮めないので、どの幅でも Excalidraw と食い違わない。
 */
export function boardLayout(
  width: number,
  height: number,
  panelWidth: number | null,
): BoardLayout {
  if (isMobileBreakpoint(width, height)) return "phone";
  if (panelWidth !== null && !isMobileBreakpoint(width - panelWidth, height)) {
    return "side";
  }
  return "overlay";
}
