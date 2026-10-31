import { Excalidraw } from "@excalidraw/excalidraw";
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { useCallback, type ComponentProps, type ReactNode, type Ref } from "react";

import { canvasMermaidPasteFailure } from "../api/errorMessage";
import { ErrorBoundary } from "../app/ErrorBoundary";
import type { Theme } from "../app/theme";
import type { AnnotationBox } from "../excalidraw/annotationOverlay";
import { isMaybeMermaidDefinition } from "../excalidraw/excalidrawMermaid";
import { AnnotationOverlay } from "./annotations/AnnotationOverlay";
import type { BoardErrorHandler } from "./useBoardNotify";

/**
 * ライブラリのメニューから閉じるもの（ADR 0045）。
 *
 * **持ち出しと取り込みの口は etoki のメニュー 1 つに寄せる。** ライブラリ側を
 * 残すと、同じ画面に意味の違う「保存」が 2 つ並び、片方だけが etoki のボード名と
 * 未保存の確認を知っている形になる。
 *
 * **`.excalidraw` を書き出しているのは `saveAsImage` ではなく `export`。** 表示は
 * 「名前を付けて保存...」で、隣の「画像のエクスポート...」と紛らわしい。
 * `saveToActiveFile` はそこで開いたファイルへの上書きなので、一緒に閉じる。
 * **画像のエクスポートは閉じない。** 答えている問いが違う（持ち出しではなく、
 * 絵を他所に貼ること）。
 *
 * **テーマの切り替えは開ける**（ADR 0055）。`theme` を渡すとライブラリは既定で
 * この項目を隠すので、明示しないと手で切り替える口が消える。
 *
 * **モジュールの定数として持つ。** 描画のたびに作り直すと、Excalidraw には
 * 毎回違うオブジェクトが渡る。
 */
const UI_OPTIONS = {
  canvasActions: {
    loadScene: false,
    export: false,
    saveToActiveFile: false,
    toggleTheme: true,
  },
} as const;

type ExcalidrawProps = ComponentProps<typeof Excalidraw>;

type Props = {
  /**
   * キャンバスの枠。スクロールの測定（`measureVisibleCanvas`）とメニューの入口の
   * 名前付け（`useMenuTriggerName`）が読む。
   */
  frameRef: Ref<HTMLDivElement>;
  onApi: (api: ExcalidrawImperativeAPI) => void;
  /** 最初のシーン。渡すのはマウントの 1 回きり（`useInitialScene`）。 */
  initialData: unknown;
  onChange: ExcalidrawProps["onChange"];
  theme: Theme;
  canEdit: boolean;
  onError: BoardErrorHandler;
  renderTopRightUI: ExcalidrawProps["renderTopRightUI"];
  /**
   * キャンバスの子要素（メニューと下の帯）。**1 つにまとめて `useMemo` で持った
   * ものを渡す**（`BoardPage` の `canvasChildren`、ADR 0065）。
   */
  children: ReactNode;
  overlayBoxes: AnnotationBox[];
};

/**
 * Excalidraw と注釈の枠。枠はこの矩形の左上からの座標で置く。
 *
 * **キャンバスに渡すものは、中身が変わったときだけ作り直す**（ADR 0065）。
 * Excalidraw は props が変わると描き直して `onChange` を呼ぶので、ここに渡す
 * 値は呼ぶ側（`BoardPage`）で同一性を保つ。
 */
export function BoardCanvas({
  frameRef,
  onApi,
  initialData,
  onChange,
  theme,
  canEdit,
  onError,
  renderTopRightUI,
  children,
  overlayBoxes,
}: Props) {
  /**
   * mermaid らしい文字列の貼り付けを、Excalidraw が描く前に止める（ADR 0067）。
   *
   * Excalidraw はこれに当たる文字列を etoki の守り（ADR 0040 / 0061）を通さずに
   * mermaid で描く。**当たらないものは素通しする。** `false` を返すと貼り付け
   * そのものが止まるので、広げると普通の文字も貼れなくなる。
   */
  const handlePaste = useCallback(
    (data: { text?: string }) => {
      if (data.text === undefined || !isMaybeMermaidDefinition(data.text)) return true;
      onError(canvasMermaidPasteFailure());
      return false;
    },
    [onError],
  );

  return (
    <div className="canvas-frame" ref={frameRef}>
      <Excalidraw
        excalidrawAPI={onApi}
        initialData={initialData as never}
        onChange={onChange}
        langCode="ja-JP"
        theme={theme}
        // 持ち出しと取り込みの口は etoki のメニューに寄せてある（ADR 0045）。
        UIOptions={UI_OPTIONS}
        // Excalidraw の AI の口を閉じる（ADR 0067）。コマンドパレットの
        // 「Mermaid to Excalidraw」はこれで出し分けられている。0.18.1 は
        // パレットを置かないので今は効いていないが、置かれたときに守りを
        // 通らない口が黙って開かないようにする。「その他」メニューの同じ
        // 項目はこれでは消えないので、`board.css` で隠している。
        aiEnabled={false}
        onPaste={handlePaste}
        // viewer には描かせない。描けるのに保存できないと、描いた内容を
        // 黙って捨てることになる（ADR 0017）。
        viewModeEnabled={!canEdit}
        renderTopRightUI={renderTopRightUI}
      >
        {children}
      </Excalidraw>
      {/*
        注釈の frame を見分けられるようにする。Excalidraw の外に重ねる
        だけで、要素には触らない（AnnotationOverlay の doc）。

        **枠も境界で包む。** 包まずに落ちると外側の 1 枚が受けることになり、
        キャンバスごと外れて未保存のブレストが消える（ADR 0027）。
      */}
      <ErrorBoundary name="絵の枠" recovery="remount">
        <AnnotationOverlay boxes={overlayBoxes} />
      </ErrorBoundary>
    </div>
  );
}
