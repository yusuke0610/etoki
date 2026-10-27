import { useEffect, useState } from "react";

import { boardsApi } from "../api/boards";
import type { ProjectAccess } from "../api/types";

/**
 * 作成先の Project に書けるかどうか（#352 で `BoardPage` から切り出し）。
 * 確かめるまでは unknown。
 *
 * **表示用で、判定には使わない**（ADR 0017）。実際の可否は作成時に GitHub が
 * 返したものが正しい。
 *
 * ボードの取得とは別に訊く。GitHub が未設定・不通でもボードは開ける必要がある
 * ため（ADR 0017）。
 */
export function useProjectAccess(boardId: string): ProjectAccess {
  const [projectAccess, setProjectAccess] = useState<ProjectAccess>("unknown");

  useEffect(() => {
    let cancelled = false;

    void (async () => {
      try {
        const access = await boardsApi.access(boardId);
        if (!cancelled) setProjectAccess(access.projectAccess);
      } catch {
        // 確かめられなかったことをエラーにしない。GitHub が落ちているだけで
        // ボードが開けなくなる理由が無い。unknown のままにする。
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [boardId]);

  return projectAccess;
}
