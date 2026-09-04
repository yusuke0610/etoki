import { convertToExcalidrawElements } from "@excalidraw/excalidraw";

import type { DiagramKind } from "../api/types";
import type { SceneElement } from "./annotation";
import { STICKY_SIZE, STICKY_STYLE } from "./sticky";

/**
 * 新しいボードを何から始めるか。
 *
 * `""` は「空白」で、**既定はこちら**（中核思想 3）。テンプレートは選ばせる
 * ものであって、勝手に適用するものではない。
 *
 * 種別そのもの（`DiagramKind`）に空白を足さないのは、あれが「何の図か」の
 * 語彙で、空白は図ですらないため。契約の enum に空文字を足すと、図のドラフト
 * 生成（`GenerateDiagramRequest.kind`）まで指定なしを受け付ける形になる。
 */
export type TemplateChoice = "" | DiagramKind;

/** 空白（テンプレートを使わない）。 */
export const BLANK_TEMPLATE = "";

/**
 * ひな形に置く言葉。**どれも書き換えられる前提の仮置き。**
 *
 * **etoki の構造語（epic / issue）を置かない**（中核思想 1）。ここに枠として
 * 置いた時点で、ブレストフェーズに issue の概念を持ち込むことになる。置くのは
 * 設計の構造（登場人物、実体、構成要素）までで、それは図の言葉であって
 * etoki の言葉ではない。
 *
 * **役割の言葉で書く。** 「実体 1」のような番号だけの仮置きにすると、何を書く
 * 場所なのかが図から読めない。逆に特定の製品名（「Stripe」など）まで踏み込むと、
 * 書き換え忘れたときに解釈がその言葉を拾い、描いていないものが draft issue の
 * 候補に出てくる。**どの開発にも当てはまる役割名**が、その 2 つの間になる。
 */
const PLACEHOLDER = {
  todo: ["要件を洗い出す", "試作を作る", "レビューしてもらう"],
  mindmapCenter: "新しい機能",
  mindmapBranches: ["使う人", "困っていること", "試したいこと"],
  sequenceActors: ["利用者", "画面", "サーバー"],
  sequenceMessages: ["操作する", "結果を返す"],
  erEntities: ["利用者", "注文", "商品"],
  erAttributes: ["名前", "作成日時"],
  architectureBoundary: "サーバー側",
  architectureNodes: ["フロントエンド", "API", "データベース"],
} as const;

/**
 * 変換器に渡す骨格。
 *
 * ライブラリの `ExcalidrawElementSkeleton` をそのまま受けないのは、
 * `mermaid.ts` の `ElementSkeleton` と同じ理由で、ここで何を組み立てて
 * いるのかが型から読めるようにするため。詰め替えはライブラリに任せる。
 */
type Skeleton = Record<string, unknown> & { type: string; id?: string };

/**
 * テンプレート 1 つぶんの要素を組み立てる。
 *
 * **注釈の frame は作らない。** etoki が frame を自前生成しない線
 * （ルートの `CLAUDE.md`、ADR 0040 で mermaid の変換結果から frame を拒むのと
 * 同じ理由）を、ひな形のためにも曲げない。**ひな形が配るのは絵だけ**で、
 * どこを issue 化の対象にするかは、これまでどおり人がフレームツールで囲んで
 * 決める（ADR 0044）。
 *
 * その帰結として、**選んだ種別はここには載らない。** 種別は注釈のメタデータ
 * なので、載る先は人が引いた frame。選ぶのは注釈パネル（`AnnotationPanel` の
 * 「種別」）で、ひな形の選択とは別の操作になる。
 *
 * **座標は原点から組む。** 置き場所を決めるのは新規ボードのときだけなので、
 * 既存の絵を避ける必要が無い（`draftOrigin` が要るのはドラフトの側）。
 */
export function templateElements(kind: DiagramKind): SceneElement[] {
  return convertToExcalidrawElements(
    SKELETONS[kind]() as never[],
  ) as unknown as SceneElement[];
}

