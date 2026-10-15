import type { Theme } from "../app/theme";
import createDark from "./media/dark/page3.mp4";
import interpretDark from "./media/dark/page2.mp4";
import drawDark from "./media/dark/page1.mp4";
import createLight from "./media/light/page3.mp4";
import interpretLight from "./media/light/page2.mp4";
import drawLight from "./media/light/page1.mp4";

/** 使い方の 1 ページ。動画と、動画と同じことを書いた本文。 */
export type TutorialPage = {
  heading: string;
  /**
   * 動画と同じ内容の説明。**動画の中身は読み上げに届かない**ので、本文として
   * 出して動画から結ぶ（ADR 0081）。
   */
  body: string;
  /** テーマごとの動画。テーマは OS に追従するので、両方を録る（ADR 0055）。 */
  video: Record<Theme, string>;
};

/**
 * 使い方のページ（#66、ADR 0081）。
 *
 * **順序は中核思想 1 で決まる。** epic / issue は 3 ページ目まで出さない。
 * 最初に構造の語彙を教えると、ブレストの前に構造を意識させることになる。
 * `pages.test.ts` がこれを見ている。
 *
 * 動画は `make tutorial-capture` が録る（`web/e2e/tutorial.capture.ts`）。
 * **画面を変えたら撮り直す。** 古い動画のままでも、どのテストも落ちない。
 */
export const TUTORIAL_PAGES: readonly TutorialPage[] = [
  {
    heading: "描いて、囲む",
    body:
      "思いついたことを、付箋を貼るように描いていきます。まとまりが見えてきたら、" +
      "フレームツール（F）でその範囲を囲みます。囲んだフレームを選んで、右の" +
      "パネルの「注釈にする」を押すと、絵解きの対象になります。",
    video: { light: drawLight, dark: drawDark },
  },
  {
    heading: "絵解く",
    body:
      "右上の「絵解き」を押すと、保存してから囲みの面が開きます。「絵解く」を" +
      "押すと、AI が囲みの中身を読み、読み解きと、作るものの候補を出します。" +
      "候補は選び直したり、書き直したりできます。",
    video: { light: interpretLight, dark: interpretDark },
  },
  {
    heading: "GitHub に作る",
    body:
      "作るものは epic と、その下の issue の 2 段です。選んだものだけを" +
      "「GitHub に作成する」で、ボードの作成先の Project に draft issue として" +
      "作ります。作成は取り消せません。Project には、種別と親を入れるフィールドが" +
      "要ります。既定の名前は Kind（単一選択で、選択肢に epic と issue）と" +
      "Parent（テキスト）です。",
    video: { light: createLight, dark: createDark },
  },
];
