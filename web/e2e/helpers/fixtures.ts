import type {
  AnnotationStatus,
  BoardDetail,
  CreatedRun,
  DiagramDraft,
  Interpretation,
  Project,
  Repository,
  SessionStatus,
} from "../../src/api/types";
import { summarize, type ApiMock } from "./api";

export const BOARD_ID = "board-1";

/** 3 状態それぞれを 1 つずつ持たせてある。並びは画面の並びと同じ。 */
export const ANNOTATION_IDS = {
  uncreated: "frame-uncreated",
  created: "frame-created",
  changed: "frame-changed",
  /** 名前を付けていない frame。Excalidraw の既定はこちら（ADR 0022）。 */
  unnamed: "frame-unnamed",
} as const;

/** 作成先を選び終えたボード。ほとんどのテストはここから始まる。 */
export function board(): BoardDetail {
  return {
    id: BOARD_ID,
    name: "認証まわりのブレスト",
    role: "owner",
    createdAt: "2026-08-01T09:00:00Z",
    updatedAt: "2026-08-05T09:30:00Z",
    scene: statesScene(),
    repositoryOwner: "acme",
    repositoryName: "web",
    projectId: "PVT_1",
    // 表示用の値は作成先を選んだ時点のスナップショット（ADR 0019 / 0025）。
    projectNumber: 1,
    projectTitle: "ロードマップ",
    projectUrl: "https://github.com/orgs/acme/projects/1",
    targetLocked: false,
    sceneOverLimit: false,
  };
}

/** Excalidraw の要素が必ず持つフィールド。中身はテストでは読まない。 */
const ELEMENT_BASE = {
  angle: 0,
  strokeColor: "#1e1e1e",
  backgroundColor: "transparent",
  fillStyle: "solid",
  strokeWidth: 1,
  strokeStyle: "solid",
  roughness: 1,
  opacity: 100,
  groupIds: [],
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
};

/**
 * 注釈の frame。
 *
 * `name` に null を渡せるのは、Excalidraw が作る frame の既定がそれだから
 * （ADR 0022）。名前なしの見え方は既定の再現でしか確かめられない。
 */
function annotationFrame(id: string, name: string | null, x: number) {
  return {
    ...ELEMENT_BASE,
    id,
    type: "frame",
    name,
    x,
    y: 0,
    width: 400,
    height: 300,
    frameId: null,
    // これがあって初めて注釈になる。frame 単体を条件にすると、ブレスト中に
    // 使った frame まで注釈と誤認する。
    customData: { etoki: { granularity: "" } },
  };
}

function sceneOf(elements: unknown[]): string {
  return JSON.stringify({
    type: "excalidraw",
    version: 2,
    source: "etoki-e2e",
    elements,
    appState: {},
    files: {},
  });
}

/**
 * 注釈の frame と、その中のテキストを持つシーン。
 *
 * 既定の `statesScene` と違って中身がある。画像の書き出しは frame の実体を
 * 要り、写るものが無いと書き出せたのかが分からない。
 */
export function annotatedScene(): string {
  return sceneOf([
    annotationFrame(ANNOTATION_IDS.uncreated, "ログイン", 0),
    {
      ...ELEMENT_BASE,
      id: "text-in-frame",
      type: "text",
      x: 40,
      y: 40,
      width: 200,
      height: 24,
      text: "ログインの入口",
      originalText: "ログインの入口",
      fontSize: 20,
      fontFamily: 1,
      textAlign: "left",
      verticalAlign: "top",
      lineHeight: 1.25,
      containerId: null,
      frameId: ANNOTATION_IDS.uncreated,
    },
  ]);
}

/**
 * `annotations()` の 3 つに対応する frame を持つシーン。
 *
 * 既定のボードはこれで開く。注釈だけあって frame が無いシーンは実際には
 * 起こらない状態で、そのままだと全部のカードが「キャンバスにありません」に
 * なる（ADR 0022）。
 */
function statesScene(): string {
  return sceneOf([
    annotationFrame(ANNOTATION_IDS.uncreated, "ログイン", 0),
    annotationFrame(ANNOTATION_IDS.created, "パスワード再設定", 600),
    annotationFrame(ANNOTATION_IDS.changed, "セッション管理", 1200),
  ]);
}

/**
 * 注釈の frame を 2 つ持ち、2 つ目に名前が無いシーン。
 *
 * 名前だけでは項目を見分けられない状態そのもの。`annotations()` の 3 つとは
 * 対応しないので、`multiFrameAnnotations` と組で使う。
 */
function multiFrameScene(): string {
  return sceneOf([
    annotationFrame(ANNOTATION_IDS.uncreated, "ログイン", 0),
    annotationFrame(ANNOTATION_IDS.unnamed, null, 600),
  ]);
}

