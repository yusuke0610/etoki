import { convertToExcalidrawElements } from "@excalidraw/excalidraw";
import { parseMermaidToExcalidraw } from "@excalidraw/mermaid-to-excalidraw";

import type { SceneElement } from "./annotation";

/**
 * 変換器が返す骨格のうち、ここで見るところだけの形。
 *
 * ライブラリの `ExcalidrawElementSkeleton` をそのまま受けないのは、置くか
 * 拒むかの判断が `type` しか見ていないと読めるようにするため。
 */
export type ElementSkeleton = { type: string };

/**
 * `parseMermaidToExcalidraw` のうち、ここで使う部分だけの形。
 *
 * 差し替えられるようにしてあるのは、**拒む側の分岐を本物の変換器では踏ませ
 * られない**ため。mermaid は SVG の実寸を測って図を組み立てるので、jsdom では
 * 図の種類によって本物とは違う結果になる（下の「jsdom では確かめられない
 * こと」を見る）。
 */
export type MermaidParser = (definition: string) => Promise<{
  elements: readonly ElementSkeleton[];
}>;

/**
 * 変換に失敗した理由。**呼び出し側はこれで次の一手を決める。**
 *
 * - `syntax` — mermaid として読めなかった。生成し直せば直りうるので、
 *   同じ種類のまま投げ直す判断に使える（#59 の再送）。
 * - `unsupported` — 読めたが、置けない形で返ってきた。同じ種類で投げ直しても
 *   同じ結果になるので、**再送の対象にしない。**
 */
export type DraftFailure = "syntax" | "unsupported";

/**
 * 変換の結果。**例外ではなく値で返す。**
 *
 * 失敗するのは etoki の不具合ではなく LLM の出力が読めなかったときなので、
 * 呼び出し側が投げ直すかどうかを決められる必要がある。
 */
export type MermaidDraft =
  | { ok: true; elements: SceneElement[] }
  /**
   * `detail` は投げ直すときの手掛かりとログのためのもので、**画面には出さない。**
   * mermaid のパーサが出す英語の位置つきメッセージで、読むのは LLM と console
   * （`web/CLAUDE.md` の「例外の中身は画面に出さない」と同じ扱い）。
   */
  | { ok: false; reason: DraftFailure; detail: string };

/**
 * 置かずに拒む要素の種類。
 *
 * - `image` — mermaid が etoki の知らない記法（mindmap など）を渡されると、
 *   図を SVG に描いて**画像 1 枚**にして返す。置くと、手で直せない絵が
 *   ドラフトの顔をして残る。**直すところから始められることがこの機能の値打ち**
 *   （#58）なので、絵に見えて直せないものは置かない。データ URL がシーンに
 *   載るぶん、保存の上限（ADR 0038）にも近づく。
 * - `frame` — etoki は frame を自前生成しない。いまの変換器は frame を返さない
 *   が、返すようになったら**注釈にできる frame を etoki が配ったことになる**。
 *   注釈の frame は人がフレームツールで作る、という線がそこで黙って崩れるので、
 *   置かずに止める。
 */
const NOT_DRAWABLE = new Set(["image", "frame"]);

/**
 * mermaid を Excalidraw の要素に変換する。**キャンバスには反映しない。**
 *
 * 返すのは要素だけで、置くのは呼び出し側。ドラフトを見てから置くかどうかを
 * 人が決める（#58 の原則、中核思想 3）ので、変換と反映は分かれている必要が
 * ある。
 *
 * **座標は変換器が決めたまま返す。** 置き場所は `draftOrigin` と `moveDraft`
 * が別に決める。
 *
 * LLM を挟むので待ちは長い。**返ってきた結果をいまのボードに入れてよいかの
 * 照合は呼び出し側の責務**（`board/generation.ts`）。ここは状態を持たない。
 *
 * ## jsdom では確かめられないこと
 *
 * mermaid は一度 SVG を描き、その実寸を測って図を組み立てる。jsdom には
 * `getBBox` も実寸も無いため、**図の種類によっては本物と違う経路を通る**
 * （flowchart と sequence は要素に、それ以外は画像に落ちる）。ここが本当に
 * どうなるかはブラウザでしか分からないので、単体テストは「返ってきた形を
 * どう扱うか」だけを固定し、**種類ごとの結果は E2E（#61）に任せる。**
 */
