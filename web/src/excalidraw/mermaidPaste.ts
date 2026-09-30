import type { SceneElement } from "./annotation";
import {
  type DraftFailure,
  MERMAID_MAX_TEXT_SIZE,
  type MermaidParser,
  mermaidToElements,
} from "./mermaid";

/**
 * 貼られた mermaid を置けなかった理由。**呼び出し側はこれで文言を選ぶ。**
 *
 * 前の 3 つは mermaid に渡す前に etoki が拒んだもの、残りは変換を試した結果
 * （`DraftFailure`）。図のドラフトと違って**どれも頼み直す相手がいない**ので、
 * 分けるのは「人が何を直せばよいか」のため。
 *
 * - `empty` — フェンスを剥がしたら中身が無かった。
 * - `kind` — 受け付けない種類だった。
 * - `tooLarge` — mermaid が変換できる長さを超えていた。
 */
export type PasteFailure = "empty" | "kind" | "tooLarge" | DraftFailure;

/** 貼り付けを変換した結果。**例外ではなく値で返す**（`mermaidToElements` と同じ）。 */
export type PastedDiagram =
  | { ok: true; elements: SceneElement[] }
  /**
   * `detail` は変換器のメッセージ。構文エラーのときだけ画面に畳んで出す
   * （ADR 0062）。それ以外は空か console 向け。
   */
  | { ok: false; reason: PasteFailure; detail: string };

/**
 * 受け付ける図の書き出しの語。
 *
 * **mermaid に渡す前に絞る。** 固定している mermaid 11.13.0 には、描いた
 * だけでタブが固まる種類（gantt / radar / xychart）と、プロトタイプ汚染の
 * ある種類（architecture）が残っている（ADR 0061）。固まると未保存の
 * ブレストが消える。変換してから画像を拒む門番（`NOT_DRAWABLE`）では
 * 間に合わない。
 *
 * 3 種に絞った理由は ADR 0062。class 図と state 図は 11.13.0 でも図形に
 * なるが、state 図には HTML の注入が残っているので入れていない。
 */
const ACCEPTED_KEYWORDS = ["erDiagram", "sequenceDiagram", "flowchart", "graph"];

/**
 * 受け付ける種類を人に見せるときの書き方。
 *
 * パネルの案内と、種類で拒んだときの文言（`mermaidPasteFailure`）の両方が
 * 使う。**上の語の並びと同じコミットで直す。** 片方だけ直すと、置けると
 * 案内した種類が拒まれる。
 */
export const ACCEPTED_KINDS_LABEL = "erDiagram・sequenceDiagram・flowchart（graph）";

/**
 * 受け付ける種類かを、**mermaid が種類を見分けるのと同じ手順で**確かめる。
 *
 * mermaid（`detectType`）は frontmatter・`%%{…}%%`・`%%` コメントを取り除いて
 * から、先頭の語で種類を決める。取り除き方が mermaid とずれると、etoki が
 * erDiagram だと思った文字列を mermaid が gantt として描く抜け道になる。
 * frontmatter と `%%{…}%%` の正規表現は mermaid 11.13.0 の `detectType` から
 * 写した。`%%` コメントだけは写さずに書き直してある（`stripComments`）。
 *
 * **語の境界まで見る。** mermaid の検出は前方一致なので `flowchart-elk` も
 * flowchart の仲間として拾うが、etoki が確かめていない描き方なので通さない。
 */
export function isAcceptedKind(definition: string): boolean {
  const withoutDirectives = definition
    .replace(/^-{3}\s*[\n\r](.*?)[\n\r]-{3}\s*[\n\r]+/s, "")
    .replace(
      /%{2}{\s*(?:(\w+)\s*:|(\w+))\s*(?:(\w+)|((?:(?!}%{2}).|\r?\n)*))?\s*(?:}%{2})?/gi,
      "",
    );
  const body = stripComments(withoutDirectives);

  const head = /^\s*([^\s]+)/.exec(body)?.[1];
  return head !== undefined && ACCEPTED_KEYWORDS.includes(head);
}

