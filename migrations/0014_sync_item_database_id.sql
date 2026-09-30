-- ProjectV2Item の数値の識別子（GraphQL の fullDatabaseId）を控える（ADR 0057）。
--
-- draft issue には固有の URL が無い。Project の URL に `?pane=issue&itemId=` で
-- これを添えると、その item のペインを開かせられる。node ID（item_id）は URL に
-- 使えないので、別に持つ。
--
-- 0 は「知らない」。この列を足す前の run と、GitHub が値を返さなかった run。
-- NULL にしないのは、読む側が「知らない」を 1 通りで扱えるようにするため。
-- 移行前の item は、後の run で更新したときに埋まる（畳み込みは MAX を採る。
-- internal/adapter/sqlite/mapping.go の foldedItemsQuery）。
--
-- 負の値は識別子として意味を持たないので CHECK で弾く。
ALTER TABLE sync_items ADD COLUMN item_database_id INTEGER NOT NULL DEFAULT 0
  CHECK (item_database_id >= 0);
