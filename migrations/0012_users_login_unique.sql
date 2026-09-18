-- login を最後にログインした 1 人にだけ持たせる（ADR 0053、#142）。
--
-- users の一意制約は (provider, subject) だけで、login は本人がログインした
-- ときにしか書き換わらない。改名で空いた login を別人が取ると、同じ login の
-- 行が 2 つ並び、招待と claim がどちらを引くかは SQLite 次第になっていた。
-- 以前の持ち主に権限が渡りうる。
--
-- **行は消さない。** ボードの所有者とメンバーが id で指している。login だけを
-- 外し（空文字）、本人が入り直せば新しい login が書かれる。
--
-- 残すのは (provider, login を大文字小文字を無視して比べた組) ごとに
-- updated_at（最後にログインした時刻）が最も新しい行。同時刻なら id で決める。
-- 時刻は julianday で比べる。RFC3339Nano は末尾の 0 を落とすので、文字列の
-- まま比べると同じ秒の中で前後が入れ替わる。julianday が見るのはミリ秒まで
-- なので、1 ミリ秒の中の前後は id で決まる。同じ login を別の 2 人が 1 ミリ秒
-- 以内にログインで取り合うことは、改名を GitHub 側で挟む以上起きない。
-- GitHub の login は ASCII なので NOCASE の比較で足りる。
UPDATE users
SET login = ''
WHERE login <> ''
  AND EXISTS (
    SELECT 1 FROM users AS newer
    WHERE newer.provider = users.provider
      AND newer.login = users.login COLLATE NOCASE
      AND newer.id <> users.id
      AND (
        julianday(newer.updated_at) > julianday(users.updated_at)
        OR (julianday(newer.updated_at) = julianday(users.updated_at) AND newer.id > users.id)
      )
  );

-- 以後は DB でも作らせない。外した login（空文字）は何行あってもよい。
CREATE UNIQUE INDEX idx_users_provider_login
  ON users (provider, login COLLATE NOCASE)
  WHERE login <> '';
