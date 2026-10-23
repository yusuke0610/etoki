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
  /** 「保存」。保存だけをする（`⌘/Ctrl+S` と同じ口、ADR 0089）。 */
  onSave: () => void;
  /** 「絵解き」。未保存なら保存してから、絵解きの面を開く（`BoardPage`、#247）。 */
  onEkidoki: () => void;
  /** 「保存」と「絵解き」を押せるか。どちらも保存を始めるので、同じ式で止まる。 */
  canSave: boolean;
  /**
   * 「保存」と「絵解き」を押せない理由。押せるなら null（`exclusion.ts` の表の
   * `saving` 行から引く）。
   */
  saveBlocked: string | null;
  /** 保存しているあいだ（「保存」「絵解き」`⌘/Ctrl+S` のどれから始めても）。 */
  saving: boolean;
};

/**
 * 右上に出す。ボード名と、未保存かどうかと、「保存」と「絵解き」（ADR 0065）。
 * #230 で `BoardPage` から切り出し。
 *
 * **「絵解き」は保存を内包する**（#247）。保存が要るのは etoki の都合（状態の
 * 判定と解釈のテキストが保存済みシーンを基準にする、ADR 0018）で、押す人が
 * 知らなくてよい。
 *
 * **保存だけの口は、未保存のあいだ「未保存」の隣にボタンで出す**（ADR 0089）。
 * `⌘/Ctrl+S` だけにすると、キーボードの無い端末では保存のたびに絵解きの面が
 * 開く。**幅でも入力の種類でも出し分けない**（ADR 0075 の「別の境目を持たない」）。
 *
 * **これだけは拡張点のメニューに入れない。** いちばん押すものであり、作成中や
 * 取り込み中に止まる理由（一時的な理由）がいちばん出る場所でもある。待たされて
 * いる本人が見ている場所で理由が読めないと意味が無い（ADR 0066）。
 *
 * **ボード名を隣に置く。** 何を保存し、どのボードを絵解きするのかが、ボタンの
 * 隣で読める。
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
  onEkidoki,
  canSave,
  saveBlocked,
  saving,
}: Props) {
  // 押せない理由は 1 つだけ出し、押せないボタンの全部から指す。2 つ並べても、
  // 止めている操作も待てば解けることも同じなので、読む量が増えるだけになる。
  const describedBy = saveBlocked !== null ? "save-blocked" : undefined;
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
            「保存」とは書かない。未保存のあいだは隣にシーンの「保存」が並ぶので、
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
            保存だけの口（ADR 0089）。**未保存のあいだだけ出す。** 保存済みなら
            押しても書くものが無く、「絵解き」の隣に押す意味の無いボタンが残る。

            **ふつうの格にする**（主となる操作は 1 つの面に 1 つ、#203）。緑は
            「絵解き」のまま。

            ショートカットは `aria-keyshortcuts` で伝え、見える文にはしない。
            キーボードの無い端末で、押せない口を案内することになる（#296）。
            修飾キーは両方書く。どちらが効くかは OS で決まるが、etoki は
            それを見ていない。
          */}
          {dirty && (
            <button
              type="button"
              onClick={onSave}
              disabled={!canSave}
              aria-describedby={describedBy}
              aria-keyshortcuts="Control+S Meta+S"
            >
              {saving ? "保存中…" : "保存"}
            </button>
          )}
          {/*
            取り消せない操作と保存は相互に排他する。**押せない理由を title に
            隠さない**（ADR 0039）。ホバーでしか読めず、disabled なボタンは
            フォーカスも当たらないので、キーボードと読み上げには届かない。
          */}
          <button
            type="button"
            className="primary"
            onClick={onEkidoki}
            disabled={!canSave}
            aria-describedby={describedBy}
          >
            {saving ? "準備中…" : "絵解き"}
          </button>
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