/**
 * テンプレートから新しいボードのシーン JSON を作る。空白なら undefined。
 *
 * **空白は「空のシーン」を組み立てて送るのではなく、送らない。** 既定の空
 * シーンを持っているのはサーバー（`usecase.emptyScene`）で、ここで組み立てる
 * と同じものが 2 箇所になる。`CreateBoardRequest.scene` が省略可能なのは
 * そのため。
 *
 * 形は `usecase.emptyScene` に合わせた最小のシーン。`serializeAsJSON` を
 * 通さないのは、まだ `appState` を持つキャンバスが無いため。読み込み時に
 * Excalidraw の `restore` が既定値を埋める。
 */
export function templateScene(choice: TemplateChoice): string | undefined {
  if (choice === BLANK_TEMPLATE) return undefined;

  return JSON.stringify({
    type: "excalidraw",
    version: 2,
    source: "etoki",
    elements: templateElements(choice),
    appState: {},
  });
}

/**
 * 直線の骨格。**大きさと点列の両方を渡す。**
 *
 * `convertToExcalidrawElements` は `line` について、点列だけを渡すと既定の
 * 100x0 に潰し、大きさだけを渡すと点列を斜めに作る（`arrow` は点列だけで
 * 通る）。**片方だけでは意図した線にならない**ので、2 つを 1 箇所で組む。
 * 直に書くと、書いた場所ごとに片方を忘れる。
 */
function horizontal(length: number): Record<string, unknown> {
  return {
    width: length,
    height: 0,
    points: [
      [0, 0],
      [length, 0],
    ],
  };
}

function vertical(length: number): Record<string, unknown> {
  return {
    width: 0,
    height: length,
    points: [
      [0, 0],
      [0, length],
    ],
  };
}

/** 図の間隔と大きさ（シーン座標）。 */
const GAP = 60;
const BOX_WIDTH = 200;
const BOX_HEIGHT = 90;

/**
 * 種別ごとの中身。
 *
 * **`Record` にすることが担保。** `DiagramKind` は `api/openapi.yaml` からの
 * 生成物なので、契約に種類を足してひな形を書き忘れると `tsc` が落ちる
 * （`DIAGRAM_KIND_LABELS` と同じ形）。
 */
const SKELETONS: Record<DiagramKind, () => Skeleton[]> = {
  todo: todoSkeletons,
  mindmap: mindmapSkeletons,
  sequence: sequenceSkeletons,
  er: erSkeletons,
  architecture: architectureSkeletons,
};

/**
 * やることの洗い出し。**いちばん構造の薄いひな形。**
 *
 * 付箋を並べるだけに近い。矢印で結ばないのは、todo が列挙であって順序では
 * ないため。順序があるなら描く人が引く。
 */
function todoSkeletons(): Skeleton[] {
  return PLACEHOLDER.todo.map((text, i) => ({
    type: "rectangle",
    id: `todo-${i}`,
    x: i * (STICKY_SIZE + GAP),
    y: 0,
    width: STICKY_SIZE,
    height: STICKY_SIZE,
    ...STICKY_STYLE,
    label: { text },
  }));
}

/**
 * 発想を広げる放射状の枝。
 *
 * **枝は矢印で表す。** 階層は矢印と配置にしか現れないので、テキスト一覧には
 * 「中心が何で、どれがその子か」が入らない。注釈範囲の画像を添えたときだけ
 * 親子が伝わる（ADR 0018）ひな形であり、画像なしでも成立するのは中心と枝の
 * 語だけになる。**そこを補う指示は書かない**（`usecase.diagramReadings`）。
 */
