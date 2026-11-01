import type { NameDraft } from "./useNameDraft";

/**
 * 詳細の見出しの隣に出す注釈の名前（#305、#384 で `AnnotationFace` から切り出し）。
 *
 * 変えられるなら名前そのものがボタンで、押すと欄になる。書きかけと確定は
 * `useNameDraft` が持つ。変えられないなら名前を出すだけで、理由は本文に出す
 * （読むだけ・注釈から外した・キャンバスに無い）。
 */
export function AnnotationNameLabel({
  labelId,
  heading,
  canRename,
  name,
}: {
  /** ダイアログの名前に使う要素の ID（`aria-labelledby`）。 */
  labelId: string;
  /** 確定済みの名前。名前が無ければ番号の見出し。 */
  heading: string;
  canRename: boolean;
  name: NameDraft;
}) {
  // 先に分ける。`ref` に渡した関数を束のまま引くと、`react-hooks/refs` が束ごと
  // ref とみなし、`draft` を描画中に読めなくなる。
  const { draft, start, change, commit, onInputKeyDown, attachInput, attachLabel } = name;

  if (canRename && draft !== null) {
    return (
      <>
        {/*
          ダイアログの名前は `labelId` から引く。書いている最中は欄の値が
          動くので、そのまま指すと名前が打つたびに変わる。隠した行に確定済みの
          名前を残して、そちらを指させる（`hidden` の要素も名前には使える）。
        */}
        <span id={labelId} hidden>
          {heading}
        </span>
        <input
          ref={attachInput}
          className="annotation-detail-label annotation-detail-rename-input"
          aria-label="名前"
          value={draft}
          placeholder="名前を付ける"
          onChange={(e) => change(e.target.value)}
          onBlur={commit}
          onKeyDown={onInputKeyDown}
        />
      </>
    );
  }

  if (canRename) {
    return (
      // 名前そのものを押して変える（#305）。frame の `name` に書くので、
      // キャンバスのラベルにも同じ名前が出る。
      <button
        type="button"
        id={labelId}
        ref={attachLabel}
        className="annotation-detail-label annotation-detail-rename"
        onClick={start}
      >
        {heading}
        <span aria-hidden="true" className="annotation-detail-rename-mark">
          ✎
        </span>
      </button>
    );
  }

  return (
    // 変えられない理由は本文に出している（読むだけ・注釈から外した・
    // キャンバスに無い）。
    <span id={labelId} className="annotation-detail-label">
      {heading}
    </span>
  );
}
