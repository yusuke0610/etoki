/**
 * ブレストらしい中身が入ったシーンを組み立てる。
 *
 * 空のキャンバスでは「注釈の frame に囲まれた図が実際どう見えるか」が分からない。
 * マインドマップとシーケンス図を 1 枚に置いて、報告用の画像を撮るために持つ。
 */

import type { AnnotationStatus } from "../../src/api/types";

type Element = Record<string, unknown>;

const BASE = {
  angle: 0,
  strokeColor: "#1e1e1e",
  backgroundColor: "transparent",
  fillStyle: "solid",
  strokeWidth: 2,
  strokeStyle: "solid",
  roughness: 1,
  opacity: 100,
  groupIds: [] as string[],
  roundness: null,
  seed: 1,
  version: 1,
  versionNonce: 1,
  isDeleted: false,
  boundElements: null,
  updated: 1,
  link: null,
  locked: false,
  index: null,
  frameId: null as string | null,
};

let seq = 0;
const nextId = (prefix: string) => `${prefix}-${++seq}`;

/** 文字幅の目安。全角は fontSize ぶん、半角はおよそ半分。 */
function textWidth(text: string, fontSize: number): number {
  let w = 0;
  for (const ch of text) {
    w += /[\x20-\x7e]/.test(ch) ? fontSize * 0.55 : fontSize;
  }
  return Math.ceil(w);
}

function text(
  value: string,
  x: number,
  y: number,
  opts: { fontSize?: number; frameId?: string; color?: string } = {},
): Element {
  const fontSize = opts.fontSize ?? 20;
  return {
    ...BASE,
    id: nextId("text"),
    type: "text",
    x,
    y,
    width: textWidth(value, fontSize),
    height: Math.round(fontSize * 1.25),
    text: value,
    originalText: value,
    fontSize,
    // 2 = Helvetica 系。手書きフォントは日本語がフォールバックして揃わない。
    fontFamily: 2,
    textAlign: "left",
    verticalAlign: "top",
    lineHeight: 1.25,
    containerId: null,
    strokeColor: opts.color ?? "#1e1e1e",
    frameId: opts.frameId ?? null,
  };
}

/** 箱と、その中央に置いた文字。付箋のかわり。 */
function note(
  value: string,
  x: number,
  y: number,
  w: number,
  h: number,
  opts: { fill?: string; frameId?: string; fontSize?: number; type?: string } = {},
): Element[] {
  const fontSize = opts.fontSize ?? 20;
  const tw = textWidth(value, fontSize);
  const th = Math.round(fontSize * 1.25);
  return [
    {
      ...BASE,
      id: nextId("box"),
      type: opts.type ?? "rectangle",
      x,
      y,
      width: w,
      height: h,
      backgroundColor: opts.fill ?? "#ffec99",
      fillStyle: "solid",
      roundness: { type: 3 },
      frameId: opts.frameId ?? null,
    },
    text(value, x + (w - tw) / 2, y + (h - th) / 2, { fontSize, frameId: opts.frameId }),
  ];
}

function arrow(
  x1: number,
  y1: number,
  x2: number,
  y2: number,
  opts: { frameId?: string; dashed?: boolean; color?: string } = {},
): Element {
  return {
    ...BASE,
    id: nextId("arrow"),
    type: "arrow",
    x: x1,
    y: y1,
    width: Math.abs(x2 - x1),
    height: Math.abs(y2 - y1),
    points: [
      [0, 0],
      [x2 - x1, y2 - y1],
    ],
    lastCommittedPoint: null,
    startBinding: null,
    endBinding: null,
    startArrowhead: null,
    endArrowhead: "arrow",
    elbowed: false,
    strokeStyle: opts.dashed ? "dashed" : "solid",
    strokeColor: opts.color ?? "#1971c2",
    frameId: opts.frameId ?? null,
  };
}

function line(
  x: number,
  y: number,
  height: number,
  opts: { frameId?: string } = {},
): Element {
  return {
    ...BASE,
    id: nextId("line"),
    type: "line",
    x,
    y,
    width: 0,
    height,
    points: [
      [0, 0],
      [0, height],
    ],
    lastCommittedPoint: null,
    strokeStyle: "dashed",
    strokeColor: "#868e96",
    strokeWidth: 1,
    frameId: opts.frameId ?? null,
  };
}

function frame(id: string, name: string, x: number, y: number, w: number, h: number) {
  return {
    ...BASE,
    id,
    type: "frame",
    name,
    x,
    y,
    width: w,
    height: h,
    // これがあって初めて注釈になる。frame 単体を条件にすると、ブレスト中に
    // 使った frame まで注釈と誤認する。
    customData: { etoki: { granularity: "" } },
  };
}

export const RICH_FRAME_IDS = {
  mindmap: "frame-mindmap",
  sequence: "frame-sequence",
} as const;

/** 開いたときの視点。保存済みシーンは appState に scroll と zoom を持つ。 */
export type View = { zoom: number; scrollX: number; scrollY: number };

