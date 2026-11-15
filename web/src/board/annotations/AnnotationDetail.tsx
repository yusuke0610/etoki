import { useEffect, useRef, useState } from "react";

import type {
  AnnotationStatus,
  DiagramKind,
  Granularity,
  Interpretation,
  ProjectAccess,
} from "../../api/types";
import type { AnnotationMeta } from "../../excalidraw/annotation";
import { boardBarButtonId } from "../BoardBar";
import { numberedLabel } from "./annotationLabel";
import { AnnotationFace } from "./AnnotationFace";
import { annotationCardButtonId } from "./AnnotationPanel";
import type { InterpretationState } from "./interpretationHistory";
import type { CreationState, RunsProps } from "./panelShared";
import type { ProjectLink } from "../target/projectLink";

/** 解釈の実行と、引いた結果の選び直し。 */
export type InterpretationProps = {
  /** 注釈 ID をキーにした解釈の状態。未実行の注釈は入っていない。 */
  states: Record<string, InterpretationState>;
  onInterpret: (annotationId: string) => void;
  /**
   * 見る解釈を選び直す。
   *
   * 解釈は引き直すたびに揺れるので、前のほうが良いことがある。選び直せないと、
   * 引き直しは「戻せない操作」になる。
   */
  onSelect: (annotationId: string, runId: number) => void;
  /** 走っている解釈を取り消す（#330）。ジョブが分かる前は何もしない。 */
  onCancel: (annotationId: string) => void;
  /**
   * LLM が未設定なら理由。使えるなら null（ADR 0030）。
   *
   * `creation.projectAccess` とは別物。あちらはこのボードの Project に
   * 書けるか、こちらは etoki に LLM が設定されているか。**混ぜない。**
   */
  unavailable: string | null;
  /**
   * いまのボードの背景（ADR 0093）。次に絵解くときに添えるもの。押す前に
   * 見せ、結果に添えた背景と食い違えばそう言う。
   */
  premise: string;
};

/** 作成の実行と、その可否。 */
export type CreationProps = {
  /** 注釈 ID をキーにした作成の状態。未実行の注釈は入っていない。 */
  states: Record<string, CreationState>;
  /**
   * 保存中は下書きの編集も止める。保存が解釈ごと捨てるため。
   *
   * **`blocked` とは別物。** あちらは「押せない理由」で、こちらは「入力を
   * 凍らせるかどうか」。作成を止める条件は保存だけではないので、一方を
   * もう一方から導かない。
   */
  saving: boolean;
  /**
   * いま作成を始められない理由。押せるなら null。
   *
   * **文言はここで組まない。** 何が走っているとどう言うかは
   * `web/src/board/exclusion.ts` の表が持ち、`BoardPage` が引いて渡す
   * （ADR 0060）。パネルが `saving` / `importing` から組み直していたころは、
   * 同じ判定がヘッダーとここの 2 箇所にあった。
   */
  blocked: string | null;
  /**
   * `interpretationId` は下書きの元になった解釈。作ったものをその解釈に
   * 結びつけて持つために渡す（ADR 0052）。
   */
  onCreate: (
    annotationId: string,
    interpretationId: number,
    interpretation: Interpretation,
  ) => void;
  /**
   * 作成先の Project に書けるかどうかの、いまの状態。
   *
   * `denied` でもボタンを黙って消さず、理由を出す。ブレストには参加できて
   * 作成だけができない、というのがこの機能で普通に起きる状態なので、
   * 「なぜできないか」が見えていないと使えない（中核思想 3）。
   */
  projectAccess: ProjectAccess;
  /** GitHub が未設定なら理由。使えるなら null（ADR 0030）。 */
  unavailable: string | null;
};

/**
 * キャンバスの frame とのやりとりのうち、詳細が持つもの（#201）。粒度と種別の
 * 選択と「キャンバスで見る」は、右のパネルのカードからここへ移した。
 */
export type FrameEditProps = {
  /**
   * キャンバスにいま在る frame の ID。まだ分からなければ null。**分からない
   * うちは無いことにしない**（右のパネルと同じ）。
   */
  canvasIds: string[] | null;
  /**
   * キャンバスにいま在る注釈の粒度と種別（frame の ID で引く）。まだ分からなければ
   * null。**粒度と種別の選択欄はここを出す**（保存済みの値ではなく）。
   */
  metas: Record<string, AnnotationMeta> | null;
  /**
   * キャンバスにいま在る注釈の名前（frame の ID で引く、名前が無ければ空文字）。
   * まだ分からなければ null。**名前欄はここを出す**（粒度と種別と同じ理由）。
   */
  names: Record<string, string> | null;
  /** キャンバスをそのフレームへ寄せて選択する（ADR 0022）。 */
  onFocus: (frameId: string) => void;
  onChangeGranularity: (frameId: string, granularity: Granularity) => void;
  /** 図の種別を差し替える。`undefined` は「指定なし」に戻す。 */
  onChangeKind: (frameId: string, kind: DiagramKind | undefined) => void;
  /** frame の名前を書き換える（#249）。**確定したときだけ呼ぶ**（入力の途中では呼ばない）。 */
  onChangeName: (frameId: string, name: string) => void;
};