/** `multiFrameScene` に対応する注釈の状態。2 つ目は名前が空。 */
function multiFrameAnnotations(): AnnotationStatus[] {
  return [
    {
      id: ANNOTATION_IDS.uncreated,
      name: "ログイン",
      granularity: "",
      state: "uncreated",
    },
    { id: ANNOTATION_IDS.unnamed, name: "", granularity: "", state: "uncreated" },
  ];
}

/**
 * ふつうの frame。注釈にしていないので `customData.etoki` を持たない。
 *
 * ブレスト中にユーザーが自分の用途で使った frame そのもの。注釈の frame と
 * 混在するのが前提なので（ルートの CLAUDE.md）、見分けが付くかどうかは
 * 混ぜた状態でしか確かめられない。
 */
function plainFrame(id: string, name: string, x: number) {
  return { ...ELEMENT_BASE, id, type: "frame", name, x, y: 0, width: 400, height: 300 };
}

/**
 * 注釈の frame（粒度つきを含む）と、ただの frame が混ざったシーン。
 *
 * 3 つが同時に画面へ収まる大きさにしてある。並べて写らないと、見分けが
 * 付くかどうかをスクリーンショットで確かめられない。ツールバーに重ならない
 * よう下げてある。
 */
function mixedFramesScene(): string {
  const box = { y: 260, width: 260, height: 220 };
  return sceneOf([
    { ...annotationFrame(ANNOTATION_IDS.uncreated, "ログイン", 20), ...box },
    {
      ...annotationFrame(ANNOTATION_IDS.created, "パスワード再設定", 310),
      ...box,
      customData: { etoki: { granularity: "epic" } },
    },
    { ...plainFrame("frame-plain", "メモ", 600), ...box },
  ]);
}

/** 注釈の frame とただの frame が混ざったボード。 */
export function mixedFramesMock(): ApiMock {
  const mock = baseMock();
  mock.details[BOARD_ID] = { ...board(), scene: mixedFramesScene() };
  mock.annotations[BOARD_ID] = annotations().slice(0, 2);
  return mock;
}

/** シーンと注釈の両方を差し替えた、名前なしの注釈を含むボード。 */
export function multiFrameMock(): ApiMock {
  const mock = baseMock();
  mock.details[BOARD_ID] = { ...board(), scene: multiFrameScene() };
  mock.annotations[BOARD_ID] = multiFrameAnnotations();
  return mock;
}

/** まだ作成先を選んでいないボード。開くとリポジトリ選択に入る。 */
export function unselectedBoard(): BoardDetail {
  return {
    ...board(),
    id: "board-unselected",
    name: "作成先未選択のブレスト",
    repositoryOwner: "",
    repositoryName: "",
    projectId: "",
    projectNumber: 0,
    projectTitle: "",
  };
}

/**
 * 認可画面の URL。
 *
 * 実際の github.com には行かせない。外部連携そのものは E2E の担当ではない
 * （ADR 0012）。遷移したことだけを確かめる。
 */
export const AUTHORIZE_URL = "https://github.test/login/oauth/authorize?state=e2e";

/** ログイン済みの状態。 */
export function signedIn(): SessionStatus {
  return {
    authRequired: true,
    authenticated: true,
    user: { provider: "github", login: "octocat", displayName: "Octo Cat" },
  };
}

export function repositories(): Repository[] {
  return [
    { owner: "acme", name: "web", description: "フロントエンド" },
    { owner: "acme", name: "api" },
  ];
}

export function projects(): Project[] {
  return [
    // URL は GitHub が返すもので、番号から組み立てたものではない。owner が
    // user か org かで形が変わる（ADR 0025）。両方の形を混ぜてある。
    {
      id: "PVT_1",
      number: 1,
      title: "ロードマップ",
      url: "https://github.com/orgs/acme/projects/1",
    },
    {
      id: "PVT_2",
      number: 4,
      title: "技術的負債",
      url: "https://github.com/users/acme/projects/4",
    },
  ];
}

export function annotations(): AnnotationStatus[] {
  return [
    {
      id: ANNOTATION_IDS.uncreated,
      name: "ログイン",
      granularity: "",
      state: "uncreated",
    },
    {
      id: ANNOTATION_IDS.created,
      name: "パスワード再設定",
      granularity: "epic",
      state: "created",
      lastSyncedAt: "2026-08-04T12:00:00Z",
      items: [
        {
          itemId: "PVTI_epic",
          kind: "epic",
          title: "パスワード再設定",
          body: "忘れたときの導線をまとめる",
          localId: "e1",
          action: "created",
        },
        {
          itemId: "PVTI_issue",
          kind: "issue",
          title: "再設定メールを送る",
          body: "有効期限つきのリンクを送る",
          localId: "i1",
          parentLocalId: "e1",
          action: "created",
        },
      ],
    },
    {
      id: ANNOTATION_IDS.changed,
      name: "セッション管理",
      granularity: "issue",
      state: "changed",
      lastSyncedAt: "2026-08-03T12:00:00Z",
      items: [
        {
          itemId: "PVTI_old",
          kind: "issue",
          title: "セッションの有効期限",
          // 本文を記録していなかった頃に作られた item。空文字で返る。
          body: "",
          localId: "i9",
          action: "created",
        },
      ],
    },
  ];
}

