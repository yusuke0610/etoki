/**
 * 失敗の `code` から、画面に出す日本語を引く層。
 *
 * **文言を持つのはここだけ。** 呼び出し側は「何をしようとしたか」だけを渡す。
 * 以前は各コンポーネントに日本語がべた書きで、同じ「取得できませんでした」が
 * 5 通りの書き方で存在していた。
 *
 * **サーバーの `error` は手掛かりであって利用者向けの文言ではない。** `etoki:
 * insufficient role` のような内部文言をそのまま出すと、利用者は次に何をすれば
 * よいか決められない。既定では畳んでおき、開けば見える形にする。
 *
 * **畳んで見せるのはサーバーが返した本文だけ。** 応答が返ってこなかったときの
 * 例外は console に固定する（`web/CLAUDE.md`）。
 */
import { log } from "../logger";
import { ApiError } from "./boards";
import type { ErrorCode } from "./types";

/**
 * code から利用者向けの文言を引く表。
 *
 * **`Record` にすることが担保。** `ErrorCode` は `api/openapi.yaml` からの
 * 生成物なので、契約に code を足して文言を書き忘れると `tsc` が落ちる
 * （`web/src/board/roles.ts` の ROLE_LABELS と同じ形）。
 *
 * 書くのは**次に何をすればよいか**。同じステータスでも打ち手が違うから code を
 * 分けたのであって、原因の言い換えだけでは分けた意味が無い。
 */
export const ERROR_MESSAGES: Record<ErrorCode, string> = {
  invalid_input: "送った内容に不備があります。",
  login_required: "ログインが必要です。ログインし直してください。",
  // 403 の 2 層は直す場所が違う（ADR 0017）。畳まずに言い分ける。
  forbidden_role: "このボードでの権限が足りません。オーナーに頼んでください。",
  forbidden_project:
    "GitHub がこの Project への書き込みを拒みました。リポジトリの権限を確かめてください。",
  cross_site_rejected: "別のサイトからの操作として拒否されました。",
  not_found: "見つかりませんでした。消されたか、権限がありません。",
  // 409 の 7 つ。打ち手が全部違うので、ここが畳まれると画面は何も案内できない。
  scene_conflict: "他の人が先に保存しました。いまの内容を控えて、開き直してください。",
  target_locked: "作成先はもう変えられません。最初の draft issue を作ったためです。",
  target_mismatch: "作成先が変わっています。ボードを開き直してください。",
  content_hash_mismatch: "解釈のあとにボードが変わりました。解釈からやり直してください。",
  previous_item_unknown:
    "更新先がこの注釈のものではありません。解釈からやり直してください。",
  already_member: "すでにメンバーです。",
  last_owner: "最後のオーナーは外せません。先に別のオーナーを立ててください。",
  // 大きさで弾かれた。**上限に届くのはほぼ貼った画像なので、そこまで言い切る。**
  // 「ボードが大きすぎます」だけでは、描いたものを減らせと読めてしまう。
  scene_too_large:
    "貼った画像が大きすぎて保存できません。画像を減らすか、小さいものに貼り替えてください。",
  target_not_selected:
    "作成先が選ばれていません。リポジトリと Project を選んでください。",
  project_field_missing: "作成先の Project に必要なフィールドがありません。",
  llm_unavailable: "LLM を呼び出せませんでした。API キーと接続を確かめてください。",
  interpretation_failed:
    "LLM の出力が期待した形になりませんでした。もう一度試してください。",
  creation_incomplete:
    "draft issue を 1 件も作れませんでした。GitHub 側の状態を確かめてください。",
  github_unavailable:
    "GitHub に問い合わせられませんでした。権限とレート制限を確かめてください。",
  internal: "etoki の内部でエラーが起きました。",
  // 設定するものが違うので 1 つに畳まない。畳むと何を設定すればよいか言えない。
  llm_not_configured: "LLM が未設定です。ETOKI_LLM_API_KEY を設定してください。",
  github_not_configured: "GitHub が未設定です。ETOKI_GITHUB_TOKEN を設定してください。",
  auth_not_configured: "認証が未設定です。GitHub App の設定が必要です。",
  sharing_not_configured: "共有には認証の設定が必要です。GitHub App を設定してください。",
};

