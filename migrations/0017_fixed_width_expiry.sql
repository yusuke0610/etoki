-- sessions と oauth_states の期限を、小数 9 桁の固定幅に揃える（#274）。
--
-- 期限は SQL で文字列の大小を比べて判定する。以前は末尾の 0 を落とす
-- RFC3339Nano で書いていたので、`…:00Z` が `…:00.300000000Z` より後に並び、
-- 期限を 1 秒未満過ぎた行が通った。書き込みと比べる値は固定幅に変えたので、
-- 残っている旧い行の桁を足す。値（時刻）は変えない。
--
-- 比べるたびに旧い形を補う式を問い合わせに置く形は採らない。一度きりの移行の
-- ために、恒久的な分岐を問い合わせに抱えることになる。
--
-- 旧い行はすべて UTC で `Z` で終わる（formatTime が UTC に揃えて書いていた）。
-- 固定幅の長さは "2006-01-02T15:04:05.000000000Z" の 30 文字。
UPDATE sessions SET expires_at = CASE
  WHEN instr(expires_at, '.') = 0
    THEN substr(expires_at, 1, length(expires_at) - 1) || '.000000000Z'
  ELSE substr(expires_at, 1, instr(expires_at, '.'))
    || substr(
      substr(expires_at, instr(expires_at, '.') + 1,
        length(expires_at) - instr(expires_at, '.') - 1) || '000000000',
      1, 9)
    || 'Z'
END
WHERE length(expires_at) <> 30;

UPDATE oauth_states SET expires_at = CASE
  WHEN instr(expires_at, '.') = 0
    THEN substr(expires_at, 1, length(expires_at) - 1) || '.000000000Z'
  ELSE substr(expires_at, 1, instr(expires_at, '.'))
    || substr(
      substr(expires_at, instr(expires_at, '.') + 1,
        length(expires_at) - instr(expires_at, '.') - 1) || '000000000',
      1, 9)
    || 'Z'
END
WHERE length(expires_at) <> 30;
