import { MainMenu } from "@excalidraw/excalidraw";
import type { ReactNode } from "react";

import type { BoardRole } from "../api/types";
import type { RunningOperation } from "./exclusion";
import { isOwner } from "./members/roles";
import {
  backToListIcon,
  changeTargetIcon,
  deleteBoardIcon,
  exportIcon,
  importIcon,
  refreshTargetIcon,
  renameIcon,
  shareIcon,
  tableIcon,
} from "./menuIcons";
import type { DeletionState } from "./useBoardManagement";

type Props = {
  /** ボード一覧へ戻る。未保存の確認は親が通す。 */
  onClose: () => void;
  canEdit: boolean;
  role: BoardRole;
  targetLocked: boolean;
  /** 名前の編集を始める。 */
  onRename: () => void;
  /** 共有のダイアログを開く（`ShareDialog`、#248）。 */
  onShare: () => void;
  /** GitHub が使えない理由。使えるなら null（ADR 0030）。 */
  creationUnavailable: string | null;
  onRefreshTarget: () => void;
  refreshingTarget: boolean;
  onChangeTarget: () => void;
  /** 作成先を変更できない理由。押せるなら null。 */
  targetChangeBlocked: string | null;
  /** キャンバスが組み上がったか（Excalidraw の API を受け取ったか）。 */
  canvasReady: boolean;
  onExport: () => void;
  /** 取り込むファイルを選ばせる。入力は `BoardPage` が持つ。 */
  onImport: () => void;
  /** いま走っている操作（`exclusion.ts`）。 */
  running: RunningOperation | null;
  /** 取り込めない理由。押せるなら null（`exclusion.ts` の表から引く）。 */
  importBlocked: string | null;
  importing: boolean;
  onAddTable: () => void;
  /** 削除の確認がいまどこにいるか。null なら押されていない（ADR 0042）。 */
  deletion: DeletionState | null;
  onAskDelete: () => void;
};

/**
 * たまに押す操作はキャンバスのメニューにしまう（ADR 0065）。#230 で
 * `BoardPage` から切り出し。
 *
 * **押せない理由は、しまった先で本文として出す**（ADR 0066）。メニューは
 * 開けばフォーカスが入るので、キーボードと読み上げには届く。形はヘッダーに
 * あったころと同じで、権限や設定で押せない操作はボタンごと出さずに理由の文を
 * 出し、一時的に押せない操作は押せないボタンと理由を `aria-describedby` で結ぶ。
 *
 * **独自のメニューを渡すと既定の中身は丸ごと置き換わる。** 残すものは
 * `MainMenu.DefaultItems` で並べ直してある。**etoki の項目には
 * `etoki-menu-item` を付ける。** axe はライブラリの DOM を外して掛けており、
 * この印で etoki の項目だけを検査に戻している（`web/e2e/helpers/a11y.ts`）。
 * テーマの切り替えはここに残す
 * （ADR 0055 / 0065 の「口は 1 つ」）。**etoki の項目にもアイコンを付ける**
 * （`menuIcons.tsx`、#204）。既定の項目にだけあると、字下げが揃わず 2 種類の
 * 部品が混ざって見える。Excalidraw 自身へのリンク
 * （`Socials`）は etoki の利用者に向けたものではないので置かない。
 *
 * Excalidraw の子要素に載る。**描くたびに作り直さない**（ADR 0065）。
 * 要素を `useMemo` で持つのは `BoardPage`。
 */
