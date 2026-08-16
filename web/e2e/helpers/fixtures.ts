import type {
  AnnotationStatus,
  BoardDetail,
  CreatedRun,
  Interpretation,
  Project,
  Repository,
  SessionStatus,
} from "../../src/api/types";
import { emptyScene, summarize, type ApiMock } from "./api";

export const BOARD_ID = "board-1";

/** 3 状態それぞれを 1 つずつ持たせてある。並びは画面の並びと同じ。 */
export const ANNOTATION_IDS = {
  uncreated: "frame-uncreated",
  created: "frame-created",
  changed: "frame-changed",
} as const;

/** 作成先を選び終えたボード。ほとんどのテストはここから始まる。 */
export function board(): BoardDetail {
  return {
    id: BOARD_ID,
    name: "認証まわりのブレスト",
    role: "owner",
    createdAt: "2026-08-01T09:00:00Z",
    updatedAt: "2026-08-05T09:30:00Z",
    scene: emptyScene(),
    repositoryOwner: "acme",
    repositoryName: "web",
    projectId: "PVT_1",
    // 表示名は作成先を選んだ時点のスナップショット（ADR 0019）。
    projectNumber: 1,
    projectTitle: "ロードマップ",
    targetLocked: false,
  };
}

/**
 * 注釈の frame を 1 つ持つシーン。
 *
 * ほとんどの spec は空のシーンで足りるが、画像の書き出しは frame の実体を要る。
 * frame の ID は uncreated の注釈に合わせてあり、パネルの「ログイン」と同じ
 * ものを指す。
 */
export function annotatedScene(): string {
  const base = {
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

  return JSON.stringify({
    type: "excalidraw",
    version: 2,
    source: "etoki-e2e",
    elements: [
      {
        ...base,
        id: ANNOTATION_IDS.uncreated,
        type: "frame",
        name: "ログイン",
        x: 0,
        y: 0,
        width: 400,
        height: 300,
        frameId: null,
        // これがあって初めて注釈になる。frame 単体を条件にすると、ブレスト中に
        // 使った frame まで注釈と誤認する。
        customData: { etoki: { granularity: "" } },
      },
      {
        ...base,
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
    ],
    appState: {},
    files: {},
  });
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
    { id: "PVT_1", number: 1, title: "ロードマップ" },
    { id: "PVT_2", number: 4, title: "技術的負債" },
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
        },
        {
          itemId: "PVTI_issue",
          kind: "issue",
          title: "再設定メールを送る",
          body: "有効期限つきのリンクを送る",
          localId: "i1",
          parentLocalId: "e1",
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
      },
      {
        itemId: "PVTI_2",
        kind: "issue",
        title: "メールとパスワードでログインする",
        body: "フォームと検証",
        localId: "i1",
        parentLocalId: "e1",
      },
      {
        itemId: "PVTI_3",
        kind: "issue",
        title: "ログイン失敗を数える",
        body: "連続失敗の記録",
        localId: "i2",
        parentLocalId: "e1",
      },
    ],
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
    createItems: { status: 201, body: createdRun() },
    // 既定は認証を設定していない構成。ログインの導線を見る spec だけが
    // 書き換える。
    session: { status: 200, body: { authRequired: false, authenticated: false } },
    login: { status: 200, body: { authorizeUrl: AUTHORIZE_URL } },
    repositories: { status: 200, body: repositories() },
    projects: {
      "acme/web": { status: 200, body: projects() },
      "acme/api": { status: 200, body: [] },
    },
  };
}
