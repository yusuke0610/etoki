import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export type DirtyScene = {
  /** 未保存の変更があるか。**表示に使う。** */
  dirty: boolean;
  /**
   * 未保存かどうかを ref から読む。
   *
   * **待ちを挟んだ判定はこちらを使う**（ADR 0021）。取り込みはファイルを読む
   * await を挟んでから確認を出すので、その時点の値が要る。state を読むと、
   * 読んでいるあいだの描き足しを確認なしで捨てる。
   */
  isDirty: () => boolean;
  /** いまのシーンの署名を取り込み、保存済みと違えば未保存にする。 */
  applySignature: (signature: string) => void;
  /**
   * 保存が送った署名を新しい基準にする。
   *
   * **`setDirty(false)` とは書かない。** 保存の待ち時間に編集されていたら
   * 未保存のまま残す必要がある。
   */
  markSaved: (signature: string) => void;
};

/**
 * 未保存かどうかを決める（#146 で `BoardPage` から切り出し）。
 *
 * **判定を持つのはここだけ。** `App` は `onDirtyChange` で受け取るだけで、
 * 自前で判定し直さない。作り直すと「未保存」の定義が 2 箇所になる
 * （`web/CLAUDE.md`）。
 *
 * 署名が「何を描いたか」を持つのに対して、保存の基準（`useSceneSave` の
 * `baseUpdatedAt`）は「何の上に描いたか」を持つ。**混ぜない。**
 */
export function useDirtyScene(onDirtyChange: (dirty: boolean) => void): DirtyScene {
  const [dirty, setDirtyState] = useState(false);
  // 未保存かどうかを ref でも持つ（`isDirty` の doc）。
  const dirtyRef = useRef(false);

  // **書くのは必ずこちらを通す。** state だけを書くと ref が置いていかれ、
  // 「未保存かどうか」の答えが 2 つになる。
  const setDirty = useCallback((next: boolean) => {
    dirtyRef.current = next;
    setDirtyState(next);
  }, []);

  // 保存済みシーンの署名。未保存かどうかはこれと現在の署名の比較で決める。
  // null は Excalidraw から最初の onChange がまだ来ていない状態。
  const savedSignature = useRef<string | null>(null);
  const latestSignature = useRef<string | null>(null);

  const applySignature = useCallback(
    (signature: string) => {
      latestSignature.current = signature;
      // Excalidraw はマウント時にも onChange を発火する。その 1 回目は保存済み
      // シーンそのものなので、未保存ではなく基準として覚える。
      savedSignature.current ??= signature;
      setDirty(signature !== savedSignature.current);
    },
    [setDirty],
  );

  const markSaved = useCallback(
    (signature: string) => {
      savedSignature.current = signature;
      setDirty(latestSignature.current !== signature);
    },
    [setDirty],
  );

  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);

  // 外れるときは未保存を下ろす。残すと、キャンバスがもう無いのに親が止め続ける。
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  const isDirty = useCallback(() => dirtyRef.current, []);

  return useMemo(
    () => ({ dirty, isDirty, applySignature, markSaved }),
    [dirty, isDirty, applySignature, markSaved],
  );
}

/**
 * 失う直前に確認を出す（ADR 0021）。
 *
 * リロード・タブを閉じる・戻るはアプリ側で止められないので、ブラウザに
 * 訊かせる。**自動保存もローカルの下書きも持たないと決めた以上、知らせる
 * 責任が対になる。**
 *
 * 渡すのは「未保存」だけではない。**作成の実行中も外さない**（ADR 0051）。
 * 解釈が保存を要求するので、作成を押す時点ではふつう保存済みで、未保存だけを
 * 見ていると何も訊かれない。閉じれば作成は止まり、残りは作られない。作れた
 * ぶんは記録されるが、閉じた人は結果を見られない。
 */
export function useConfirmLeave(active: boolean): void {
  useEffect(() => {
    if (!active) return;

    const confirmLeave = (e: BeforeUnloadEvent) => {
      // 文面はブラウザが決める。ここで渡した文字列は表示されない。
      e.preventDefault();
      // preventDefault だけを見ないブラウザが残っているので両方立てる。
      e.returnValue = "";
    };

    window.addEventListener("beforeunload", confirmLeave);
    return () => window.removeEventListener("beforeunload", confirmLeave);
  }, [active]);
}
