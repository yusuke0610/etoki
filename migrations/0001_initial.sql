-- ボード本体。スナップショットとバージョニングは行わないため、scene は
-- 常に最新状態のみを保持する。
CREATE TABLE boards (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  scene      TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE INDEX idx_boards_updated_at ON boards (updated_at DESC);

-- 1 つの注釈に対する 1 回分の issue 化実行。再実行しても過去の run は残す。
-- 消してしまうと、GitHub 側に残っている draft issue を追跡できなくなるため。
CREATE TABLE sync_runs (
  id                    INTEGER PRIMARY KEY AUTOINCREMENT,
  board_id              TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
  annotation_element_id TEXT NOT NULL,
  content_hash          TEXT NOT NULL,
  created_at            TEXT NOT NULL
);

-- 「最新の run」は created_at ではなく id で決める。時刻は呼び出し側が与える
-- 設計なので同一時刻の run がありえて、created_at だけでは順序が定まらない。
-- AUTOINCREMENT の id は単調増加するため一意に定まる。
CREATE INDEX idx_sync_runs_latest
ON sync_runs (board_id, annotation_element_id, id DESC);

-- 1 回の実行で作成した draft issue。
CREATE TABLE sync_items (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id          INTEGER NOT NULL REFERENCES sync_runs (id) ON DELETE CASCADE,
  item_id         TEXT NOT NULL,
  kind            TEXT NOT NULL CHECK (kind IN ('epic', 'issue')),
  title           TEXT NOT NULL,
  local_id        TEXT NOT NULL,
  parent_local_id TEXT,
  created_at      TEXT NOT NULL,

  -- 同一 run 内で local_id は一意。LLM が同じ一時 ID を重複して出力した場合に
  -- 部分的に書き込まれた run が残らないよう、DB 側でも弾く。
  UNIQUE (run_id, local_id)
);

CREATE INDEX idx_sync_items_run ON sync_items (run_id);
