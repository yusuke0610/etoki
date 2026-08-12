-- ボードのメンバーとロール（ADR 0017）。
--
-- 所有者の単一情報源をここに移す。boards.owner_user_id と 2 箇所に持つと、
-- 片方だけ更新される経路が必ずできる。
--
-- user_id は ADR 0016 のまま「空文字は無効値ではなく認証なしの所有者 1 人」。
-- 認証を設定していない構成では、作られるボードの owner 行が全部その 1 人に
-- なり、これまでどおり全部が見える。
--
-- users への外部キーは張らない。利用者を消したときにボードを道連れにせず、
-- 所有者の無いボードとして残したいため（0004 と同じ理由）。
--
-- 招待した人は記録しない。監査は「誰が何をしたか」をリクエストログに載せる
-- 方針で足りている（ADR 0016）。しかも invited_by を置くと、空文字が
-- 「招待ではなく自分で作った」なのか「認証なしの所有者が招待した」なのか
-- 区別できない列になる。
CREATE TABLE board_members (
  board_id   TEXT NOT NULL REFERENCES boards (id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL,
  role       TEXT NOT NULL CHECK (role IN ('owner', 'editor', 'viewer')),
  created_at TEXT NOT NULL,

  PRIMARY KEY (board_id, user_id)
);

-- 一覧は「その利用者がメンバーであるボード」を引く。主キーは board_id が
-- 先頭なので、この向きには効かない。
CREATE INDEX idx_board_members_user ON board_members (user_id, board_id);

-- 既存のボードの所有者をそのまま owner のメンバーにする。
INSERT INTO board_members (board_id, user_id, role, created_at)
SELECT id, owner_user_id, 'owner', created_at FROM boards;

-- 列に張った索引は先に落とす。残っていると DROP COLUMN が通らない。
DROP INDEX idx_boards_owner_updated_at;

ALTER TABLE boards DROP COLUMN owner_user_id;
