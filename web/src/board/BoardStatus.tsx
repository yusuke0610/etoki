type Props = {
  /** ボード名。 */
  name: string;
  /** 名前を編集中なら、その下書き。null なら編集していない（`useRename`）。 */
  nameDraft: string | null;
  onNameDraftChange: (draft: string | null) => void;
  renaming: boolean;
  onRename: () => void;
  dirty: boolean;
  canEdit: boolean;
  onSave: () => void;
  canSave: boolean;
  /** 保存を押せない理由。押せるなら null（`exclusion.ts` の表から引く）。 */
  saveBlocked: string | null;
  saving: boolean;
};

/**
 * 右上に出す。ボード名と、未保存かどうかと、保存（ADR 0065）。#230 で
 * `BoardPage` から切り出し。
 *
 * **保存だけは拡張点のメニューに入れない。** いちばん押すものであり、作成中や
 * 取り込み中に止まる理由（一時的な理由）がいちばん出る場所でもある。待たされて
 * いる本人が見ている場所で理由が読めないと意味が無い（ADR 0066）。
 *
 * **ボード名を隣に置く。** 何を保存するのかが、ボタンの隣で読める。
 *
 * Excalidraw の `renderTopRightUI` に載る。**描くたびに作り直さない**（ADR 0065）。
 * 要素を `useMemo` で持つのは `BoardPage`。
 */
export function BoardStatus({
  name,
  nameDraft,
  onNameDraftChange,
  renaming,
  onRename,
  dirty,
  canEdit,
  onSave,
  canSave,
  saveBlocked,
  saving,
}: Props) {
  return (
    <div className="board-status etoki-ui">
      {nameDraft === null ? (
        <h1>{name}</h1>
      ) : (
        <form
          className="rename-form"
          onSubmit={(e) => {
            e.preventDefault();
            onRename();
          }}
        >
          {/*
            ラベルは一覧の画面の「ボード名」（新規作成の入力）と分ける。
            入れるものが違う（作るボードの名前か、開いているボードの名前か）。
          */}
          <input
            aria-label="ボードの名前"
            value={nameDraft}
            disabled={renaming}
            onChange={(e) => onNameDraftChange(e.target.value)}
            /*
              jsx-a11y が禁じているのは「開いた瞬間に勝手に焦点が移る」
              autoFocus で、ここはそれに当たらない。メニューの「名前を変更」を
              押した結果としてこの入力が現れるので、移さないとキーボードの
              利用者の焦点は body に落ちる。**外すほうが a11y は悪くなる。**
              規則が見ているのは属性で、押した結果として現れたかどうかは
              見られない（ADR 0039）。
            */
            // eslint-disable-next-line jsx-a11y/no-autofocus
            autoFocus
          />
          {/*
            「保存」とは書かない。隣にシーンの保存ボタンが並んでいるので、
            同じ文言だと何を保存するのかが読めない。
          */}
          <button type="submit" disabled={renaming || nameDraft.trim() === ""}>
            {renaming ? "変更中…" : "名前を保存"}
          </button>
          <button
            type="button"
            className="quiet"
            disabled={renaming}
            onClick={() => onNameDraftChange(null)}
          >
            取消
          </button>
        </form>
      )}
      {dirty && <span className="dirty">未保存</span>}
      {canEdit && (
        <>
          {/*
            取り消せない操作と保存は相互に排他する。**押せない理由を title に
            隠さない**（ADR 0039）。ホバーでしか読めず、disabled なボタンは
            フォーカスも当たらないので、キーボードと読み上げには届かない。
          */}
          <button
            type="button"
            className="primary"
            onClick={onSave}
            disabled={!canSave}
            aria-describedby={
              saveBlocked !== null ? "save-shortcut save-blocked" : "save-shortcut"
            }
          >
            {saving ? "保存中…" : "保存"}
          </button>
          {/*
            ショートカットの存在を画面に出す。**`title` に隠さない**
            （ADR 0039）。**ボタンの中には置かない。** 中に置くと読み上げる名前が
            「保存 Ctrl / ⌘ + S」になり、名前で引いている E2E が全部ずれる。
            外に出して `aria-describedby` で結ぶ。

            修飾キーは両方書く。どちらが効くかは OS で決まるが、etoki は
            それを見ていないので、片方だけ出すともう片方の利用者には嘘になる。
          */}
          <kbd className="hint shortcut" id="save-shortcut">
            ⌘/Ctrl+S
          </kbd>
          {saveBlocked !== null && (
            <span className="hint" id="save-blocked">
              {saveBlocked}
            </span>
          )}
        </>
      )}
    </div>
  );
}