export function interpretation(): Interpretation {
  return {
    summary: "ログインの入口まわりを 1 つの epic として読みました。",
    contentHash: "sha256:e2e",
    items: [
      { localId: "e1", kind: "epic", title: "ログイン基盤", body: "入口をまとめる" },
      {
        localId: "i1",
        kind: "issue",
        title: "メールとパスワードでログインする",
        body: "フォームと検証",
        parentLocalId: "e1",
      },
      {
        localId: "i2",
        kind: "issue",
        title: "ログイン失敗を数える",
        body: "連続失敗の記録",
        parentLocalId: "e1",
      },
    ],
  };
}

export function createdRun(): CreatedRun {
  return {
    runId: 7,
    createdAt: "2026-08-05T10:00:00Z",
    items: [
      // 解釈結果と同じ本文で作られる。作成後もそのまま読める（ADR 0022）。
      {
        itemId: "PVTI_1",
        kind: "epic",
        title: "ログイン基盤",
        body: "入口をまとめる",
        localId: "e1",
        action: "created",
      },
      {
        itemId: "PVTI_2",
        kind: "issue",
        title: "メールとパスワードでログインする",
        body: "フォームと検証",
        localId: "i1",
        parentLocalId: "e1",
        action: "created",
      },
      {
        itemId: "PVTI_3",
        kind: "issue",
        title: "ログイン失敗を数える",
        body: "連続失敗の記録",
        localId: "i2",
        parentLocalId: "e1",
        action: "created",
      },
    ],
  };
}

/**
 * 生成された図のドラフト（ADR 0041）。
 *
 * **変換できる mermaid にしてある。** 置ける形かどうかを決めるのは変換器なので、
 * ここに置けないものを書くと、置く流れの spec が別の理由で落ちる。
 */
export function diagramDraft(): DiagramDraft {
  return {
    kind: "todo",
    mermaid: "flowchart TD\n  A[受注] --> B[出荷]",
    turnsRemaining: 9,
  };
}

/** 素直に全部成功する状態。個別のテストが必要なところだけ書き換える。 */
export function baseMock(): ApiMock {
  const detail = board();
  return {
    boards: [summarize(detail)],
    details: { [detail.id]: detail },
    annotations: { [detail.id]: annotations() },
    interpret: { status: 200, body: interpretation() },
    interpretRequests: [],
    createRequests: [],
    diagramDraft: { status: 200, body: diagramDraft() },
    diagramRequests: [],
    createItems: { status: 201, body: createdRun() },
    // 既定は認証を設定していない構成。ログインの導線を見る spec だけが
    // 書き換える。
    // 既定は全部そろった構成。未設定の見せ方を確かめる spec だけが落とす。
    capabilities: {
      status: 200,
      body: { interpretation: true, diagramDraft: true, creation: true, sharing: true },
    },
    session: { status: 200, body: { authRequired: false, authenticated: false } },
    login: { status: 200, body: { authorizeUrl: AUTHORIZE_URL } },
    repositories: { status: 200, body: repositories() },
    projects: {
      "acme/web": { status: 200, body: projects() },
      "acme/api": { status: 200, body: [] },
    },
  };
}

/**
 * `changed` の注釈で、LLM が前回ぶんとの対応づけを返した解釈（ADR 0026）。
 *
 * `i1` は前回の `PVTI_old` を書き換え、`i2` は新しく作る。前回ぶんの
 * `PVTI_kept` はどこからも指されないので取り残しになる。
 *
 * **spec ごとに書かない。** 同じ筋書きを 2 箇所で組むと、片方だけ直したときに
 * 食い違う。
 */
export function matchedInterpretationMock(): ApiMock {
  const mock = baseMock();

  mock.annotations[BOARD_ID] = (mock.annotations[BOARD_ID] ?? []).map((a) =>
    a.id !== ANNOTATION_IDS.changed
      ? a
      : {
          ...a,
          items: [
            {
              itemId: "PVTI_old",
              kind: "issue",
              title: "セッションの有効期限",
              body: "",
              localId: "i9",
              action: "created",
            },
            {
              itemId: "PVTI_kept",
              kind: "issue",
              title: "触らないほう",
              body: "",
              localId: "i8",
              action: "created",
            },
          ],
        },
  );

  mock.interpret = {
    status: 200,
    body: {
      summary: "前回の続きとして読みました。",
      contentHash: "sha256:e2e",
      items: [
        {
          localId: "i1",
          kind: "issue",
          title: "セッションの有効期限を延ばす",
          body: "書き直した本文",
          previousItemId: "PVTI_old",
        },
        { localId: "i2", kind: "issue", title: "新しく足す issue", body: "" },
      ],
    },
  };

  return mock;
}
