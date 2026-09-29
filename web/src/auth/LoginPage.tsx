import { useCallback, useState } from "react";

import { authApi } from "../api/boards";
import { describeFailure, type Failure } from "../api/errorMessage";
import { ErrorNotice } from "../ErrorNotice";

/**
 * ログインを促す画面。
 *
 * 遷移そのものはここで行う。サーバーからリダイレクトさせないのは、fetch での
 * リダイレクト追跡が cross-origin で扱いにくいため。サーバーは URL を返すだけ
 * にしてある（ADR 0015）。
 */
export function LoginPage() {
  const [error, setError] = useState<Failure | null>(null);
  const [starting, setStarting] = useState(false);

  const start = useCallback(async () => {
    setStarting(true);
    setError(null);
    try {
      // **いま見えている場所を戻り先として渡す**（ADR 0059）。セッションが
      // 切れてここへ落ちた人は、ボードの URL を開いたまま立っている。渡さないと
      // ログインし直した先が常に `/` になり、作業していたボードを探し直す。
      //
      // **戻り先を持つのはサーバー**（state と一緒）。ここから渡すのは 1 度きりで、
      // 認可の往復の URL には載らない。自オリジン以外は 400 で弾かれる。
      const { authorizeUrl } = await authApi.start(
        window.location.pathname + window.location.search,
      );
      window.location.assign(authorizeUrl);
    } catch (e) {
      setError(describeFailure("ログインを開始できませんでした", e));
      setStarting(false);
    }
    // 成功したら遷移するので starting は戻さない。戻すとボタンが一瞬
    // 押せる状態に見え、二重に押せてしまう。
  }, []);

  return (
    <div className="login">
      <div className="login-card">
        <h1 className="login-mark">etoki</h1>

        <p className="login-lead">
          {"ホワイトボードのブレストを、GitHub の draft issue に変えます。"}
        </p>

        {/*
          何をする道具なのかを 1 目で出す。**説明の代わりではなく、文の隣に置く。**
          絵だけで意味が伝わる保証は無いので、読み上げからは外す。
        */}
        <svg
          className="login-art"
          viewBox="0 0 320 84"
          aria-hidden="true"
          focusable="false"
        >
          <rect className="art-line" x="1" y="1" width="132" height="82" rx="8" />
          <rect className="art-shape" x="14" y="16" width="34" height="22" rx="4" />
          <rect className="art-shape" x="56" y="24" width="30" height="20" rx="4" />
          <rect className="art-shape" x="26" y="48" width="42" height="20" rx="4" />
          <rect className="art-frame" x="8" y="10" width="84" height="64" rx="6" />
          <path className="art-line" d="M150 42h28" />
          <path className="art-line" d="M173 37l6 5-6 5" />
          <rect className="art-line" x="195" y="1" width="124" height="82" rx="8" />
          <circle className="art-done" cx="211" cy="22" r="5" />
          <path className="art-text" d="M223 19h72M223 26h48" />
          <circle className="art-done" cx="211" cy="42" r="5" />
          <path className="art-text" d="M223 39h62M223 46h56" />
          <circle className="art-text" cx="211" cy="62" r="5" />
          <path className="art-text" d="M223 59h54M223 66h38" />
        </svg>

        {error && <ErrorNotice failure={error} />}

        <button
          type="button"
          className="primary login-start"
          disabled={starting}
          onClick={() => void start()}
        >
          {/* マークは飾り。押すものの名前は文字が持つ（読み上げには文字だけ届く）。 */}
          <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">
            <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
          </svg>
          {starting ? "GitHub へ移動中…" : "GitHub でログイン"}
        </button>

        <p className="hint login-note">
          {
            "ログインすると、あなたの権限でリポジトリと Projects v2 を読み書きします。見えるのは etoki をインストールしたリポジトリだけです。"
          }
        </p>
      </div>
    </div>
  );
}
