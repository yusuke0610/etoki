import { useCallback, useEffect, useState } from "react";

import { ApiError, membersApi } from "../api/boards";
import type { BoardMember, BoardRole } from "../api/types";

type Props = {
  boardId: string;
  /** 見ている人のロール。owner だけが招待と解除を触れる。 */
  role: BoardRole;
  onClose: () => void;
};

/** ロールの表示名。値そのものを画面に出すと、意味が伝わらない。 */
const ROLE_LABELS: Record<BoardRole, string> = {
  owner: "オーナー",
  editor: "編集できる",
  viewer: "読むだけ",
};

/**
 * ボードを誰と共有しているかを見せ、owner なら招待と解除をさせる。
 *
 * **招待される側にリポジトリのアクセス権は要らない**（ADR 0017）。ブレストに
 * 呼ぶ相手と GitHub に書ける相手は同じではない。書けるかどうかはボードの
 * ヘッダに別に出る。
 *
 * 一覧は owner でなくても見られる。誰と共有しているかを owner だけが知って
 * いる状態にすると、招待された側は自分が何に呼ばれたのか分からない。
 */
export function MemberPanel({ boardId, role, onClose }: Props) {
  const [members, setMembers] = useState<BoardMember[] | null>(null);
  const [login, setLogin] = useState("");
  const [inviteRole, setInviteRole] = useState<BoardRole>("editor");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const reload = useCallback(async () => {
    try {
      setMembers(await membersApi.list(boardId));
    } catch (e) {
      setError(describe(e, "メンバーを取得できませんでした"));
    }
  }, [boardId]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const invite = useCallback(async () => {
    if (!login.trim()) return;

    setBusy(true);
    setError(null);
    try {
      await membersApi.invite(boardId, login.trim(), inviteRole);
      setLogin("");
      await reload();
    } catch (e) {
      setError(describe(e, "招待できませんでした"));
    } finally {
      setBusy(false);
    }
  }, [boardId, inviteRole, login, reload]);

  const changeRole = useCallback(
    async (userId: string, next: BoardRole) => {
      setBusy(true);
      setError(null);
      try {
        await membersApi.setRole(boardId, userId, next);
        await reload();
      } catch (e) {
        setError(describe(e, "ロールを変更できませんでした"));
      } finally {
        setBusy(false);
      }
    },
    [boardId, reload],
  );

  const remove = useCallback(
    async (userId: string) => {
      setBusy(true);
      setError(null);
      try {
        await membersApi.remove(boardId, userId);
        await reload();
      } catch (e) {
        setError(describe(e, "メンバーを外せませんでした"));
      } finally {
        setBusy(false);
      }
    },
    [boardId, reload],
  );

  const isOwner = role === "owner";

  return (
    <section className="member-panel" aria-label="メンバー">
      <header className="member-panel-header">
        <h2>メンバー</h2>
        <button type="button" onClick={onClose}>
          閉じる
        </button>
      </header>

      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}

      {isOwner && (
        <form
          className="invite-form"
          onSubmit={(e) => {
            e.preventDefault();
            void invite();
          }}
        >
          <input
            aria-label="招待する login"
            placeholder="GitHub の login"
            value={login}
            onChange={(e) => setLogin(e.target.value)}
          />
          <select
            aria-label="招待するロール"
            value={inviteRole}
            onChange={(e) => setInviteRole(e.target.value as BoardRole)}
          >
            <option value="editor">{ROLE_LABELS.editor}</option>
            <option value="viewer">{ROLE_LABELS.viewer}</option>
            <option value="owner">{ROLE_LABELS.owner}</option>
          </select>
          <button type="submit" disabled={busy || !login.trim()}>
            招待
          </button>
          {/*
            相手が一度ログインしている必要があることは、失敗してから知らせるの
            では遅い。先に書いておく（ADR 0017）。
          */}
          <p className="hint">
            {"招待できるのは、一度 etoki にログインしたことがある人だけです。"}
          </p>
        </form>
      )}

      {members === null ? (
        <p className="hint">読み込み中…</p>
      ) : (
        <ul className="member-list">
          {members.map((m) => (
            <li key={m.userId}>
              <span className="member-name">
                {m.displayName || m.login || "(不明な利用者)"}
                {m.login && <span className="kind">@{m.login}</span>}
              </span>

              {isOwner ? (
                <>
                  <select
                    aria-label={`${m.login || m.userId} のロール`}
                    value={m.role}
                    disabled={busy}
                    onChange={(e) =>
                      void changeRole(m.userId, e.target.value as BoardRole)
                    }
                  >
                    <option value="owner">{ROLE_LABELS.owner}</option>
                    <option value="editor">{ROLE_LABELS.editor}</option>
                    <option value="viewer">{ROLE_LABELS.viewer}</option>
                  </select>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void remove(m.userId)}
                  >
                    外す
                  </button>
                </>
              ) : (
                <span className="badge">{ROLE_LABELS[m.role]}</span>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/**
 * サーバーが返した理由をそのまま見せる。
 *
 * 「最後の owner は外せない」「まだログインしていない」といった、利用者が
 * 次に何をすればよいか決められる文言がここに載る。握りつぶさない。
 */
function describe(e: unknown, fallback: string): string {
  if (e instanceof ApiError) {
    return `${fallback}: ${e.message}`;
  }
  return `${fallback}: ${String(e)}`;
}
