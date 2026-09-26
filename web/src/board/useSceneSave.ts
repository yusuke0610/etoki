import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";

import { ApiError, boardsApi } from "../api/boards";
import { describeFailure, type Failure } from "../api/errorMessage";
import { sceneSignature } from "../excalidraw/dirty";
import type { SceneElement } from "../excalidraw/annotation";
import { sceneJSON } from "../excalidraw/transfer";
import type { Exclusion } from "./exclusion";

type Options = {
  api: ExcalidrawImperativeAPI | null;
  boardId: string;
  /**
   * 保存の基準にする版。「サーバーが持っているシーンはどれか」（ADR 0020）。
   *
   * 作成先の変更などでボードを取り直したら差し替わる。据え置くと、自分の
   * 操作でずれた版のせいで以後の保存が必ず衝突する。
   */
  updatedAt: string;
  /**
   * 開いた時点で、保存済みシーンが上限を超えているとサーバーが判定していたか
   * （ADR 0048、issue #103）。
   */
  sceneOverLimit: boolean;
  exclusive: Exclusion;
  /** いまキャンバスに出ている背景色。署名に入れる（ADR 0045）。 */
  currentBackground: () => string | undefined;
  /** 送った署名を新しい基準にする（`useDirtyScene`）。 */
  markSaved: (signature: string) => void;
  /**
   * 保存が済んだあとに、保存済みシーンを前提にしていたものを捨てる。
   *
   * **何を捨てるかの一覧は `BoardPage` の 1 箇所にある。** ここに書き写すと、
   * 捨てるものを足した日に無効にし忘れる場所が増える（#146）。
   */
  onSaved: () => Promise<void>;
  onError: (failure: Failure) => void;
};

export type SceneSave = {
  /** 他の人が先に保存していて、こちらの保存を拒まれた状態（ADR 0020）。 */
  conflicted: boolean;
  /**
   * 保存済みシーンが上限を超えていて、このままでは保存し直せない状態。
   *
   * **保存が成功したら手元で false に倒す。** 保存が成功した = サーバーの
   * 上限を満たした、という事実からそう言える。上限の数値をフロントが持って
   * いなくても、判定結果だけを追随させられる（ADR 0038 は数値の複製を禁じて
   * いるのであって、この推論を禁じてはいない）。
   */
  overLimit: boolean;
  save: () => Promise<void>;
};

/** シーンを保存する（#146 で `BoardPage` から切り出し）。 */
export function useSceneSave({
  api,
  boardId,
  updatedAt,
  sceneOverLimit,
  exclusive,
  currentBackground,
  markSaved,
  onSaved,
  onError,
}: Options): SceneSave {
  /**
   * 衝突したときに基準だった版。衝突していなければ null。
   *
   * **真偽値では持たない。** ボードを取り直したら知らせを下ろす必要があるが、
   * それを effect の中の `setState(false)` で行うと、余計な再描画を 1 回挟む
   * （`react-hooks/set-state-in-effect`）。どの版に対する衝突かを持てば、
   * 版が変わった時点で自動的に一致しなくなる。
   */
  const [conflictedFor, setConflictedFor] = useState<string | null>(null);
  const conflicted = conflictedFor !== null && conflictedFor === updatedAt;

  const [overLimit, setOverLimit] = useState(sceneOverLimit);

  // 署名が「何を描いたか」を持つのに対して、こちらは「何の上に描いたか」。
  const baseUpdatedAt = useRef(updatedAt);

  // 作成先の変更などでボードを取り直したら基準も差し替える。据え置くと、
  // 自分の操作でずれた版のせいで以後の保存が必ず衝突する。
  useEffect(() => {
    baseUpdatedAt.current = updatedAt;
  }, [updatedAt]);

  const save = useCallback(async () => {
    if (!api) return;

    // disabled は表示の約束。取り込みや作成の最中に直接呼ばれても保存しないよう、
    // 永続化の入口でも同じ排他を確かめる（表は `exclusion.ts`）。
    await exclusive.run("saving", async () => {
      try {
        const elements = api.getSceneElements();
        const scene = sceneJSON(api);
        // 送った内容そのものを新しい基準にする。保存の待ち時間に編集されていたら
        // 未保存のまま残す必要があるので、`markSaved` が比べ直す。
        // **背景色も `scene` に載っている**ので、基準にも同じものを含める。
        const sent = sceneSignature(
          elements as unknown as SceneElement[],
          currentBackground(),
        );

        const saved = await boardsApi.saveScene(boardId, scene, baseUpdatedAt.current);
        // 返った版が次の基準。捨てると 2 回目の保存が必ず衝突する。
        baseUpdatedAt.current = saved.updatedAt;
        setConflictedFor(null);
        // 保存が成功した = いまのシーンはサーバーの上限を満たしている。
        setOverLimit(false);
        markSaved(sent);
        await onSaved();
      } catch (e) {
        // 409 は「保存に失敗した」ではなく「他の人が先に保存した」という状態。
        // こちらの編集は未保存のまま残す。捨てて読み直すと、消えるのは相手では
        // なくこちらの作業になる（ADR 0020）。
        if (e instanceof ApiError && e.code === "scene_conflict") {
          setConflictedFor(updatedAt);
          return;
        }
        onError(describeFailure("保存できませんでした", e));
      }
    });
  }, [
    api,
    boardId,
    currentBackground,
    exclusive,
    markSaved,
    onError,
    onSaved,
    updatedAt,
  ]);

  return useMemo(() => ({ conflicted, overLimit, save }), [conflicted, overLimit, save]);
}
