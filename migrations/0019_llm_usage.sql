-- LLM に払った量を UTC の日ごとに積む（ADR 0102、#467）。
--
-- 全員の合計の費用の上限（日・月）を見るための数え。**誰が使ったかは持たない。**
-- 利用者ごとの台帳でも、金額でもない（ADR 0031 を改めたのはここまで）。再起動や
-- アイドルの停止で消えないよう、メモリではなくここに置く。
--
-- day は UTC の日付（YYYY-MM-DD）。月の合計を文字列の範囲で引くので、文字列の
-- 並びが日付の並びと一致する固定幅にしておく。
CREATE TABLE llm_usage_days (
  day           TEXT PRIMARY KEY
                CHECK (day GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
  executions    INTEGER NOT NULL DEFAULT 0 CHECK (executions >= 0),
  input_tokens  INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
  output_tokens INTEGER NOT NULL DEFAULT 0 CHECK (output_tokens >= 0)
);
