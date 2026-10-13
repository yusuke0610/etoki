import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";

import { ApiError, boardsApi } from "../api/boards";
import { describeFailure, type Failure } from "../api/errorMessage";
import { sceneSignature } from "../excalidraw/dirty";
import type { SceneElement } from "../excalidraw/annotation";
import { filesToSave } from "../excalidraw/files";
import { savedSceneJSON } from "../excalidraw/transfer";
import type { NotifyOptions } from "../notification/types";
import type { Exclusion } from "./exclusion";

/** 保存の失敗の通知。続けて失敗しても 1 件に畳み、保存できたら下げる。 */
export const SAVE_FAILED = "save-failed";

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
   * 開いた時点でサーバーが持っていた画像の ID（`BoardWithFiles.files` のキー）。
   *
   * 保存はこれに無い画像だけを送る（ADR 0074）。**開き直すまで読み直さない。**
   * 以後は保存の応答（`fileIds`）で差し替える。
   */
  fileIds: readonly string[];
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
  onError: (failure: Failure, options?: Pick<NotifyOptions, "action" | "key">) => void;
  /** 前の保存の失敗の通知を下げる（ADR 0058）。 */
  dismiss: (key: string) => void;
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
  /**
   * 保存する。**書けたときだけ true を返す。** 押せない（排他に弾かれた）・衝突・
   * 失敗はどれも false。保存の続きに何かを始める呼び出し側（「絵解き」、#247）が、
   * 書けていないシーンを前提に走り出さないために返す。
   */
  save: () => Promise<boolean>;
};

/** シーンを保存する（#146 で `BoardPage` から切り出し）。 */
export function useSceneSave({
  api,
  boardId,
  updatedAt,
  fileIds,
  sceneOverLimit,
  exclusive,
  currentBackground,
  markSaved,
  onSaved,
  onError,
  dismiss,
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

  // サーバーが持っている画像（ADR 0074）。**サーバーが返したものだけで決める。**
  // 送ったものから手元で導くと、何が残って何が消えるかの規則が 2 箇所になる。
  // 衝突や失敗では動かさない。書けていないので、サーバーの持ち物も変わって
  // いない。
  const heldFileIds = useRef<ReadonlySet<string>>(new Set(fileIds));

  // 作成先の変更などでボードを取り直したら基準も差し替える。据え置くと、
  // 自分の操作でずれた版のせいで以後の保存が必ず衝突する。
  useEffect(() => {
    baseUpdatedAt.current = updatedAt;
  }, [updatedAt]);

  // 通知の「再試行」から呼ぶ保存。**押された時点の save を呼ぶ。** 通知は失敗した
  // 時点で作られるので、そのときの save を閉じ込めると、あとで api が変わった
  // あとでも古いものを呼ぶ。
  const saveRef = useRef<() => Promise<boolean>>(async () => false);

  const save = useCallback(async (): Promise<boolean> => {
    if (!api) return false;
    let written = false;

    // disabled は表示の約束。取り込みや作成の最中に直接呼ばれても保存しないよう、
    // 永続化の入口でも同じ排他を確かめる（表は `exclusion.ts`）。
    const ran = await exclusive.run("saving", async () => {
      try {
        const elements = api.getSceneElements();
        // 画像は抜いて、サーバーがまだ持っていないものだけを添える（ADR 0074）。
        // 図形を動かしただけの保存で画像を送り直さない。
        const scene = savedSceneJSON(api);
        const files = filesToSave(api, heldFileIds.current);
        // 送った内容そのものを新しい基準にする。保存の待ち時間に編集されていたら
        // 未保存のまま残す必要があるので、`markSaved` が比べ直す。
        // **背景色も `scene` に載っている**ので、基準にも同じものを含める。
        const sent = sceneSignature(
          elements as unknown as SceneElement[],
          currentBackground(),
        );

        const saved = await boardsApi.saveScene(
          boardId,
          scene,
          files,
          baseUpdatedAt.current,
        );
        // 返った版が次の基準。捨てると 2 回目の保存が必ず衝突する。
        baseUpdatedAt.current = saved.updatedAt;
        // 返った画像が次の保存で送らなくてよいもの。捨てると毎回送り直す。
        heldFileIds.current = new Set(saved.fileIds);
        setConflictedFor(null);
        // 前の保存の失敗はもう当てはまらない。残すと、保存できているのに
        // 「保存できませんでした」が読める。
        dismiss(SAVE_FAILED);
        // 保存が成功した = いまのシーンはサーバーの上限を満たしている。
        setOverLimit(false);
        markSaved(sent);
        await onSaved();
        written = true;
      } catch (e) {
        // 409 は「保存に失敗した」ではなく「他の人が先に保存した」という状態。
        // こちらの編集は未保存のまま残す。捨てて読み直すと、消えるのは相手では
        // なくこちらの作業になる（ADR 0020）。
        if (e instanceof ApiError && e.code === "scene_conflict") {
          setConflictedFor(updatedAt);
          return;
        }
        // 失敗したら同じ保存をその場から押し直せるようにする。**押し直して解けない
        // ものには出さない。** 409 は上で帯に回してあり、413 は描いたものを減らす
        // まで何度押しても同じ答えが返る。key を揃えて、失敗が続いても同じ 1 件を
        // 差し替えるだけにする。
        const retryable = !(e instanceof ApiError && e.code === "scene_too_large");
        onError(describeFailure("保存できませんでした", e), {
          key: SAVE_FAILED,
          action: retryable
            ? { label: "再試行", run: () => void saveRef.current() }
            : undefined,
        });
      }
    });
    return ran && written;
  }, [
    api,
    boardId,
    currentBackground,
    dismiss,
    exclusive,
    markSaved,
    onError,
    onSaved,
    updatedAt,
  ]);

  useEffect(() => {
    saveRef.current = save;
  }, [save]);

  return useMemo(() => ({ conflicted, overLimit, save }), [conflicted, overLimit, save]);
}
