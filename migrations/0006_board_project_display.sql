-- 作成先 Project の表示名を、選んだ時点のスナップショットとして持つ（ADR 0019）。
--
-- project_id は `PVT_kwDO...` という不透明な node ID で、人には読めない。
-- 一覧をリポジトリと Project でまとめて見せるには名前が要るが、表示のたびに
-- GitHub へ問い合わせると、GitHub が不通のときに一覧そのものが描けなくなる。
--
-- 0 と空文字は「名前を知らない」を表す。移行前のボードと、番号や名前を送らずに
-- API を直接叩いて設定した作成先が該当する。**判定には使わない。** 作成先が
-- 選ばれているかどうかも、固定済みかどうかも、この 2 列とは無関係に決まる。
ALTER TABLE boards ADD COLUMN project_number INTEGER NOT NULL DEFAULT 0;
ALTER TABLE boards ADD COLUMN project_title TEXT NOT NULL DEFAULT '';
