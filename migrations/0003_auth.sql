-- 認証まわりの状態（ADR 0015）。
--
-- 認証を設定していない構成ではどの表も空のまま。既存の DB はマイグレーション
-- だけ当たって、挙動は変わらない。

-- 認証基盤が返した利用者。
--
-- login と display_name は変わりうるので同定には使わない。(provider, subject)
-- の組が不変の鍵になる。
CREATE TABLE users (
  id           TEXT PRIMARY KEY,
  provider     TEXT NOT NULL,
  subject      TEXT NOT NULL,
  login        TEXT NOT NULL,
  display_name TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  updated_at   TEXT NOT NULL,

  UNIQUE (provider, subject)
);

-- 1 つのログイン。
--
-- cookie に載せた値そのものは保存しない。DB が漏れても生きたセッションに
-- ならないよう、SHA-256 だけを持つ。
CREATE TABLE sessions (
  token_hash TEXT PRIMARY KEY,
  user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);

-- GitHub を叩くための資格情報。
--
-- access_token と refresh_token は暗号化して入れる。ETOKI_TOKEN_ENCRYPTION_KEY
-- が無ければ起動時に落とすので、平文が入ることはない。
--
-- 失効しない構成（App 側で「Expire user authorization tokens」を切った場合）
-- では refresh_token が空文字、期限は空文字になる。
CREATE TABLE github_tokens (
  user_id            TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
  access_token       TEXT NOT NULL,
  refresh_token      TEXT NOT NULL,
  expires_at         TEXT NOT NULL,
  refresh_expires_at TEXT NOT NULL,
  updated_at         TEXT NOT NULL
);

-- OAuth の state。単回使用で、照合したら消す。
--
-- プロセス内メモリではなく DB に置く。make dev の air は保存のたびに再起動する
-- ため、メモリだとログインの途中で state が消え、理由の分からない失敗になる。
CREATE TABLE oauth_states (
  state      TEXT PRIMARY KEY,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE INDEX idx_oauth_states_expires_at ON oauth_states (expires_at);