function mindmapSkeletons(): Skeleton[] {
  const centerY = ((PLACEHOLDER.mindmapBranches.length - 1) * (BOX_HEIGHT + GAP)) / 2;

  const center: Skeleton = {
    type: "ellipse",
    id: "mindmap-center",
    x: 0,
    y: centerY,
    width: BOX_WIDTH,
    height: BOX_HEIGHT,
    label: { text: PLACEHOLDER.mindmapCenter },
  };

  const branches: Skeleton[] = PLACEHOLDER.mindmapBranches.map((text, i) => ({
    type: "rectangle",
    id: `mindmap-branch-${i}`,
    x: BOX_WIDTH + GAP * 3,
    y: i * (BOX_HEIGHT + GAP),
    width: BOX_WIDTH,
    height: BOX_HEIGHT,
    label: { text },
  }));

  // **枝ごとに終点まで引く。** 結びつき（start / end）だけを渡すと、変換器は
  // 既定の長さ（右へ 100）で作り、**要素を動かすまで引き直されない。** 3 本の
  // 矢印がぴったり重なって 1 本に見えるので、点列も渡して初めてひな形になる。
  const arrows: Skeleton[] = branches.map((branch, i) => ({
    type: "arrow",
    id: `mindmap-arrow-${i}`,
    x: BOX_WIDTH,
    y: centerY + BOX_HEIGHT / 2,
    points: [
      [0, 0],
      [GAP * 3, i * (BOX_HEIGHT + GAP) - centerY],
    ],
    start: { id: center.id },
    end: { id: branch.id },
  }));

  return [center, ...branches, ...arrows];
}

/**
 * 登場人物とやりとりの順序。
 *
 * **順序と向きはテキスト一覧に一切現れない。** 矢印の始点・終点・上下位置が
 * 情報を全部持っているので、画像を添えないと人物名と操作名がばらばらに並ぶ
 * だけになる（ADR 0018）。
 *
 * **矢印の向きから呼び出し方向を決める実装を書かないこと**（中核思想 2）。
 * `domain.Element` が読むフィールドが増えていないことが、守れている印になる。
 */
function sequenceSkeletons(): Skeleton[] {
  const laneGap = BOX_WIDTH + GAP * 2;
  const lifelineTop = BOX_HEIGHT + GAP / 2;
  const lifelineLength = (PLACEHOLDER.sequenceMessages.length + 1) * (BOX_HEIGHT + GAP);

  const heads: Skeleton[] = PLACEHOLDER.sequenceActors.map((text, i) => ({
    type: "rectangle",
    id: `sequence-actor-${i}`,
    x: i * laneGap,
    y: 0,
    width: BOX_WIDTH,
    height: BOX_HEIGHT,
    label: { text },
  }));

  // 生存線。**矢印ではなく線で引く。** 矢印にすると、時間の流れが
  // やりとりと同じ記号になり、どちらが順序なのか読めなくなる。
  const lifelines: Skeleton[] = PLACEHOLDER.sequenceActors.map((_, i) => ({
    type: "line",
    id: `sequence-lifeline-${i}`,
    x: i * laneGap + BOX_WIDTH / 2,
    y: lifelineTop,
    ...vertical(lifelineLength),
    strokeStyle: "dashed",
  }));

  // やりとりは隣のレーンへ。上から順に並べる。
  const messages: Skeleton[] = PLACEHOLDER.sequenceMessages.map((text, i) => ({
    type: "arrow",
    id: `sequence-message-${i}`,
    x: i * laneGap + BOX_WIDTH / 2,
    y: lifelineTop + (i + 1) * (BOX_HEIGHT / 2 + GAP),
    ...horizontal(laneGap),
    label: { text },
  }));

  return [...heads, ...lifelines, ...messages];
}

/**
 * 実体と関連。
 *
 * **このひな形だけ、テキスト一覧がかなり効く。** 実体名も属性名も文字として
 * 書かれるので、画像が無くても中身は伝わる。落ちるのは関連と多重度だけ。
 *
 * 属性は実体の箱とは別のテキスト要素にする。1 つの図形に紐づけられるラベルは
 * 1 つなので、見出しをラベルに、属性は frame の直接の子として置く。
 * **どちらの経路も `Scene.AnnotationTexts` が拾う**（`containerId` と
 * `frameId`）。
 *
 * **属性行に印を付けて構造を持たせない**（中核思想 2）。平坦な一覧から
 * 実体名と属性を区別できないのは承知のうえで、区別させるのは LLM の仕事。
 */
