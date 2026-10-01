/*
 * キャンバスのメニューの etoki の項目に付けるアイコン（#204）。
 *
 * 形は Tabler Icons（v3.48.0、outline）から、使う分だけ写した。Excalidraw が
 * 既定の項目に使っているのと同じ系統の線画なので、並べても浮かない。
 * `@tabler/icons-react` は入れない。数個のために依存を 1 つ増やすことになる。
 *
 * ---
 *
 * Tabler Icons — https://tabler.io/icons
 *
 * MIT License
 *
 * Copyright (c) 2020-2026 Paweł Kuna
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */
import type { ReactNode } from "react";

/**
 * Tabler の 24 の枠に描いた線画を、メニューの項目の枠（1rem 四方、Excalidraw が
 * 決める）に収める。
 *
 * **線の太さは 1.5。** 描いたあとの太さを Excalidraw の既定の項目に揃えた値で、
 * Tabler の既定（2）でも Excalidraw の多くのアイコンが書いている 1.25 でもない。
 * Excalidraw の項目のアイコンは 20 の枠に 1.25 で描いたもの（テーマ・
 * キャンバスのリセット）と、24 の枠に 1.5 で描いたもの（検索・ヘルプ）が混ざって
 * いて、1rem に縮めるとどちらも 1px になる。24 の枠に 1.25 で描くと 0.8px ほどに
 * なり、同じメニューの中でこちらだけ細く見える。
 *
 * 色は `currentColor`。押せない項目は文字と一緒に薄くなり、「ボードを削除」は
 * 文字と同じ危険の色になる。**飾りなので読み上げから外す。** 項目の名前は文字が
 * 持つ。
 */
function menuIcon(paths: ReactNode) {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {paths}
    </svg>
  );
}

/** ボード一覧へ戻る（`arrow-left`）。 */
export const backToListIcon = menuIcon(
  <>
    <path d="M5 12l14 0" />
    <path d="M5 12l6 6" />
    <path d="M5 12l6 -6" />
  </>,
);

/** 名前を変更（`pencil`）。 */
export const renameIcon = menuIcon(
  <>
    <path d="M4 20h4l10.5 -10.5a2.828 2.828 0 1 0 -4 -4l-10.5 10.5v4" />
    <path d="M13.5 6.5l4 4" />
  </>,
);

/** 作成先を変更（`target-arrow`）。 */
export const changeTargetIcon = menuIcon(
  <>
    <path d="M11 12a1 1 0 1 0 2 0a1 1 0 1 0 -2 0" />
    <path d="M12 7a5 5 0 1 0 5 5" />
    <path d="M13 3.055a9 9 0 1 0 7.941 7.945" />
    <path d="M15 6v3h3l3 -3h-3v-3l-3 3" />
    <path d="M15 9l-3 3" />
  </>,
);

/** 作成先の名前を取り直す（`refresh`）。取り直し中も同じ絵にする。 */
export const refreshTargetIcon = menuIcon(
  <>
    <path d="M20 11a8.1 8.1 0 0 0 -15.5 -2m-.5 -4v4h4" />
    <path d="M4 13a8.1 8.1 0 0 0 15.5 2m.5 4v-4h-4" />
  </>,
);

/** 書き出し（`download`）。 */
export const exportIcon = menuIcon(
  <>
    <path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2 -2v-2" />
    <path d="M7 11l5 5l5 -5" />
    <path d="M12 4l0 12" />
  </>,
);

/** 取り込み（`upload`）。 */
export const importIcon = menuIcon(
  <>
    <path d="M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2 -2v-2" />
    <path d="M7 9l5 -5l5 5" />
    <path d="M12 4l0 12" />
  </>,
);

/**
 * ボードを削除（`trash-x`）。**キャンバスのリセットとは違う絵にする。** あちらは
 * ごみ箱（`trash`）で、同じ絵だと、キャンバスの中身を消す操作とボードごと消す
 * 操作が見分けにくい。
 */
export const deleteBoardIcon = menuIcon(
  <>
    <path d="M4 7h16" />
    <path d="M5 7l1 12a2 2 0 0 0 2 2h8a2 2 0 0 0 2 -2l1 -12" />
    <path d="M9 7v-3a1 1 0 0 1 1 -1h4a1 1 0 0 1 1 1v3" />
    <path d="M10 12l4 4m0 -4l-4 4" />
  </>,
);
