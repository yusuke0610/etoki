import { useEffect, useId, useRef, useState, useSyncExternalStore } from "react";

import { ErrorBoundary } from "../app/ErrorBoundary";
import type { Theme } from "../app/theme";
import { TUTORIAL_PAGES } from "./pages";

type Props = {
  /** 開いているか。開くのは一覧の口とキャンバスのメニュー。 */
  open: boolean;
  /** いまのテーマ。同じテーマで録った動画を見せる（ADR 0055）。 */
  theme: Theme;
  /**
   * 閉じた。**焦点を戻すのは呼ぶ側。** 開いた口が一覧のボタンか、メニューの
   * 項目（閉じた時点で消えている）かで、戻す先が違う。
   */
  onClose: () => void;
};

/**
 * etoki の使い方（#66、ADR 0081）。操作を録った動画を 3 ページで見せる。
 *
 * **ボードを作らせない。** 練習のボードには作成先の選択が要り（ADR 0017）、
 * 作成まで進むと取り消せない draft issue が本番の Project に残る（ADR 0009）。
 * 見せるだけにしておけば、LLM や GitHub を設定していない構成でも同じものを
 * 見せられる。
 *
 * **勝手に開かない**（中核思想 3）。開くのは人が口を押したときだけ。
 *
 * **ブラウザの `<dialog>` をモーダルで開く**（`NewBoardDialog` と同じ）。焦点の
 * 閉じ込めと外の不活性化をブラウザに任せる。
 */
export function TutorialDialog({ open, theme, onClose }: Props) {
  const dialog = useRef<HTMLDialogElement>(null);
  const headingId = useId();
  useEffect(() => {
    const element = dialog.current;
    if (element === null) return;
    if (open && !element.open) element.showModal();
    else if (!open && element.open) element.close();
  }, [open]);

  return (
    <dialog
      ref={dialog}
      className="tutorial-dialog"
      aria-labelledby={headingId}
      // Esc はブラウザが閉じ、「閉じる」も閉じるだけなので、閉じたときの処理は
      // ここ 1 か所に集まる。
      onClose={onClose}
    >
      <h2 id={headingId}>etoki の使い方</h2>
      {/*
        **開いているあいだだけ中身を描く。** 閉じたら外すので、開き直すと
        1 ページ目から始まり、閉じたダイアログの裏で動画が回り続けることもない。
      */}
      {/*
        落ちてもキャンバスごと外れないよう、ダイアログの中で包む（ADR 0027）。
        ボードの上で開くので、外側まで落ちると保存していないブレストが消える。
      */}
      {open && (
        <ErrorBoundary name="使い方" recovery="remount">
          <TutorialPages theme={theme} onDone={() => dialog.current?.close()} />
        </ErrorBoundary>
      )}
    </dialog>
  );
}

function TutorialPages({ theme, onDone }: { theme: Theme; onDone: () => void }) {
  const [index, setIndex] = useState(0);
  const video = useRef<HTMLVideoElement>(null);
  const bodyId = useId();
  const reduced = usePrefersReducedMotion();
  const page = TUTORIAL_PAGES[index];
  const last = index === TUTORIAL_PAGES.length - 1;

  // **動きを減らす設定では自動で再生しない**（ADR 0081）。再生は `controls` から
  // 人が選ぶ。設定が無ければ、ミュートで繰り返し再生する。
  //
  // **`muted` は属性ではなくプロパティで立てる。** React は `muted` を属性として
  // 書かないので、属性を見て自動再生を許すブラウザでは止められる。
  useEffect(() => {
    const element = video.current;
    if (element === null) return;
    element.muted = true;
    if (reduced) element.pause();
    // 止められても困らない。`controls` から再生できる。
    else element.play().catch(() => {});
  }, [reduced, index, theme]);

  if (page === undefined) return null;

  return (
    <div className="tutorial-page">
      <div className="tutorial-step">
        <h3>{page.heading}</h3>
        <span className="tutorial-count">{`${index + 1} / ${TUTORIAL_PAGES.length}`}</span>
      </div>
      {/*
        音は無い。中身は読み上げに届かないので、同じことを下の本文に書いて
        `aria-describedby` で結ぶ（字幕は持たない）。
      */}
      <video
        ref={video}
        className="tutorial-video"
        src={page.video[theme]}
        aria-describedby={bodyId}
        controls
        loop
        muted
        playsInline
        preload="metadata"
      />
      <p id={bodyId} className="tutorial-body">
        {page.body}
      </p>
      <div className="dialog-actions">
        {/* いつでも抜けられる（ADR 0081）。Esc でも閉じる。 */}
        {!last && (
          <button type="button" className="quiet tutorial-skip" onClick={onDone}>
            あとで見る
          </button>
        )}
        {/*
          1 ページ目に「戻る」は出さない。戻る先が無いボタンを押せなくして置くと、
          押せない理由を本文で出す約束（ADR 0039）が要る。
        */}
        {index > 0 && (
          <button type="button" className="quiet" onClick={() => setIndex(index - 1)}>
            戻る
          </button>
        )}
        {last ? (
          <button type="button" className="primary" onClick={onDone}>
            閉じる
          </button>
        ) : (
          <button type="button" className="primary" onClick={() => setIndex(index + 1)}>
            次へ
          </button>
        )}
      </div>
    </div>
  );
}

const REDUCED_MOTION = "(prefers-reduced-motion: reduce)";

function subscribeReducedMotion(onChange: () => void): () => void {
  const query = window.matchMedia(REDUCED_MOTION);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}

/**
 * 動きを減らす設定。**開いているあいだに変えても追う。** 開いたときの値だけを
 * 読むと、設定を変えてもその回は動き続ける。
 */
function usePrefersReducedMotion(): boolean {
  return useSyncExternalStore(
    subscribeReducedMotion,
    () => window.matchMedia(REDUCED_MOTION).matches,
  );
}
