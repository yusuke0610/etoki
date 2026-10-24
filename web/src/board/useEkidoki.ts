import { useCallback, useEffect, useMemo, useRef } from "react";

import type { AnnotationStatus } from "../api/types";
import type { AnnotationMeta } from "../excalidraw/annotation";
import { listed } from "./annotations/useAnnotationDetail";
import type { InterpretOptions } from "./annotations/useInterpretations";

export type Ekidoki = {
  /** 右上の「絵解き」。未保存なら保存してから、絵解きの面を開く。 */
  startEkidoki: () => Promise<void>;
  /** 詳細の「絵解く」。未保存なら保存してから読む。 */
  interpretAfterSave: (id: string) => Promise<void>;
};

/**
 * 「絵解き」の流れ（#352 で `BoardPage` から切り出し、ADR 0077）。
 *
 * **解釈の前に保存を挟む**（ADR 0018）。保存・取り直し・確かめ直し・開く/読むの
 * 順番と、待つあいだの変更の扱いは `.claude/rules/async-ui.md` に #259 の指摘が
 * 積まれている。**順番を崩すときは、そこを先に読む。**
 */
export function useEkidoki({
  isDirty,
  save,
  refreshAnnotations,
  canvasMetas,
  interpret,
  openDetail,
  showAnnotationList,
}: {
  /** 未保存か。押した時点ではなく、呼んだ時点の値を返す（`useDirtyScene`）。 */
  isDirty: () => boolean;
  /** 保存する。書けたときだけ true（`useSceneSave`）。 */
  save: () => Promise<boolean>;
  /** 保存済みの注釈の一覧を取り直す。取れなかったら null（`useAnnotationStates`）。 */
  refreshAnnotations: () => Promise<AnnotationStatus[] | null>;
  /** キャンバスにいま在る注釈の粒度と種別。null は「まだ聞いていない」。 */
  canvasMetas: Record<string, AnnotationMeta> | null;
  interpret: (annotationId: string, options?: InterpretOptions) => Promise<void>;
  openDetail: (id: string) => void;
  /** パネルの注釈の一覧を見せる（`usePanelLayout` の `showTab`）。 */
  showAnnotationList: () => void;
}): Ekidoki {
  // 保存のあとに読むキャンバスの値。押した時点の関数は押した時点の値を握って
  // いるので、呼ぶ側が最新の値を引けるよう、描画のたびに置き直す。
  const latestCanvasMetas = useRef(canvasMetas);
  useEffect(() => {
    latestCanvasMetas.current = canvasMetas;
  });

  /**
   * 未保存でなくなるまで保存し、保存済みの注釈の一覧を取り直して返す。保存か
   * 取り直しができなかったら null（ADR 0077）。
   *
   * **保存と取り直しを待つあいだに変わったら、保存し直す。** 保存が送るのは
   * 呼んだ時点のシーンで、待つあいだの描き足しや選び直しは未保存のまま残る
   * （`markSaved`）。1 回で済ませると、このあと読むもの（注釈の一覧、解釈）が
   * 保存済みシーンと食い違う。**確かめ直すのは最後の待ち（取り直し）のあと。**
   * 保存のあとで確かめても、取り直すあいだの変更が残る。
   *
   * **一覧は保存が要らなかったときも取り直す。** 手元の一覧は、開いた直後なら
   * まだ届いておらず、前の取り直しが失敗していれば古い。保存したときは保存の
   * あとの取り直し（`discardAfterSave`）と 2 度引くが、あちらは失敗しても保存を
   * 失敗にしない（保存は済んでいる）ので、成否はここで見る。
   */
  const saveAndRefresh = useCallback(async () => {
    let saved: AnnotationStatus[] | null;
    do {
      while (isDirty()) {
        if (!(await save())) return null;
      }
      saved = await refreshAnnotations();
    } while (saved !== null && isDirty());
    return saved;
  }, [isDirty, refreshAnnotations, save]);

  /**
   * 右上の「絵解き」。**未保存なら保存してから、絵解きの面を開く**（#247）。
   *
   * 保存が要るのは etoki の都合（ADR 0018）で、押す人が知らなくてよい。**未保存の
   * ときだけ保存する。** 保存は全部の注釈の引いた解釈と作成の状態を捨てる
   * （`discardAfterSave`）ので、毎回保存すると、開き直すたびに前の結果が消える。
   * 保存できなかったら（衝突・失敗・排他）開かない。通知と衝突の帯が理由を言う。
   * 保存と取り直しを待つあいだに変わったら保存し直す（`saveAndRefresh`）。待つ
   * あいだに付けた注釈を開くと、保存済みの一覧に無いので詳細がその場で閉じる。
   * **一覧を取り直せなかったときも開かない**（通知が言う）。前の一覧で数えると、
   * 別の面を開く。
   *
   * 開く先: 注釈が 1 件ならその詳細。0 件か複数ならパネルの注釈の一覧。右上の「絵解き」は
   * どの注釈かを指していないので、選ぶのは人（中核思想 3）。スマホではパネルを
   * 全面に開く（上の帯から開くのと同じ口、#199）。**数えるのは取り直した保存済みの
   * 一覧で、キャンバスではない**（状態は保存済みシーンが基準）。キャンバスで数えると、
   * フレームが画面に無い注釈を数えそこね、1 件なのに一覧を開く。
   */
  const startEkidoki = useCallback(async () => {
    const saved = await saveAndRefresh();
    if (saved === null) return;
    const ids = saved.map((a) => a.id);
    const only = ids.length === 1 ? ids[0] : undefined;
    if (only !== undefined) {
      openDetail(only);
      return;
    }
    showAnnotationList();
  }, [openDetail, saveAndRefresh, showAnnotationList]);

  /**
   * 詳細の「絵解く」。**未保存なら保存してから読む**（#247）。
   *
   * 保存の理由は `startEkidoki` と同じ。保存できなかったら読まない。保存は
   * 引いた解釈をすべて捨てるので、**読むのは必ず保存のあと**（先に読むと、
   * 引いた結果が直後に消える）。
   *
   * **保存と取り直しを待つあいだに変わったら、保存し直してから読む**
   * （`saveAndRefresh`）。テキストは保存済みシーンから、画像は画面から取る
   * （ADR 0018）ので、残したまま読むと 2 つが食い違う。画像に使う要素は
   * `interpret` を呼んだその場で読む
   * （`exportAnnotationImage`）ので、未保存でないと確かめてから読むまでのあいだに
   * 変わる隙は無い。
   *
   * **保存で消えた注釈は読まない。** 押す前か待つあいだに、フレームを消したか
   * 注釈から外した。保存済みの一覧に無いので詳細は閉じていて、読んでも結果を出す
   * 先が無い。見るのは取り直した一覧で、**キャンバスではない。** フレームが画面に
   * 無くても、保存済みの一覧に在ればテキストだけで読む（ADR 0018）。**一覧を
   * 取り直せなかったときも読まない**（通知が言う）。消えたかどうかを前の一覧では
   * 決められない。
   */
  const interpretAfterSave = useCallback(
    async (id: string) => {
      const saved = await saveAndRefresh();
      if (saved === null || !listed(saved, id)) return;
      // 控える粒度は、読む時点のキャンバスの値（= 保存済みシーンの値）。押した
      // 時点の値を控えると、待つあいだに選び直したときに読んだものと食い違う。
      const granularity = latestCanvasMetas.current?.[id]?.granularity;
      void interpret(id, { granularity });
    },
    [interpret, saveAndRefresh],
  );

  return useMemo(
    () => ({ startEkidoki, interpretAfterSave }),
    [startEkidoki, interpretAfterSave],
  );
}