/**
 * `%%` コメントを取り除く。**mermaid の `/\s*%%.*\n/gm` と同じ行を消す。**
 *
 * 正規表現を写さないのは、改行で終わらない長い行で 2 乗の時間がかかるため。
 * 上限（`MERMAID_MAX_TEXT_SIZE`）いっぱいの `%` 1 行で数秒かかり、その間
 * ブラウザのメインスレッドが止まってキャンバスも触れない。
 *
 * 消えるのは「`\n` で終わる行の、最初の `%%` から行末まで」。`.` は `\r`・
 * `\u2028`・`\u2029` にも当たらないので、それらより前の `%%` は消えない。
 * mermaid が前に付く空白（改行を含む）も消すのとは違うが、見たいのは
 * 先頭の語だけなので結果は変わらない（テストで mermaid の正規表現と突き合わせている）。
 */
export function stripComments(text: string): string {
  const lines = text.split("\n");
  // 最後の要素は `\n` で終わっていないので、mermaid も消さない。
  for (let i = 0; i < lines.length - 1; i++) {
    const line = lines[i] ?? "";
    const runStart =
      Math.max(
        line.lastIndexOf("\r"),
        line.lastIndexOf("\u2028"),
        line.lastIndexOf("\u2029"),
      ) + 1;
    const at = line.indexOf("%%", runStart);
    if (at !== -1) lines[i] = line.slice(0, at);
  }
  return lines.join("\n");
}

/**
 * 入力全体を囲む Markdown のフェンスを 1 組だけ剥がす。
 *
 * GitHub の README や issue から写すと ```` ```mermaid ```` が付いてくる。
 * **全体が 1 つのフェンスで囲まれているときだけ**剥がす。途中にある
 * フェンスや複数の図を Markdown から拾い出すことはしない（どれを置くかを
 * etoki が選ぶことになる）。
 */
export function unwrapFence(text: string): string {
  const fenced = /^\s*```[ \t]*(?:mermaid)?[ \t]*\r?\n([\s\S]*?)\r?\n[ \t]*```\s*$/.exec(
    text,
  );
  if (!fenced) return text;
  const inner = fenced[1] ?? "";
  // 中にフェンスの行が残っていれば、2 つ以上のブロックを 1 つとして囲んだ
  // 取り違え。剥がすと ``` を含む中身を mermaid に渡すことになる。
  return /^[ \t]*```/m.test(inner) ? text : inner;
}

/** 「キャンバスに置く」を押せるか。空白だけなら押させない。 */
export function canPaste(text: string): boolean {
  return text.trim() !== "";
}

/**
 * 貼られた mermaid を Excalidraw の要素にする。**キャンバスには反映しない。**
 *
 * 置き場所は図のドラフトと同じく `draftOrigin` / `moveDraft` が決め、置くのは
 * 呼び出し側（`BoardPage`）。**種類（`kind`）は付けない。** `erDiagram` を
 * 貼っても、注釈の種別を選ぶのは人（ADR 0047）。
 *
 * 前検査の順は「空 → 大きさ → 種類」。大きさを種類より先に見るのは、上限を
 * 超えた文字列に正規表現を当てないため。
 */
export async function pasteToElements(
  text: string,
  parse?: MermaidParser,
): Promise<PastedDiagram> {
  const definition = unwrapFence(text);

  if (definition.trim() === "") return { ok: false, reason: "empty", detail: "" };
  // mermaid は元の文字列の長さで比べる（`text.length > maxTextSize`）。
  if (definition.length > MERMAID_MAX_TEXT_SIZE) {
    return { ok: false, reason: "tooLarge", detail: "" };
  }
  if (!isAcceptedKind(definition)) return { ok: false, reason: "kind", detail: "" };

  return mermaidToElements(definition, parse);
}
