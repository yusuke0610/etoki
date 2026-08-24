import type { Failure } from "./api/errorMessage";

type Props = {
  failure: Failure;
  /** 閉じられる帯にするなら渡す。パネル内の表示では省く。 */
  onClose?: () => void;
};

/**
 * 失敗 1 件の見せ方を 1 箇所に集める。
 *
 * **サーバーが返した本文は既定で畳む。** `etoki: insufficient role` のような
 * 内部文言を前に出すと、読むべき文（次に何をすればよいか）が埋もれる。捨てはし
 * ない。GitHub や LLM が返した本文が実際の手掛かりになる経路があるので、開けば
 * 見える形にしておく。
 */
export function ErrorNotice({ failure, onClose }: Props) {
  return (
    <div className="error" role="alert">
      <div className="error-body">
        <p className="error-message">{failure.message}</p>
        {failure.detail !== "" && (
          <details className="error-detail">
            <summary>詳細</summary>
            <pre>{failure.detail}</pre>
          </details>
        )}
      </div>
      {onClose && (
        <button type="button" onClick={onClose}>
          閉じる
        </button>
      )}
    </div>
  );
}
