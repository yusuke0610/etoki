import { useEffect, useState } from "react";

import { boardsApi } from "../api/boards";
import type { ProjectAccess } from "../api/types";

/** どのボードの、どの作成先について確かめた結果か。 */
type Checked = {
  boardId: string;
  projectId: string;
  access: ProjectAccess;
};

/**
 * 作成先の Project に書けるかどうか（#352 で `BoardPage` から切り出し）。
 * 確かめるまでは unknown。
 *
 * **表示用で、判定には使わない**（ADR 0017）。実際の可否は作成時に GitHub が
 * 返したものが正しい。
 *
 * ボードの取得とは別に訊く。GitHub が未設定・不通でもボードは開ける必要がある
 * ため（ADR 0017）。
 *
 * **作成先が変わったら訊き直す**（ADR 0100）。作成先はボードを開いたまま選ぶ
 * ので、開いたときに 1 度訊くだけだと、選ぶ前の答え（未選択なら unknown）が
 * 残る。訊き直しが届くまでは、前の作成先の答えを出さずに unknown に戻す。
 *
 * **前と同じ作成先に戻しても、前の答えを出さない。** 答えを作成先の ID で照らす
 * だけだと、`PVT_1` → `PVT_2` → `PVT_1` と戻したときに古い `PVT_1` の答えが
 * 照合をまた通る。訊き直しが失敗すると、その古い答えが読み込み直すまで残る
 * （#481 の full review）。
 */
export function useProjectAccess(boardId: string, projectId: string): ProjectAccess {
  const [checked, setChecked] = useState<Checked | null>(null);

  // 作成先が変わったら、前の答えを捨てる。**描画の中で捨てる**（props の変化に
  // 合わせて state を直す React の形）。effect で捨てると、捨てるための描き直しが
  // 1 回余計に走る。
  const [askedFor, setAskedFor] = useState({ boardId, projectId });
  if (askedFor.boardId !== boardId || askedFor.projectId !== projectId) {
    setAskedFor({ boardId, projectId });
    setChecked(null);
  }

  useEffect(() => {
    let cancelled = false;

    void (async () => {
      try {
        const access = await boardsApi.access(boardId);
        if (!cancelled) setChecked({ boardId, projectId, access: access.projectAccess });
      } catch {
        // 確かめられなかったことをエラーにしない。GitHub が落ちているだけで
        // ボードが開けなくなる理由が無い。unknown のままにする。
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [boardId, projectId]);

  // 答えは「どの作成先について確かめたか」と一緒に持ち、いまの作成先と照らして
  // 出す。上で捨てた描き直しより前（作成先が変わった描画そのもの）でも、前の
  // 作成先の答えを出さない。
  return checked !== null &&
    checked.boardId === boardId &&
    checked.projectId === projectId
    ? checked.access
    : "unknown";
}
