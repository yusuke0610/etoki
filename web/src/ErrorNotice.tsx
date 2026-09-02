import type { Failure } from "./api/errorMessage";

type Props = {
  failure: Failure;
  /** 閉じられる帯にするなら渡す。パネル内の表示では省く。 */
  onClose?: () => void;
  /**
   * いま起きた失敗として読み上げるか。既定は読み上げる。
   *
   * **過ぎた失敗の記録には渡さない。** 実行の履歴は過去の run を並べたもので、
   * 開いた瞬間に「いま起きた」として割り込ませる中身ではない。見た目は同じで
   * よいので、分けるのは role だけにする。
   */
  live?: boolean;
};

/**
 * 失敗 1 件の見せ方を 1 箇所に集める。
 *
 * **サーバーが返した本文は既定で畳む。** `etoki: insufficient role` のような
 * 内部文言を前に出すと、読むべき文（次に何をすればよいか）が埋もれる。捨てはし
 * ない。GitHub や LLM が返した本文が実際の手掛かりになる経路があるので、開けば
 * 見える形にしておく。
 */
export function ErrorNotice({ failure, onClose, live = true }: Props) {
  return (
    <div className="error" role={live ? "alert" : undefined}>
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
