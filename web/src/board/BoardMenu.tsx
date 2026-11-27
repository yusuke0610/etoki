import { MainMenu } from "@excalidraw/excalidraw";
import type { ReactNode } from "react";

import type { RunningOperation } from "./exclusion";
import {
  backToListIcon,
  changeTargetIcon,
  deleteBoardIcon,
  exportIcon,
  guideIcon,
  importIcon,
  premiseIcon,
  refreshTargetIcon,
  renameIcon,
  tableIcon,
} from "./menuIcons";
import type { DeletionState } from "./useBoardManagement";

type Props = {
  /** ボード一覧へ戻る。未保存の確認は親が通す。 */
  onClose: () => void;
  targetLocked: boolean;
  /** 名前の編集を始める。 */
  onRename: () => void;
  /** ボードの背景のダイアログを開く（`PremiseDialog`、ADR 0093）。 */
  onPremise: () => void;
  /** 使い方のダイアログを開く（`UsageGuideDialog`、ADR 0094）。 */
  onOpenGuide: () => void;
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
 * あったころと同じで、状態や設定で押せない操作はボタンごと出さずに理由の文を
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
  targetLocked,
  onRename,
  onPremise,
  onOpenGuide,
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
      <MainMenu.Item className="etoki-menu-item" icon={renameIcon} onSelect={onRename}>
        名前を変更
      </MainMenu.Item>
      {/*
        ボードの背景（ADR 0093）。ブレストの外に置くボードの設定なので、名前と
        並べる。
      */}
      <MainMenu.Item className="etoki-menu-item" icon={premiseIcon} onSelect={onPremise}>
        ボードの背景…
      </MainMenu.Item>
      <MainMenu.Separator />
      {targetLocked ? (
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
      */}
      <MainMenu.Item
        className="etoki-menu-item"
        icon={exportIcon}
        onSelect={onExport}
        disabled={!canvasReady}
      >
        書き出し
      </MainMenu.Item>
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
      {importBlocked !== null && <MenuNote id="import-blocked">{importBlocked}</MenuNote>}
      <MainMenu.Item
        className="etoki-menu-item"
        icon={tableIcon}
        onSelect={onAddTable}
        disabled={!canvasReady}
      >
        表
      </MainMenu.Item>
      <MainMenu.Separator />
      <MainMenu.DefaultItems.SaveAsImage />
      <MainMenu.DefaultItems.SearchMenu />
      {/*
        etoki の使い方（ADR 0094）。**誰にでも出す。** Excalidraw の「ヘルプ」は
        キャンバスの操作とショートカットの案内で、囲んで絵解く流れは書いて
        いない。名前で分け、並べて置く。
      */}
      <MainMenu.Item className="etoki-menu-item" icon={guideIcon} onSelect={onOpenGuide}>
        etoki の使い方
      </MainMenu.Item>
      <MainMenu.DefaultItems.Help />
      <MainMenu.DefaultItems.ClearCanvas />
      <MainMenu.Separator />
      <MainMenu.DefaultItems.ToggleTheme />
      <MainMenu.DefaultItems.ChangeCanvasBackground />
      <MainMenu.Separator />
      {/*
        ボードごと畳む（ADR 0042）。**押した時点では消さない。** 何が残るのかを
        引いてから確認を出す。

        **最後に区切って置く。** 取り消せない操作を、日常の操作と同じ並びの
        途中に置かない。
      */}
      <MainMenu.Item
        className="etoki-menu-item danger"
        icon={deleteBoardIcon}
        onSelect={onAskDelete}
        disabled={deletion !== null}
      >
        {deletion?.status === "loading" ? "確認中…" : "ボードを削除"}
      </MainMenu.Item>
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
