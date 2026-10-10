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
  /** 「絵解き」。未保存なら保存してから、絵解きの面を開く（`BoardPage`、#247）。 */
  onEkidoki: () => void;
  canEkidoki: boolean;
  /** 「絵解き」を押せない理由。押せるなら null（`exclusion.ts` の表から引く）。 */
  ekidokiBlocked: string | null;
  /** 「絵解き」の中で保存しているあいだ。 */
  saving: boolean;
};

/**
 * 右上に出す。ボード名と、未保存かどうかと、「絵解き」（ADR 0065）。#230 で
 * `BoardPage` から切り出し。
 *
 * **「絵解き」は保存を内包する**（#247）。保存が要るのは etoki の都合（状態の
 * 判定と解釈のテキストが保存済みシーンを基準にする、ADR 0018）で、押す人が
 * 知らなくてよい。保存だけをしたいときは `⌘/Ctrl+S`（#145）。
 *
 * **これだけは拡張点のメニューに入れない。** いちばん押すものであり、作成中や
 * 取り込み中に止まる理由（一時的な理由）がいちばん出る場所でもある。待たされて
 * いる本人が見ている場所で理由が読めないと意味が無い（ADR 0066）。
 *
 * **ボード名を隣に置く。** どのボードを絵解きするのかが、ボタンの隣で読める。
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
  onEkidoki,
  canEkidoki,
  ekidokiBlocked,
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
      {/*
        未保存のあいだだけ、保存だけの口（ショートカット）を出す。**「絵解き」の
        隣には置かない。** 並べると絵解きのショートカットに読める。**`title` に
        隠さない**（ADR 0039）。

        修飾キーは両方書く。どちらが効くかは OS で決まるが、etoki は
        それを見ていないので、片方だけ出すともう片方の利用者には嘘になる。
      */}
      {dirty && <span className="dirty">未保存</span>}
      {dirty && canEdit && (
        <span className="hint save-shortcut">
          <kbd className="shortcut">⌘/Ctrl+S</kbd> で保存
        </span>
      )}
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
            onClick={onEkidoki}
            disabled={!canEkidoki}
            aria-describedby={ekidokiBlocked !== null ? "ekidoki-blocked" : undefined}
          >
            {saving ? "準備中…" : "絵解き"}
          </button>
          {ekidokiBlocked !== null && (
            <span className="hint" id="ekidoki-blocked">
              {ekidokiBlocked}
            </span>
          )}
        </>
      )}
    </div>
  );
}