type Props = {
  /** 開いている注釈。null なら閉じている。 */
  openId: string | null;
  /**
   * 開く操作が起きるたびに増える数。**注釈 ID とは別に持つ。** 開いたままの注釈の
   * 「解釈結果を開く」や解釈し直しでは `openId` が変わらず、ID だけを見ると
   * 焦点がカードに残る。描画ごとには移さない（下書きの入力を奪う）。
   */
  openRequest: number;
  onClose: () => void;
  annotations: AnnotationStatus[];
  frames: FrameEditProps;
  interpretation: InterpretationProps;
  creation: CreationProps;
  runs: RunsProps;
  /** 未保存の変更があるか。「絵解く」が保存してから読むことを案内する（ADR 0077）。 */
  stale: boolean;
  /** 編集できるか。viewer は false（ADR 0017）。 */
  canEdit: boolean;
  projectLink: ProjectLink | null;
  /** 作る先の見出し（`acme/web › #1 ロードマップ`）。作成先が未選択なら null。 */
  targetLabel: string | null;
};

/**
 * 注釈 1 件の詳細。粒度と種別、GitHub にあるもの、実行の履歴、解釈の結果と
 * 作る前の手直し（ADR 0024）を 1 つの面に集め、キャンバスの上に広く開く
 * （ADR 0065、#201）。
 *
 * 右のパネルのカードの中に出していたころは、下書きの手直しまで含めると 1 件で
 * 画面数枚分に伸び、ほかの注釈の状態が読めなくなった。**読んで直すものは
 * 広い面で開く。** 解釈と作成はブレストが終わったあとの開発者のフェーズなので
 * （中核思想 1）、キャンバスを一時的に覆っても筋が通る。
 *
 * **画面全体は止めない**（モーダルにしない）。解釈は「解釈する → 見比べる →
 * 粒度を変えて解釈し直す」を行き来する作業で、止めると右上の「保存」も押せ
 * なくなる。
 *
 * **1 度開いた注釈は描いたまま隠す。** 閉じるたびに外すと、手直し中の下書きが
 * 閉じただけで消える（右のパネルのタブと同じ理由、`SidePanel`）。
 */
export function AnnotationDetail({
  openId,
  openRequest,
  onClose,
  annotations,
  frames,
  interpretation,
  creation,
  runs,
  stale,
  canEdit,
  projectLink,
  targetLabel,
}: Props) {
  const [visited, setVisited] = useState<ReadonlySet<string>>(() => new Set());
  if (openId !== null && !visited.has(openId)) {
    // 描いている最中に積む。effect で積むと、開いた直後の 1 回は中身の無い面が
    // 出る。前の値と違うときだけ置く形なので React が許している。
    setVisited(new Set([...visited, openId]));
  }

  const sections = useRef(new Map<string, HTMLElement>());

  // 開いたら面へ焦点を移す。**移さないと、キーボードの利用者は開いたことに
  // 気づけない。** 見出しではなく面そのもので受ける（削除の確認と同じ形）。
  useEffect(() => {
    if (openId !== null) sections.current.get(openId)?.focus();
    // `openRequest` を依存に置くのは、同じ注釈を開き直す操作でも焦点を移すため。
  }, [openId, openRequest]);

  // 閉じたらその注釈のカードへ焦点を戻す。**戻さないと、焦点は隠した面の中に
  // 取り残され、次の Tab が画面の先頭から始まる。**
  //
  // **カードが見えていないことがある。** 開いたまま別のタブへ切り替えると、
  // カードは DOM に残ったまま隠れる（`SidePanel`）。パネルを畳んでいれば、
  // タブの列ごと隠れる（#202）。隠れたボタンは焦点を受けられないので、見えて
  // いて押せるものだけを候補にし、カードが無理なら選んでいるタブ、それも
  // 無理なら畳んだ帯の「いまのタブ」へ戻す。**スマホの置き方では帯が無い**
  // （#199）。パネルを畳んでいれば、上の帯の「注釈」へ戻す。
  const close = (id: string) => {
    onClose();
    const candidates = [
      document.getElementById(annotationCardButtonId(id)),
      document.querySelector('[role="tab"][aria-selected="true"]'),
      document.querySelector('.side-panel-rail-tab[aria-current="true"]'),
      document.getElementById(boardBarButtonId("annotations")),
    ];
    for (const el of candidates) {
      if (
        el instanceof HTMLButtonElement &&
        !el.disabled &&
        el.getClientRects().length > 0
      ) {
        el.focus();
        return;
      }
    }
  };

  // **1 度も開いていないうちは注釈の一覧に触らない。** 一覧が壊れていると
  // 見出しを組むところで落ち、開いてもいない詳細の境界が右のパネルと並んで
  // 2 つ目の「表示できませんでした」を出す。
  if (visited.size === 0) return null;

  return (
    <>
      {[...visited].map((id) => {
        const index = annotations.findIndex((a) => a.id === id);
        const annotation = annotations[index];
        // 保存で注釈が消えたら面も出さない。描いたまま残すと、一覧に無い注釈の
        // 下書きが開いたまま取り残される。
        if (!annotation) return null;

        return (
          <AnnotationFace
            key={id}
            sectionRef={(el) => {
              if (el) sections.current.set(id, el);
              else sections.current.delete(id);
            }}
            annotation={annotation}
            numberLabel={numberedLabel(index)}
            open={id === openId}
            onClose={() => close(id)}
            onShowOnCanvas={() => {
              close(id);
              frames.onFocus(id);
            }}
            frames={frames}
            interpretation={interpretation}
            creation={creation}
            runs={runs}
            stale={stale}
            canEdit={canEdit}
            projectLink={projectLink}
            targetLabel={targetLabel}
          />
        );
      })}
    </>
  );
}
