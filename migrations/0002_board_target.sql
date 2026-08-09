-- draft issue の作成先をボードごとに持つ（ADR 0014）。
--
-- 空文字は「未選択」を表す。NULL にしないのは、3 列とも「値が無い」を
-- 1 通りで表したいため。既存のボードは未選択として移行され、開いたときに
-- 選択画面に入る。
--
-- リポジトリは owner / name で持ち、node ID では持たない。リポジトリ名の
-- 変更に追随できないという難点はあるが、画面に出すのも GraphQL で引くのも
-- owner / name であり、node ID からは逆に引けない。
ALTER TABLE boards ADD COLUMN repository_owner TEXT NOT NULL DEFAULT '';
ALTER TABLE boards ADD COLUMN repository_name TEXT NOT NULL DEFAULT '';
ALTER TABLE boards ADD COLUMN project_id TEXT NOT NULL DEFAULT '';
