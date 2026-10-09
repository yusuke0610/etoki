-- MCP のクライアントに etoki が発行する許可（ADR 0076、#185）。
--
-- 認証を設定していない構成ではどの表も空のまま。
--
-- 時刻は**固定幅**（小数 9 桁の UTC）で書く。期限の判定を文字列の大小で
-- 行うので、桁数が揃っていないと順序が時刻と食い違う。

-- 動的登録（RFC 7591）で登録したクライアント。
--
-- Client ID Metadata Document で名乗るクライアントは入らない。メタデータの
-- 正本は相手の URL にある。
--
-- granted_at は最初に許可を得た時刻。空文字は「まだ一度も得ていない」で、
-- 登録から時間がたっても空のままなら掃除する。**許可を取り消しても消さない。**
-- 消すと、手元に client_id を控えているクライアントが次の認可で知らない
-- クライアントとして断られる。
CREATE TABLE oauth_clients (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  redirect_uris TEXT NOT NULL,
  created_at    TEXT NOT NULL,
  granted_at    TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_oauth_clients_unused ON oauth_clients (granted_at, created_at);

-- 認可コード。値そのものは持たず SHA-256 だけを置く。
--
-- 引き換えたあとも期限までは残す。使い回されたコードに気づき、そこから出した
-- 許可を失効させるため（grant_id）。used_at が空文字なら未使用。
CREATE TABLE oauth_codes (
  code_hash      TEXT PRIMARY KEY,
  user_id        TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  client_id      TEXT NOT NULL,
  client_name    TEXT NOT NULL,
  scope          TEXT NOT NULL,
  resource       TEXT NOT NULL,
  redirect_uri   TEXT NOT NULL,
  code_challenge TEXT NOT NULL,
  created_at     TEXT NOT NULL,
  expires_at     TEXT NOT NULL,
  used_at        TEXT NOT NULL DEFAULT '',
  grant_id       TEXT NOT NULL DEFAULT '',

  -- 使用済みなら、どの許可に引き換えたかが要る。片方だけの行を作らせない。
  CHECK ((used_at = '' AND grant_id = '') OR (used_at <> '' AND grant_id <> ''))
);

CREATE INDEX idx_oauth_codes_expires_at ON oauth_codes (expires_at);

-- 利用者がクライアントに許した 1 件。失効はこの単位で行う。
--
-- client_id は oauth_clients を参照しない。Client ID Metadata Document の
-- クライアントは表に居ない。client_name は許可した時点の名前を控える。
CREATE TABLE oauth_grants (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  client_id    TEXT NOT NULL,
  client_name  TEXT NOT NULL,
  scope        TEXT NOT NULL,
  resource     TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  last_used_at TEXT NOT NULL
);

CREATE INDEX idx_oauth_grants_user ON oauth_grants (user_id, created_at);

-- 発行したトークン。値そのものは持たず SHA-256 だけを置く。
--
-- used_at は refresh token だけが使う。使用済みのものも期限までは残す。
-- 同じ値がもう一度来たら、漏れた値が使われたと見なして許可ごと失効させる。
CREATE TABLE oauth_tokens (
  token_hash TEXT PRIMARY KEY,
  grant_id   TEXT NOT NULL REFERENCES oauth_grants (id) ON DELETE CASCADE,
  kind       TEXT NOT NULL CHECK (kind IN ('access', 'refresh')),
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  used_at    TEXT NOT NULL DEFAULT '',

  -- アクセストークンは使用済みにならない。
  CHECK (kind = 'refresh' OR used_at = '')
);

CREATE INDEX idx_oauth_tokens_grant ON oauth_tokens (grant_id);
CREATE INDEX idx_oauth_tokens_expires_at ON oauth_tokens (expires_at);
