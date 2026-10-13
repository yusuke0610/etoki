import { useCallback, useEffect, useRef, useState } from "react";

import { oauthApi } from "../api/boards";
import { describeFailure, type Failure } from "../api/errorMessage";
import type { OAuthAuthorization } from "../api/types";
import { ErrorNotice } from "../app/ErrorNotice";
import { clientDisplayName, clientOrigin } from "./clientLabel";

type Props = {
  /** `/oauth/authorize` から運ばれてきた要求（クエリ文字列のまま）。 */
  request: string;
  /** 要求を受けられなかったときに、ボードの一覧へ戻る。 */
  onLeave: () => void;
};

/** いまできることの説明。scope は `read` の 1 つだけ（ADR 0076）。 */
const READ_SCOPE_LABEL =
  "ボードの一覧と、注釈の状態・GitHub に作った draft issue・実行の履歴を読みます。解釈や作成はしません。";

/**
 * MCP のクライアントに、ボードを読むことを許すかを訊く画面（ADR 0076）。
 *
 * **毎回訊く。** 登録は誰でもできるので、一度許したことを理由に自動で許すと、
 * ログインしている人の許可を別のページから取られうる（中核思想 3）。
 *
 * **名前はクライアントの自称であることを隠さない。** etoki は名前を確かめて
 * いない。URL で名乗ったクライアントだけは出どころのドメインを出せる。
 *
 * 返事を送ったら、サーバーが返した戻り先（クライアントの待ち受け）へ遷移する。
 * etoki の画面には戻らない。
 */
export function ConsentPage({ request, onLeave }: Props) {
  const [view, setView] = useState<OAuthAuthorization | null>(null);
  const [error, setError] = useState<Failure | null>(null);
  const [sending, setSending] = useState(false);
  // 二重に押させない。state だと同じ tick の 2 回目がまだ false を読む
  // （`.claude/rules/async-ui.md`）。
  const sendingRef = useRef(false);

  useEffect(() => {
    let alive = true;
    void (async () => {
      try {
        const got = await oauthApi.authorization(request);
        if (alive) setView(got);
      } catch (e) {
        if (alive) setError(describeFailure("接続の要求を読めませんでした", e));
      }
    })();
    return () => {
      alive = false;
    };
  }, [request]);

  const decide = useCallback(
    async (approve: boolean) => {
      if (sendingRef.current) return;
      sendingRef.current = true;
      setSending(true);
      setError(null);
      try {
        const { redirectTo } = await oauthApi.decide(request, approve);
        window.location.assign(redirectTo);
        // 遷移するので sending は戻さない。戻すとボタンが一瞬押せる状態に見える
        // （LoginPage と同じ）。
      } catch (e) {
        setError(
          describeFailure(
            approve ? "接続を許可できませんでした" : "接続を断れませんでした",
            e,
          ),
        );
        sendingRef.current = false;
        setSending(false);
      }
    },
    [request],
  );

  const origin =
    view === null || !view.clientIdIsUrl ? null : clientOrigin(view.clientId);
  const name = view === null ? "" : clientDisplayName(view.clientName);

  return (
    <div className="consent">
      <div className="consent-card">
        {/*
          何の画面かを一目で分からせる。この画面はクライアントのブラウザ操作から
          いきなり開くので、etoki の画面だと読めないと、許可してよいかを考える
          前に戸惑う。**ロゴは仮。** 絵は飾りなので読み上げから外し、名前は文字が持つ。
        */}
        <p className="consent-brand">
          <svg
            className="consent-logo"
            viewBox="0 0 32 32"
            aria-hidden="true"
            focusable="false"
          >
            <rect className="consent-logo-bg" width="32" height="32" rx="8" />
            <rect
              className="consent-logo-frame"
              x="6"
              y="7"
              width="20"
              height="18"
              rx="3"
            />
            <rect
              className="consent-logo-note"
              x="10"
              y="11"
              width="6"
              height="5"
              rx="1"
            />
            <rect
              className="consent-logo-note"
              x="17"
              y="16"
              width="6"
              height="5"
              rx="1"
            />
          </svg>
          <span className="consent-wordmark">etoki</span>
        </p>

        <h1 className="consent-heading">MCP のクライアントからの接続</h1>

        {error && <ErrorNotice failure={error} />}

        {view === null && error === null && (
          <p className="hint">要求を読み込んでいます…</p>
        )}

        {view === null && error !== null && (
          <div className="dialog-actions">
            <button type="button" onClick={onLeave}>
              ボード一覧へ
            </button>
          </div>
        )}

        {view !== null && (
          <>
            <p className="consent-lead">
              <strong className="consent-client">{name}</strong>
              {" が、あなたとして etoki に接続しようとしています。"}
            </p>

            <dl className="consent-facts">
              <dt>できること</dt>
              <dd>{READ_SCOPE_LABEL}</dd>
              <dt>名乗り</dt>
              <dd>
                {origin !== null
                  ? `${origin} が名乗っています。`
                  : "名前はクライアントが名乗ったもので、etoki は確かめていません。"}
              </dd>
              <dt>許可したあとの戻り先</dt>
              <dd>
                <code>{view.redirectUri}</code>
              </dd>
            </dl>

            <p className="hint">
              自分で始めた接続でなければ許可しないでください。許可した接続は、ボード一覧の利用者のメニューにある「MCP
              の接続」からいつでも取り消せます。
            </p>

            <div className="dialog-actions">
              {/* 断るのは控えめな操作（#203）。主となる操作は「許可する」の 1 つ。 */}
              {/*
                送っているあいだも押せないようにはしない。押せない理由を出す間が
                無いので、二重に押させない守りは ref の側に置く（ADR 0039）。
              */}
              <button type="button" className="quiet" onClick={() => void decide(false)}>
                許可しない
              </button>
              <button type="button" className="primary" onClick={() => void decide(true)}>
                {sending ? "送っています…" : "許可する"}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
