import { useCallback, useMemo, useState } from "react";

import { boardsApi, githubApi } from "../api/boards";
import {
  describeFailure,
  targetProjectMissingFailure,
  type Failure,
} from "../api/errorMessage";
import type { BoardDeletion, BoardDetail } from "../api/types";

/**
 * ボードそのものの管理（改名・削除・作成先の名前の取り直し）。#146 で
 * `BoardPage` から切り出し。
 *
 * どれもキャンバスとは別の操作で、互いにも関わらない。**フックを 1 つに
 * まとめない。** まとめると、改名しかしない呼び出しにも削除の state が付いて回る。
 */

export type Rename = {
  /** 編集中の下書き。null なら編集していない。 */
  draft: string | null;
  setDraft: (draft: string | null) => void;
  renaming: boolean;
  rename: () => Promise<void>;
};

/**
 * 名前を変える。
 *
 * 画面の見出しをその場で書き換えるだけの操作なので、キャンバスは外れない。
 */
export function useRename({
  board,
  onRenamed,
  onError,
}: {
  board: BoardDetail;
  /** 名前を変えたので、手元のボードを差し替えてもらう。 */
  onRenamed: (board: BoardDetail) => void;
  onError: (failure: Failure) => void;
}): Rename {
  // **開いているあいだだけ入力を出す。** 常に入力欄にすると、見出しとして
  // 読むところが編集欄になり、押し間違いで名前が変わる。
  const [draft, setDraft] = useState<string | null>(null);
  const [renaming, setRenaming] = useState(false);

  // 空白だけの名前はサーバーが弾くが、押させないほうが早いのでここでも止める
  // （**判定を持つのではなく、押せない理由を見せる側**）。
  const rename = useCallback(async () => {
    if (draft === null) return;

    const next = draft.trim();
    if (next === "" || next === board.name) {
      setDraft(null);
      return;
    }

    setRenaming(true);
    try {
      onRenamed(await boardsApi.rename(board.id, next));
      setDraft(null);
    } catch (e) {
      // 編集中の下書きは残す。閉じると、入力し直しからやり直しになる。
      onError(describeFailure("名前を変更できませんでした", e));
    } finally {
      setRenaming(false);
    }
  }, [board.id, board.name, draft, onError, onRenamed]);

  return useMemo(
    () => ({ draft, setDraft, renaming, rename }),
    [draft, renaming, rename],
  );
}

/**
 * 削除の確認がいまどこにいるか（ADR 0042）。
 *
 * **`losing` を持たない状態と持つ状態を型で分ける。** 件数が無いまま確認を
 * 出せる形にすると、何を失うのかを見せずに押させる画面が書ける。
 */
export type DeletionState =
  | { status: "loading" }
  | { status: "confirming"; losing: BoardDeletion }
  | { status: "deleting"; losing: BoardDeletion };

export type BoardDeletionFlow = {
  /**
   * 削除の確認。null なら押されていない（ADR 0042）。
   *
   * **失われるものを引き終わるまで確認を出さない。** 件数を伏せたまま
   * 「削除しますか」と訊くと、何を失うのかを知らないまま押させることになる。
   */
  state: DeletionState | null;
  /** 削除で失われるものを引き、確認を出す。 */
  ask: () => Promise<void>;
  /** 確認を経て消す。**取り消せない。** */
  confirm: () => Promise<void>;
  cancel: () => void;
};

