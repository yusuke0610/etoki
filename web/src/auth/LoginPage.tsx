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
      const { authorizeUrl } = await authApi.start();
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
      <h1>etoki</h1>

      <p className="hint">
        {
          "GitHub にログインすると、あなたの権限でリポジトリと Projects v2 を読み書きします。"
        }
      </p>
      <p className="hint">{"見えるのは etoki をインストールしたリポジトリだけです。"}</p>

      {error && <ErrorNotice failure={error} />}

      <button type="button" disabled={starting} onClick={() => void start()}>
        {starting ? "GitHub へ移動中…" : "GitHub でログイン"}
      </button>
    </div>
  );
}