export const VIEWS = {
  /** 2 つの frame が両方入る。 */
  overview: { zoom: 0.45, scrollX: 75, scrollY: 306 },
  /** マインドマップだけ。 */
  mindmap: { zoom: 0.9, scrollX: 42, scrollY: 162 },
  /** シーケンス図だけ。 */
  sequence: { zoom: 0.9, scrollX: -938, scrollY: 162 },
} as const satisfies Record<string, View>;

/** マインドマップとシーケンス図を 1 つずつ、それぞれ注釈の frame に入れたシーン。 */
export function richScene(view: View = VIEWS.overview): string {
  seq = 0;
  const m = RICH_FRAME_IDS.mindmap;
  const s = RICH_FRAME_IDS.sequence;

  const mindmap: Element[] = [
    ...note("認証を作り直す", 340, 310, 200, 90, {
      frameId: m,
      fill: "#d0bfff",
      type: "ellipse",
    }),

    ...note("ログイン", 40, 60, 210, 64, { frameId: m }),
    ...note("メール + パスワード", 40, 150, 210, 54, {
      frameId: m,
      fill: "#fff9db",
      fontSize: 16,
    }),

    ...note("サインアップ", 630, 60, 210, 64, { frameId: m }),
    ...note("招待された人だけ", 630, 150, 210, 54, {
      frameId: m,
      fill: "#fff9db",
      fontSize: 16,
    }),

    ...note("パスワード再設定", 40, 510, 230, 64, { frameId: m, fill: "#a5d8ff" }),
    ...note("再設定メールを送る", 40, 600, 230, 54, {
      frameId: m,
      fill: "#e7f5ff",
      fontSize: 16,
    }),

    ...note("セッション管理", 620, 510, 220, 64, { frameId: m, fill: "#a5d8ff" }),
    ...note("有効期限は 30 日", 620, 600, 220, 54, {
      frameId: m,
      fill: "#e7f5ff",
      fontSize: 16,
    }),

    arrow(360, 320, 250, 110, { frameId: m }),
    arrow(520, 320, 630, 110, { frameId: m }),
    arrow(370, 390, 260, 520, { frameId: m }),
    arrow(515, 390, 620, 520, { frameId: m }),
    arrow(145, 128, 145, 146, { frameId: m }),
    arrow(735, 128, 735, 146, { frameId: m }),
    arrow(155, 578, 155, 596, { frameId: m }),
    arrow(730, 578, 730, 596, { frameId: m }),
  ];

  const lanes: Array<[string, number]> = [
    ["ブラウザ", 1020],
    ["etoki API", 1350],
    ["GitHub", 1680],
  ];

  const message = (
    label: string,
    from: number,
    to: number,
    y: number,
    dashed = false,
  ): Element[] => {
    const tw = textWidth(label, 16);
    return [
      text(label, (from + to) / 2 - tw / 2, y - 26, { fontSize: 16, frameId: s }),
      arrow(from, y, to, y, { frameId: s, dashed }),
    ];
  };

  const sequence: Element[] = [
    ...lanes.flatMap(([name, x]) =>
      note(name, x, 40, 180, 56, { frameId: s, fill: "#b2f2bb" }),
    ),
    ...lanes.map(([, x]) => line(x + 90, 96, 520, { frameId: s })),

    ...message("POST /api/auth/login", 1110, 1440, 190),
    ...message("認可画面へ飛ばす", 1440, 1770, 300),
    ...message("code を返す", 1770, 1440, 410, true),
    ...message("セッション Cookie", 1440, 1110, 520, true),
  ];

  return JSON.stringify({
    type: "excalidraw",
    version: 2,
    source: "etoki-e2e",
    elements: [
      frame(m, "認証まわりの発散", 0, 0, 880, 700),
      frame(s, "ログインの流れ", 980, 0, 920, 700),
      ...mindmap,
      ...sequence,
    ],
    appState: {
      viewBackgroundColor: "#ffffff",
      zoom: { value: view.zoom },
      scrollX: view.scrollX,
      scrollY: view.scrollY,
    },
  });
}

/** 上のシーンに対応する注釈の状態。未作成と作成済みを 1 つずつ。 */
export function richAnnotations(): AnnotationStatus[] {
  return [
    {
      id: RICH_FRAME_IDS.mindmap,
      name: "認証まわりの発散",
      granularity: "",
      state: "uncreated",
    },
    {
      id: RICH_FRAME_IDS.sequence,
      name: "ログインの流れ",
      granularity: "epic",
      state: "created",
      lastSyncedAt: "2026-08-04T12:00:00Z",
      items: [
        { itemId: "PVTI_epic", kind: "epic", title: "ログインの流れ", localId: "e1" },
        {
          itemId: "PVTI_issue",
          kind: "issue",
          title: "OAuth のコールバックを受ける",
          localId: "i1",
          parentLocalId: "e1",
        },
      ],
    },
  ];
}