export async function mermaidToElements(
  definition: string,
  parse: MermaidParser = parseMermaidToExcalidraw,
): Promise<MermaidDraft> {
  let skeletons: readonly ElementSkeleton[];
  try {
    ({ elements: skeletons } = await parse(definition));
  } catch (err) {
    return { ok: false, reason: "syntax", detail: messageOf(err) };
  }

  const refused = skeletons.find((el) => NOT_DRAWABLE.has(el.type));
  if (refused) {
    return {
      ok: false,
      reason: "unsupported",
      detail: `conversion returned a ${refused.type} element`,
    };
  }

  try {
    return {
      ok: true,
      // 骨格から要素への詰め替えはライブラリに任せる。ラベルの入れ子や矢印の
      // 結びつきを自前で組み立てると、壊れたシーンを作る側に回る。
      elements: convertToExcalidrawElements(
        skeletons as never[],
      ) as unknown as SceneElement[],
    };
  } catch (err) {
    // **詰め替えも投げる。** 骨格が変換器の求める形になっていないと
    // `convertToExcalidrawElements` が落ちる。ここを囲まないと、この関数だけが
    // 「例外ではなく値で返す」という約束を破り、呼び出し側（`placeDraft`）は
    // 失敗を見せないまま終わる。
    //
    // **投げ直さない。** mermaid は読めているので、直すべき構文が無い。同じ
    // 種類で頼み直しても同じ骨格が返るとしか言えず、課金だけが増える。
    return { ok: false, reason: "unsupported", detail: messageOf(err) };
  }
}

/** 例外から投げ直しに渡せる文字列を取り出す。 */
function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * ドラフトと既存の絵のあいだに空ける間隔（シーン座標）。
 *
 * 触れそうなほど近いと、隣り合っているのか続きなのかが読めない。
 */
export const DRAFT_GAP = 120;

/** シーン座標での位置。 */
export type Point = { x: number; y: number };

/**
 * ドラフトを置く左上のシーン座標を決める。
 *
 * **既存の絵の右外に置く。重ねない。** 重ねると、生成物と手描きの区別が
 * つかなくなり、気に入らないときに選び分けて消せない（#60）。付箋
 * （`stickyNotePosition`）が見えている範囲の中央に置くのとは逆の判断で、
 * 理由は 2 つある。ドラフトは**描いている最中の 1 手ではなく、見てから置く
 * 操作**なので、置いた先へ寄せる間があること。そして図 1 枚ぶんの大きさが
 * あるので、重なったときに巻き込む範囲が付箋 1 枚とは違うこと。
 *
 * **視界の外に出ることは織り込んである。** 置いたあとにその範囲へ寄せるのは
 * 呼び出し側の責務（`scrollToContent`）。寄せないと、押したのに何も起きて
 * いないように見える。
 */
export function draftOrigin(existing: readonly SceneElement[]): Point {
  const box = boundingBox(existing);
  if (!box) return { x: 0, y: 0 };
  // 上端を揃えるのは、既存の絵と並んで読めるようにするため。
  return { x: box.right + DRAFT_GAP, y: box.top };
}

/**
 * ドラフトの左上が `origin` に来るように、要素をまとめて動かす。
 *
 * **要素は書き換えず新しい配列を返す。** Excalidraw は要素の同一性で再描画を
 * 判断するので、その場で書き換えると反映されないことがある
 * （`markAsAnnotation` と同じ理由）。
 *
 * 全要素を同じだけずらすので、矢印の結びつきもラベルの位置も相対のまま保たれる。
 */
export function moveDraft(
  elements: readonly SceneElement[],
  origin: Point,
): SceneElement[] {
  const box = boundingBox(elements);
  if (!box) return [...elements];

  const dx = origin.x - box.left;
  const dy = origin.y - box.top;

  return elements.map((el) => ({
    ...el,
    x: el.x === undefined ? el.x : el.x + dx,
    y: el.y === undefined ? el.y : el.y + dy,
  }));
}

type Box = { left: number; top: number; right: number; bottom: number };

/**
 * 要素をすべて含む矩形。囲めるものが 1 つも無ければ undefined。
 *
 * 削除済みは数えない。消した絵のぶんまで避けると、ドラフトが理由もなく
 * 遠くへ離れていく（`stickyNotePosition` が削除済みを避けないのと同じ）。
 */
function boundingBox(elements: readonly SceneElement[]): Box | undefined {
  let box: Box | undefined;

  for (const el of elements) {
    if (el.isDeleted) continue;
    if (el.x === undefined || el.y === undefined) continue;

    const right = el.x + (el.width ?? 0);
    const bottom = el.y + (el.height ?? 0);
    box = box
      ? {
          left: Math.min(box.left, el.x),
          top: Math.min(box.top, el.y),
          right: Math.max(box.right, right),
          bottom: Math.max(box.bottom, bottom),
        }
      : { left: el.x, top: el.y, right, bottom };
  }

  return box;
}
