import { useCallback, useEffect, useRef, useState } from "react";

import { oauthApi } from "../api/boards";
import { describeFailure, type Failure } from "../api/errorMessage";
import type { OAuthGrant } from "../api/types";
import { ErrorNotice } from "../app/ErrorNotice";
import { clientDisplayName, clientOrigin } from "./clientLabel";

type Props = {
  /** 開いているか。 */
  open: boolean;
  /** 閉じた。焦点を戻すのは呼ぶ側。 */
  onClose: () => void;
};

/** 日時の表示。接続は数が少なく、相対表記で丸めるほどの理由が無い。 */
function formatTime(value: string): string {
  return new Date(value).toLocaleString("ja-JP", {
    dateStyle: "medium",
    timeStyle: "short",
  });
}

/**
 * 自分が MCP のクライアントに許した接続の一覧と取り消し（ADR 0076）。
 *
 * **開くたびに引き直す。** 接続はブラウザの外（MCP のクライアント）で増えるので、
 * 前に開いたときの一覧は古い。開くまでは引かない（押したときだけ引く、
 * `.claude/rules/async-ui.md`）。
 *
 * **取り消したら、引き直しを待たずに手元からも外す。** サーバーが消したと
 * 答えているので推測ではない。引き直しが失敗すると、取り消した行が残り続ける。
 */
export function ConnectionsDialog({ open, onClose }: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  const [grants, setGrants] = useState<OAuthGrant[] | null>(null);
  const [error, setError] = useState<Failure | null>(null);
  // 取り消している接続の ID。二重に押させない守りは ref に置く（同じ tick の
  // 2 回目が state の古い値を読む）。
  const revoking = useRef(new Set<string>());
  // 読み込みの世代。開き直したら前の応答は捨てる。
  const loading = useRef(0);

  const load = useCallback(async () => {
    const generation = ++loading.current;
    setGrants(null);
    setError(null);
    try {
      const got = await oauthApi.grants();
      if (generation === loading.current) setGrants(got);
    } catch (e) {
      if (generation === loading.current) {
        setError(describeFailure("接続の一覧を読めませんでした", e));
      }
    }
  }, []);

  useEffect(() => {
    const element = dialog.current;
    if (element === null) return;
    if (open && !element.open) {
      element.showModal();
      // 開くたびに引き直す。ブラウザの外で増えた接続を見せるため。
      void load();
    } else if (!open && element.open) {
      element.close();
    }
  }, [load, open]);

  const revoke = useCallback(async (grantId: string) => {
    if (revoking.current.has(grantId)) return;
    revoking.current.add(grantId);
    setError(null);
    try {
      await oauthApi.revoke(grantId);
      // **外す対象は引数の ID で決める。** 開いている行から読み直すと、遅れて
      // 返ったときに別の行を外す。
      setGrants((prev) => prev?.filter((g) => g.id !== grantId) ?? prev);
    } catch (e) {
      setError(describeFailure("接続を取り消せませんでした", e));
    } finally {
      revoking.current.delete(grantId);
    }
  }, []);

  return (
    <dialog
      ref={dialog}
      className="connections-dialog"
      aria-labelledby="connections-heading"
      onClose={onClose}
    >
      <h2 id="connections-heading">MCP の接続</h2>
      <p className="hint">
        あなたとして etoki
        のボードを読めるクライアントです。取り消すと、そのクライアントは許可からやり直しになります。
      </p>

      {/* 失敗は一覧の隣に出す。操作した場所から離すと、どの操作の結果か読めない（ADR 0058）。 */}
      {error && <ErrorNotice failure={error} />}

      {grants === null && error === null && <p className="hint">読み込んでいます…</p>}
      {grants !== null && grants.length === 0 && (
        <p className="hint">許可している接続はありません。</p>
      )}
      {grants !== null && grants.length > 0 && (
        <ul className="connections-list">
          {grants.map((grant) => {
            const name = clientDisplayName(grant.clientName);
            const origin = clientOrigin(grant.clientId);
            return (
              <li key={grant.id} className="connection">
                <div className="connection-body">
                  <p className="connection-name">{name}</p>
                  <p className="hint connection-meta">
                    {origin !== null ? `${origin} が名乗る` : "名前は自称"}
                    {` · 許可 ${formatTime(grant.createdAt)} · 最終更新 ${formatTime(grant.lastUsedAt)}`}
                  </p>
                </div>
                <button
                  type="button"
                  className="danger"
                  // 行ごとに同じ文言が並ぶので、読み上げで区別できる名前にする
                  // （`.claude/rules/async-ui.md`）。
                  aria-label={`${name} の接続を取り消す`}
                  onClick={() => void revoke(grant.id)}
                >
                  取り消す
                </button>
              </li>
            );
          })}
        </ul>
      )}

      <div className="dialog-actions">
        <button type="button" onClick={() => dialog.current?.close()}>
          閉じる
        </button>
      </div>
    </dialog>
  );
}