/** ボードを消す（ADR 0042）。**取り消せない。** */
export function useBoardDeletion({
  boardId,
  onDeleted,
  onError,
}: {
  boardId: string;
  /** 消えた ID を渡す。親は一覧からその 1 件を外す。 */
  onDeleted: (id: string) => void;
  onError: (failure: Failure) => void;
}): BoardDeletionFlow {
  const [state, setState] = useState<DeletionState | null>(null);

  /**
   * 削除で失われるものを引き、確認を出す。
   *
   * **押されたときだけ引く。** 開いたときに数えると、削除するまで要らない
   * 畳み込みをボードを開くたびに引くことになる（中核思想 3、ADR 0037 の
   * 取り直しと同じ形）。
   *
   * 世代は持たない。ボードを切り替えると BoardPage ごと作り直される
   * （App が `key={current.id}` を渡している）ので、遅れて届いた応答が別の
   * ボードの確認として出ることはない。
   */
  const ask = useCallback(async () => {
    setState({ status: "loading" });
    try {
      setState({ status: "confirming", losing: await boardsApi.deletion(boardId) });
    } catch (e) {
      // 確認を出さずに閉じる。件数を知らないまま「削除しますか」と訊くと、
      // 見せてから選ばせるという約束（ADR 0042）が守れない。
      setState(null);
      onError(describeFailure("削除で失われるものを確かめられませんでした", e));
    }
  }, [boardId, onError]);

  /**
   * ボードを消す。**取り消せない。**
   *
   * GitHub に作った draft issue は消えない。消えるのは etoki 側の記録の
   * ほうで、残った draft issue の出どころが辿れなくなる（ADR 0042）。
   */
  const confirm = useCallback(async () => {
    if (state?.status !== "confirming") return;
    const losing = state.losing;

    setState({ status: "deleting", losing });
    try {
      await boardsApi.delete(boardId);
      onDeleted(boardId);
    } catch (e) {
      // 確認は開いたまま戻す。閉じると、押し直すのに引き直しからになる
      // （改名が下書きを残すのと同じ）。
      setState({ status: "confirming", losing });
      onError(describeFailure("ボードを削除できませんでした", e));
    }
  }, [boardId, state, onDeleted, onError]);

  const cancel = useCallback(() => setState(null), []);

  return useMemo(() => ({ state, ask, confirm, cancel }), [state, ask, confirm, cancel]);
}

export type TargetRefresh = {
  refreshing: boolean;
  refresh: () => Promise<void>;
};

/**
 * 作成先の表示名を GitHub から取り直す。
 *
 * **押されたときだけ引く。** 開いただけで取りにいくと、ボードを開くたびに
 * GitHub を叩くうえ、名前が変わったことに気づく機会が消える（中核思想 3、
 * ADR 0037）。作成先そのものは固定されたままで、送るのは表示用の 3 つだけ。
 */
export function useTargetRefresh({
  board,
  onTargetRefreshed,
  onError,
}: {
  board: BoardDetail;
  /** 取り直したので、手元のボードを差し替えてもらう（版も進む）。 */
  onTargetRefreshed: (board: BoardDetail) => void;
  onError: (failure: Failure) => void;
}): TargetRefresh {
  const [refreshing, setRefreshing] = useState(false);

  const refresh = useCallback(async () => {
    setRefreshing(true);
    try {
      const projects = await githubApi.projects(
        board.repositoryOwner,
        board.repositoryName,
      );
      const project = projects.find((p) => p.id === board.projectId);
      if (!project) {
        // GitHub 側から消えた（あるいは見えなくなった）。作成先は固定なので
        // 選び直しでは直せない。分かったことをそのまま出す。
        onError(targetProjectMissingFailure());
        return;
      }

      // 番号も名前も URL も、この画面が GitHub から受け取ったものをそのまま
      // 送る。組み立てない（ADR 0025）。
      onTargetRefreshed(
        await boardsApi.refreshTargetDisplay(board.id, {
          projectId: board.projectId,
          projectNumber: project.number,
          projectTitle: project.title,
          projectUrl: project.url,
        }),
      );
    } catch (e) {
      onError(describeFailure("作成先の名前を取り直せませんでした", e));
    } finally {
      setRefreshing(false);
    }
  }, [
    board.id,
    board.projectId,
    board.repositoryName,
    board.repositoryOwner,
    onError,
    onTargetRefreshed,
  ]);

  return useMemo(() => ({ refreshing, refresh }), [refreshing, refresh]);
}
