import "@excalidraw/excalidraw/index.css";

import { useCallback, useEffect, useState } from "react";

import { capabilitiesApi } from "../api/boards";
import type { Failure } from "../api/errorMessage";
import type { Capabilities } from "../api/types";
import { LoginPage } from "../auth/LoginPage";
import { useSession } from "../auth/useSession";
import { BoardListPage } from "../boards/BoardListPage";
import { useBoardList } from "../boards/useBoardList";
import { useNewBoardFlow } from "../boards/useNewBoardFlow";
import { BoardPage } from "../board/BoardPage";
import { RepositoryPicker } from "../board/target/RepositoryPicker";
import { ConsentPage } from "../connections/ConsentPage";
import { unavailableReason } from "./capability";
import { parseConsentRequest } from "./location";
import { useNotify } from "../notification/NotificationProvider";
import { Notifications } from "../notification/Notifications";
import type { NotifyOptions } from "../notification/types";
import { useTheme } from "./theme";
import { useBoardNavigation } from "./useBoardNavigation";

export function App() {
  // 画面全体に出す失敗は通知へ（ADR 0058）。1 本の state に持つと、後から
  // 来た失敗が前の失敗を黙って消していた。
  const { notify, dismissKey } = useNotify();
  const showFailure = useCallback(
    (failure: Failure, options: Pick<NotifyOptions, "action" | "key"> = {}) =>
      notify({
        kind: "error",
        message: failure.message,
        detail: failure.detail,
        ...options,
      }),
    [notify],
  );
  // 新しいボードを作るまでの流れ（`useNewBoardFlow`）。ダイアログの開閉と入力、
  // 作成先を選んでいる最中のボードの名前を持つ。
  const newBoard = useNewBoardFlow();
  const {
    dialogOpen: creatingDialog,
    name,
    template,
    creating,
    submitting: creatingWithoutTarget,
    failure: creationFailure,
    openDialog: openCreatingDialog,
    setName,
    setTemplate,
    cancel: cancelCreating,
    backToDialog: backToCreatingDialog,
  } = newBoard;
  // ログイン状態（`useSession`）。session が null のあいだは問い合わせ中。
  const {
    session,
    signedIn,
    reread: rereadSession,
    replace: replaceSession,
  } = useSession(showFailure);
  // ボードの一覧（`useBoardList`）。
  const boardList = useBoardList({
    showFailure,
    dismissKey,
    onLoginRequired: rereadSession,
  });
  const { boards, reload } = boardList;
  // いま使える機能。**null は「まだ確かめていない」。**
  //
  // LLM や GitHub を設定しなくても etoki は起動する（ADR 0008）。設定していない
  // 機能は押した後に 503 で返るだけなので、押す前に見せるために引く。
  const [capabilities, setCapabilities] = useState<Capabilities | null>(null);
  // 作成先を選ぶか。GitHub が未設定と分かっている構成だけ選ばない（ADR 0090）。
  // 候補を引けないので、選択画面に入ると行き止まりになる。**まだ確かめて
  // いなければ選ぶ**（いつもの流れに倒す。ADR 0030 の「引けていないうちは
  // 止めない」と同じ向き）。
  const choosesTarget = unavailableReason(capabilities, "creation") === null;
  // 配色。**持つのはここだけ**で、キャンバスのメニューで切り替えても BoardPage
  // から戻ってくる（ADR 0055）。ログインや作成先の選択の画面にも効かせるため、
  // ボードより上に置く。
  const [theme, setTheme] = useTheme();
  // MCP のクライアントからの認可の要求（ADR 0076）。`/oauth/authorize` が
  // `/?authorize=...` へ転送してくる。**読むのは起動時の 1 度だけ。** 同意の
  // 画面はクライアントの戻り先へ遷移して終わるので、途中で変わることは無い。
  // ログインを挟んでも、戻り先として URL ごと運ばれてくる（ADR 0059）。
  const [consentRequest] = useState(() => parseConsentRequest(window.location.search));
  // どのボードを、どの導線で画面に出しているか（`useBoardNavigation`）。
  // キャンバスが外れる導線（一覧へ戻る・作成先の選択・ログアウト・戻る / 進む）は
  // すべてそちらにあり、未保存の確認と URL の書き換えもそこで通す。
  const {
    current,
    picking,
    handleDirtyChange,
    open,
    closeBoard,
    startCreating,
    createWithTarget,
    createWithoutTarget,
    replaceBoard,
    handleDeleted,
    startPicking,
    changeTarget,
    cancelPicking,
    logout,
  } = useBoardNavigation({
    signedIn,
    consenting: consentRequest !== null,
    showFailure,
    boardList,
    newBoard,
    replaceSession,
  });

  useEffect(() => {
    if (!signedIn) return;
    // 一覧は開いた時点で要る。
    void reload();

    void (async () => {
      try {
        setCapabilities(await capabilitiesApi.get());
      } catch {
        // 引けなくても止めない。null のままなら押させる側に倒れ、これまで
        // どおり押した後に 503 の理由が出る。**確かめられなかったことを
        // 「使えない」として見せない**（中核思想 3）。
      }
    })();
  }, [reload, signedIn]);

  // 問い合わせ中は何も出さない。ログイン画面を一瞬見せてから消すと、
  // 認証を設定していない構成でもちらつく。
  if (session === null) {
    return <div className="app etoki-ui" />;
  }

  if (session.authRequired && !session.authenticated) {
    return (
      // 地を沈めるためだけの印。ログインはカード 1 枚しか置かないので、
      // 他の画面と同じ地にすると、置いたものが浮いて見えない。
      <div className="app app-signed-out etoki-ui">
        <main className="main">
          <LoginPage />
        </main>
      </div>
    );
  }

  if (consentRequest !== null) {
    return (
      // ログイン画面と同じく、カード 1 枚を沈めた地に置く。
      <div className="app app-signed-out etoki-ui">
        <main className="main">
          <ConsentPage
            request={consentRequest}
            // 要求を受けられなかったら一覧へ。同意の画面の URL を残すと、
            // 読み込み直すたびに同じ失敗を繰り返す。
            onLeave={() => window.location.assign("/")}
          />
        </main>
      </div>
    );
  }

  return (
    <div className="app etoki-ui">
      <main className="main">
        {/*
          失敗の通知はフローの外に出す（position: fixed）。帯としてここに置くと、
          出た瞬間にキャンバスが縮んで描画位置が動く（ADR 0058）。
        */}
        <Notifications />

        {/*
          画面は 1 度に 1 つだけ出す（ADR 0064）。一覧・作成先の選択・ボードは
          別の画面で、**一覧はボードと同時に出ない。** 描いているあいだは、他の
          ボードも作成先の構造も視界に入れない（中核思想 1）。

          「ボードに入る → 対象リポジトリ選択 → ブレスト開始」の分岐はここに置く。
          BoardPage の中ではなく手前で切ることで、作成先が決まるまでキャンバスを
          出さないという要求がそのまま形になる。
        */}
        {creating !== null ? (
          <RepositoryPicker
            key="creating"
            title={creating}
            onSelected={createWithTarget}
            onCancel={backToCreatingDialog}
          />
        ) : current === null ? (
          <BoardListPage
            user={session.user}
            onLogout={() => void logout()}
            boards={boards}
            onOpen={(id) => void open(id)}
            choosesTarget={choosesTarget}
            connectionsUnavailable={unavailableReason(capabilities, "mcpConnections")}
            dialog={{
              open: creatingDialog,
              name,
              template,
              onOpen: openCreatingDialog,
              onNameChange: setName,
              onTemplateChange: setTemplate,
              onNext: choosesTarget ? startCreating : () => void createWithoutTarget(),
              onCancel: cancelCreating,
              submitting: creatingWithoutTarget,
              failure: creationFailure,
            }}
          />
        ) : picking || (current.projectId === "" && choosesTarget) ? (
          // 未選択のボードは作成先の選択から始める（ADR 0014）。**GitHub が未設定と
          // 分かっている構成では始めない**（ADR 0090）。候補を引けず、選べないまま
          // 一覧へ戻るしかなくなる。キャンバスをそのまま開き、作成のところで
          // 「GitHub が未設定」と言う（ADR 0030）。
          <RepositoryPicker
            key={current.id}
            title={current.name}
            onSelected={changeTarget}
            // 選び直しなら、選ぶ前のボードへ戻る。**未選択のボードは一覧へ
            // 戻す。** 未選択のうちはキャンバスを出さないので引き返す先が無く、
            // 一覧が同じ画面に無くなったいま、渡さないと行き止まりになる。
            onCancel={picking ? () => void cancelPicking() : closeBoard}
          />
        ) : (
          // ボードを切り替えたら Excalidraw ごと作り直す。initialData は
          // マウント時にしか読まれないため、key を変えないと前のシーンが残る。
          <BoardPage
            key={current.id}
            board={current}
            capabilities={capabilities}
            onClose={closeBoard}
            onChangeTarget={startPicking}
            // 表示名の取り直しと改名と背景の書き換えは、どれも「サーバーが返した
            // ボードで手元を差し替える」だけ。同じ扱いにする（replaceBoard を参照）。
            onTargetRefreshed={replaceBoard}
            onRenamed={replaceBoard}
            onPremiseChanged={replaceBoard}
            onDeleted={handleDeleted}
            onDirtyChange={handleDirtyChange}
            theme={theme}
            onThemeChange={setTheme}
          />
        )}
      </main>
    </div>
  );
}