function erSkeletons(): Skeleton[] {
  const skeletons: Skeleton[] = [];
  const attributeLineHeight = 28;
  const entityHeight = BOX_HEIGHT + PLACEHOLDER.erAttributes.length * attributeLineHeight;

  PLACEHOLDER.erEntities.forEach((name, i) => {
    const x = i * (BOX_WIDTH + GAP * 2);

    skeletons.push({
      type: "rectangle",
      id: `er-entity-${i}`,
      x,
      y: 0,
      width: BOX_WIDTH,
      height: entityHeight,
      // 見出しは上に寄せる。属性行を下に並べるため。
      label: { text: name, verticalAlign: "top" },
    });

    PLACEHOLDER.erAttributes.forEach((attribute, j) => {
      skeletons.push({
        type: "text",
        id: `er-attribute-${i}-${j}`,
        x: x + GAP / 3,
        y: BOX_HEIGHT / 2 + j * attributeLineHeight,
        text: attribute,
        fontSize: 16,
      });
    });
  });

  // 関連。多重度と関連名は描く人が書く。**ひな形が決めない。**
  PLACEHOLDER.erEntities.slice(1).forEach((_, i) => {
    skeletons.push({
      type: "line",
      id: `er-relation-${i}`,
      x: (i + 1) * (BOX_WIDTH + GAP * 2) - GAP * 2,
      y: entityHeight / 2,
      ...horizontal(GAP * 2),
    });
  });

  return skeletons;
}

/**
 * 構成要素と境界。
 *
 * **境界は frame ではなく矩形で描く**（ADR 0044）。人がこの絵を frame で囲んで
 * 注釈にすると、frame で描いた境界はその中の frame になり、内側に入れた要素の
 * `frameId` が内側を指す。
 * `Scene.AnnotationTexts` は入れ子を辿らないので、境界の中に書いた文字が
 * まるごとハッシュと解釈から抜ける。矩形なら中身は注釈の直接の子のままで、
 * ラベルも `containerId` 経由で拾える。
 *
 * 内側の frame が注釈と誤認されないこと自体は、既存の判定規則
 * （`customData.etoki` を持つ frame だけが注釈）がすでに守っている。ここで
 * 避けているのは誤認ではなく、テキストの取りこぼしのほう。
 */
function architectureSkeletons(): Skeleton[] {
  const [first, ...rest] = PLACEHOLDER.architectureNodes;
  const boundaryPadding = GAP / 2;
  const boundaryX = BOX_WIDTH + GAP * 2 - boundaryPadding;

  const outside: Skeleton = {
    type: "rectangle",
    id: "architecture-node-0",
    x: 0,
    y: 0,
    width: BOX_WIDTH,
    height: BOX_HEIGHT,
    label: { text: first },
  };

  const inside: Skeleton[] = rest.map((text, i) => ({
    type: "rectangle",
    id: `architecture-node-${i + 1}`,
    x: boundaryX + boundaryPadding,
    y: i * (BOX_HEIGHT + GAP),
    width: BOX_WIDTH,
    height: BOX_HEIGHT,
    label: { text },
  }));

  const boundary: Skeleton = {
    type: "rectangle",
    id: "architecture-boundary",
    x: boundaryX,
    y: -boundaryPadding,
    width: BOX_WIDTH + boundaryPadding * 2,
    height: rest.length * (BOX_HEIGHT + GAP) - GAP + boundaryPadding * 2,
    strokeStyle: "dashed",
    backgroundColor: "transparent",
    label: { text: PLACEHOLDER.architectureBoundary, verticalAlign: "top" },
  };

  const link: Skeleton = {
    type: "arrow",
    id: "architecture-link-0",
    x: BOX_WIDTH,
    y: BOX_HEIGHT / 2,
    // start/end の binding だけでは既定の長さ 100 に潰れ、内側の最初の
    // ノード（x = boundaryX + boundaryPadding）まで届かない。大きさも渡す。
    ...horizontal(boundaryX + boundaryPadding - BOX_WIDTH),
    start: { id: outside.id },
    end: { id: inside[0]?.id },
  };

  // 境界を先に置いて背面にする。後から置くと構成要素を覆う。
  return [boundary, outside, ...inside, link];
}