export function BoardMenu({
  onClose,
  canEdit,
  role,
  targetLocked,
  onRename,
  onShare,
  creationUnavailable,
  onRefreshTarget,
  refreshingTarget,
  onChangeTarget,
  targetChangeBlocked,
  canvasReady,
  onExport,
  onImport,
  running,
  importBlocked,
  importing,
  onAddTable,
  deletion,
  onAskDelete,
}: Props) {
  return (
    <MainMenu>
      <MainMenu.Item className="etoki-menu-item" icon={backToListIcon} onSelect={onClose}>
        ボード一覧へ戻る
      </MainMenu.Item>
      <MainMenu.Separator />
      {/*
        名前はブレストの中身に属する表示物なので、editor にも直させる
        （作成先の変更は owner だけ、ADR 0017）。押せる人にだけ出す。
      */}
      {canEdit && (
        <MainMenu.Item className="etoki-menu-item" icon={renameIcon} onSelect={onRename}>
          名前を変更
        </MainMenu.Item>
      )}
      {/*
        共有はボードそのものの管理なので、右のパネル（ボードの中身に対する作業）
        ではなくここから開く（#248、ADR 0078）。**誰にでも出す。** 一覧は読む
        だけの人にも見え、招待と解除はダイアログの中で owner にだけ出る
        （ADR 0017）。共有が組み立てられていない構成でも口は消さず、開いた先で
        理由を出す（ADR 0030）。
      */}
      <MainMenu.Item className="etoki-menu-item" icon={shareIcon} onSelect={onShare}>
        共有…
      </MainMenu.Item>
      <MainMenu.Separator />
      {!isOwner(role) ? (
        // 作成先を変えられるのは owner だけ（ADR 0017）。押せるのに 403 で
        // 断るより、押せないことを見せるほうが状態として正しい。
        <MenuNote>作成先を変えられるのはオーナーだけです</MenuNote>
      ) : targetLocked ? (
        // 固定済みなら変更手段を出さない。押せるのに 409 で断るより、
        // 押せないことを見せるほうが状態として正しい。
        //
        // **名前の取り直しだけは出す。** 固定するのは作成先そのもので
        // あって、表示用のスナップショットではない（ADR 0037）。
        <>
          <MenuNote>作成先は確定（draft issue を作成済み）</MenuNote>
          {/*
            GitHub が組み立てられていない構成では、押しても Project の
            一覧を引けない。ボタンを黙って消さず、代わりに理由を出す
            （ADR 0030）。
          */}
          {creationUnavailable !== null ? (
            <MenuNote>{creationUnavailable}</MenuNote>
          ) : (
            <MainMenu.Item
              className="etoki-menu-item"
              icon={refreshTargetIcon}
              onSelect={onRefreshTarget}
              disabled={refreshingTarget}
            >
              {refreshingTarget ? "取り直し中…" : "作成先の名前を取り直す"}
            </MainMenu.Item>
          )}
        </>
      ) : (
        <>
          <MainMenu.Item
            className="etoki-menu-item"
            icon={changeTargetIcon}
            onSelect={onChangeTarget}
            // 選択画面に移るとキャンバスごと外れ、未保存の編集は失われる。
            // 黙って捨てずに、保存してからにしてもらう。
            disabled={targetChangeBlocked !== null}
            aria-describedby={
              targetChangeBlocked !== null ? "target-change-blocked" : undefined
            }
          >
            作成先を変更
          </MainMenu.Item>
          {targetChangeBlocked !== null && (
            <MenuNote id="target-change-blocked">{targetChangeBlocked}</MenuNote>
          )}
        </>
      )}
      <MainMenu.Separator />
      {/*
        持ち出しと取り込みの口はここ 1 つ（ADR 0045）。ライブラリの既定の
        項目（開く・保存）はこのメニューに並べていない。

        書き出しは viewer にも出す。見えているものを出すだけなので、
        共有した相手に新しく見せるものが無い（ADR 0017）。
      */}
      <MainMenu.Item
        className="etoki-menu-item"
        icon={exportIcon}
        onSelect={onExport}
        disabled={!canvasReady}
      >
        書き出し
      </MainMenu.Item>
      {canEdit && (
        <>
          <MainMenu.Item
            className="etoki-menu-item"
            icon={importIcon}
            onSelect={onImport}
            // **作成中は取り込ませない。** キャンバスを置き換えるので、
            // 保存を止めているのと同じ理由で止める（作られた内容と記録
            // されるハッシュが食い違いうる）。
            disabled={!canvasReady || running !== null}
            aria-describedby={importBlocked !== null ? "import-blocked" : undefined}
          >
            {importing ? "取り込み中…" : "取り込み"}
          </MainMenu.Item>
          {importBlocked !== null && (
            <MenuNote id="import-blocked">{importBlocked}</MenuNote>
          )}
          <MainMenu.Item
            className="etoki-menu-item"
            icon={tableIcon}
            onSelect={onAddTable}
            disabled={!canvasReady}
          >
            表
          </MainMenu.Item>
        </>
      )}
      <MainMenu.Separator />
      <MainMenu.DefaultItems.SaveAsImage />
      <MainMenu.DefaultItems.SearchMenu />
      <MainMenu.DefaultItems.Help />
      {canEdit && <MainMenu.DefaultItems.ClearCanvas />}
      <MainMenu.Separator />
      <MainMenu.DefaultItems.ToggleTheme />
      {canEdit && <MainMenu.DefaultItems.ChangeCanvasBackground />}
      <MainMenu.Separator />
      {/*
        ボードごと畳むのは owner だけ（ADR 0042）。**押した時点では消さない。**
        何が残るのかを引いてから確認を出す。

        **消すなら理由を出す**（ADR 0017 / 0030）。権限で押せない操作は、
        ボタンを黙って消さずに押せない理由のほうを見せる。

        **最後に区切って置く。** 取り消せない操作を、日常の操作と同じ並びの
        途中に置かない。
      */}
      {isOwner(role) ? (
        <MainMenu.Item
          className="etoki-menu-item danger"
          icon={deleteBoardIcon}
          onSelect={onAskDelete}
          disabled={deletion !== null}
        >
          {deletion?.status === "loading" ? "確認中…" : "ボードを削除"}
        </MainMenu.Item>
      ) : (
        <MenuNote>ボードを削除できるのはオーナーだけです</MenuNote>
      )}
    </MainMenu>
  );
}

/**
 * メニューの中に出す、押せない理由の文（ADR 0066）。
 *
 * **ボタンとして描かない。** 押しても何も起きないものを項目の形で置くと、
 * 押せるように見える。`ItemCustom` は項目と同じ並びに置けて、押せない。
 */
function MenuNote({ id, children }: { id?: string; children: ReactNode }) {
  return (
    <MainMenu.ItemCustom className="menu-note">
      <span className="hint" id={id}>
        {children}
      </span>
    </MainMenu.ItemCustom>
  );
}
