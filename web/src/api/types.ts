/**
 * 生成物（`generated.ts`）の型を、契約上のスキーマ名のまま再エクスポートする層。
 *
 * 呼び出し側は `components["schemas"][...]` を直書きせず、ここの名前を import する。
 * 独自の別名は付けない。名前が食い違うと、契約を直したときに追随すべき箇所が
 * 機械的に辿れなくなる（ADR 0011）。型を増やすときはここに 1 行足す。
 */
import type { components } from "./generated";

type Schemas = components["schemas"];

/** 注釈の 3 状態。判定は保存済みシーンが基準。 */
export type SyncState = Schemas["SyncState"];

/** 注釈の粒度。空文字は「指定なし」で、粒度の判断を LLM に任せる。 */
export type Granularity = Schemas["Granularity"];

/** GitHub に作る draft issue の種別。epic と issue の 2 階層のみ。 */
export type ItemKind = Schemas["ItemKind"];

/** 一覧で返すボード。シーンは含まない。 */
export type BoardSummary = Schemas["BoardSummary"];

/** シーンと作成先を含むボード。 */
export type BoardDetail = Schemas["BoardDetail"];

/** draft issue の作成先。設定のリクエストボディ。 */
export type BoardTarget = Schemas["BoardTarget"];

/** シーン保存のリクエストボディ。編集の基準にした版を伴う。 */
export type SaveSceneRequest = Schemas["SaveSceneRequest"];

/** 保存後のボードの版。次の保存の基準になる。 */
export type SaveSceneResponse = Schemas["SaveSceneResponse"];

/** 作成先を選ぶときに見せるリポジトリ。 */
export type Repository = Schemas["Repository"];

/** リポジトリに紐づく Projects v2。 */
export type Project = Schemas["Project"];

/** 作成済みの draft issue 1 件。 */
export type SyncItem = Schemas["SyncItem"];

/** 注釈 1 つの状態。 */
export type AnnotationStatus = Schemas["AnnotationStatus"];

/** 解釈結果に含まれる draft issue 1 件。まだ作成はしていない。 */
export type InterpretedItem = Schemas["InterpretedItem"];

/** 注釈の frame 範囲だけを写した画像。サーバーは保存しない。 */
export type AnnotationImage = Schemas["AnnotationImage"];

/** 解釈のリクエストボディ。シーンから作れないものだけを載せる。 */
export type InterpretRequest = Schemas["InterpretRequest"];

/**
 * LLM が注釈をどう解釈したか。
 *
 * 作成のリクエストボディでもある。開発者が確認した結果をそのまま送り返す。
 */
export type Interpretation = Schemas["Interpretation"];

/** 作成した run。途中で失敗しても、作れたぶんは items に入る。 */
export type CreatedRun = Schemas["CreatedRun"];

/** 失敗したときのレスポンスボディ。 */
export type ErrorResponse = Schemas["ErrorResponse"];

/** 失敗の原因を表す符号。画面はこれで打ち手を分ける。 */
export type ErrorCode = Schemas["ErrorCode"];

/** いま使える機能。プロセスの設定であって、利用者ごとの権限ではない。 */
export type Capabilities = Schemas["Capabilities"];

/** ログイン状態。認証を設定していない構成でも返る。 */
export type SessionStatus = Schemas["SessionStatus"];

/** ログイン中の利用者。 */
export type AuthUser = Schemas["AuthUser"];

/** 認可画面へ送り出すための URL。 */
export type LoginResponse = Schemas["LoginResponse"];

/** ボードに対する権限の強さ。owner / editor / viewer の 3 つ。 */
export type BoardRole = Schemas["BoardRole"];

/** ボードのメンバー 1 人。 */
export type BoardMember = Schemas["BoardMember"];

/** 招待のリクエストボディ。 */
export type InviteMemberRequest = Schemas["InviteMemberRequest"];

/** ロール変更のリクエストボディ。 */
export type SetMemberRoleRequest = Schemas["SetMemberRoleRequest"];

/** 作成先の Project に書けるかどうかの、いまの状態。判定ではない。 */
export type ProjectAccess = Schemas["ProjectAccess"];

/** そのボードで何ができるか。etoki 側と GitHub 側を別々に持つ。 */
export type BoardAccess = Schemas["BoardAccess"];