/**
 * 応答が返ってこなかったときの文言。
 *
 * `ErrorCode` を持たない唯一の失敗なので `ERROR_MESSAGES` の外にあるが、
 * **文言を持つのはこのモジュールだけ**という約束は同じ。
 */
const NETWORK_FAILURE = "etoki に接続できませんでした。起動しているか確かめてください。";

/** 画面に出す失敗 1 件。 */
export type Failure = {
  /** 見せる文。「何をしようとしたか」と「次にどうするか」。 */
  message: string;
  /** 開いたときだけ見える手掛かり。無ければ空文字。 */
  detail: string;
};

/**
 * 失敗を画面に出せる形にする。
 *
 * `action` は操作の名前（`保存できませんでした`）。原因の文言は code から引く。
 */
export function describeFailure(action: string, e: unknown): Failure {
  if (!(e instanceof ApiError)) {
    // 応答が返ってこなかった。**例外の中身は画面に出さず console に固定する**
    // （`web/CLAUDE.md`、ADR 0027）。`TypeError: Failed to fetch` を畳んで
    // 見せても打ち手は増えないうえ、ここに来る値は投げた側しだいで何でもありうる。
    log.error(action, e);
    return { message: `${action}: ${NETWORK_FAILURE}`, detail: "" };
  }

  // 対応表は ErrorCode で網羅しているが、受け取る値は契約より新しいことがある。
  // 知らない code をここで拾えるよう、引くときだけ緩い型で見る。
  const known: string | undefined = (ERROR_MESSAGES as Partial<Record<string, string>>)[
    e.code
  ];
  if (known === undefined) {
    // サーバーのほうが新しい。畳んで隠すと何も言えなくなるので本文を出す。
    return { message: `${action}: ${e.message}`, detail: "" };
  }

  return { message: `${action}: ${known}`, detail: e.message };
}

/**
 * 保存されたシーンが読めなかった。
 *
 * `ErrorCode` を持たない。応答ではなく手元の JSON.parse が落ちた失敗で、
 * サーバーは 200 を返している。**それでも文言はここに置く**（`web/CLAUDE.md`）。
 * コンポーネントに書くと、同じ「読み込めませんでした」がまた散る。
 */
export function sceneUnreadableFailure(): Failure {
  return {
    message: "シーンを読み込めませんでした。空のボードとして開きます。",
    detail: "",
  };
}

/**
 * 作成先の Project が GitHub 側で見つからなかった（ADR 0037）。
 *
 * `ErrorCode` を持たない。GitHub も etoki も 200 を返していて、一覧に
 * 目当ての ID が無かっただけ。**それでも文言はここに置く**（`web/CLAUDE.md`）。
 * コンポーネントに書くと、同じ「見つかりません」がまた散る。
 */
export function targetProjectMissingFailure(): Failure {
  return {
    message:
      "作成先の Project が GitHub 側で見つかりませんでした。消されたか、権限が変わっています。",
    detail: "",
  };
}

/**
 * draft issue の作成が途中で止まった（ADR 0009 / 0026）。
 *
 * **`code` では表せない。** 1 件ずつ理由が違いうるので 1 つの `ErrorCode` に
 * 落ちない。`summary` は「何件までは GitHub 側に残っているか」で、数える側
 * （項目の内訳を持つのはコンポーネント）から受け取る。
 *
 * `detail` はサーバーが `SyncRun.error` で返した本文。例外ではないので畳んで
 * 残す（`web/CLAUDE.md` の「失敗の見せ方」）。
 */
export function partialCreationFailure(
  summary: string,
  detail: string | undefined,
): Failure {
  // 契約上は任意。incomplete なら必ず入るが、型に従って畳む。
  return { message: `途中で失敗しました（${summary}）`, detail: detail ?? "" };
}
