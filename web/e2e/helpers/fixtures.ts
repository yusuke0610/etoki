import type {
  AnnotationStatus,
  BoardDetail,
  CreatedRun,
  Interpretation,
  Project,
  Repository,
} from "../../src/api/types";
import { emptyScene, type ApiMock } from "./api";

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
    createdAt: "2026-08-01T09:00:00Z",
    updatedAt: "2026-08-05T09:30:00Z",
    scene: emptyScene(),
    repositoryOwner: "acme",
    repositoryName: "web",
    projectId: "PVT_1",
    targetLocked: false,
  };
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
          localId: "e1",
        },
        {
          itemId: "PVTI_issue",
          kind: "issue",
          title: "再設定メールを送る",
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
      { itemId: "PVTI_1", kind: "epic", title: "ログイン基盤", localId: "e1" },
      {
        itemId: "PVTI_2",
        kind: "issue",
        title: "メールとパスワードでログインする",
        localId: "i1",
        parentLocalId: "e1",
      },
      {
        itemId: "PVTI_3",
        kind: "issue",
        title: "ログイン失敗を数える",
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
    boards: [
      {
        id: detail.id,
        name: detail.name,
        createdAt: detail.createdAt,
        updatedAt: detail.updatedAt,
      },
    ],
    details: { [detail.id]: detail },
    annotations: { [detail.id]: annotations() },
    interpret: { status: 200, body: interpretation() },
    createItems: { status: 201, body: createdRun() },
    repositories: { status: 200, body: repositories() },
    projects: {
      "acme/web": { status: 200, body: projects() },
      "acme/api": { status: 200, body: [] },
    },
  };
}
