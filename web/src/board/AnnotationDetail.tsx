import { type KeyboardEvent, useEffect, useRef, useState } from "react";

import type { AnnotationStatus, Interpretation, ProjectAccess } from "../api/types";
import { STATE_LABEL, annotationLabels } from "./annotationLabel";
import type { InterpretationProps } from "./AnnotationPanel";
import {
  InterpretationSection,
  interpretButtonId,
  openDetailButtonId,
} from "./InterpretationSection";
import type { CreationState } from "./panelShared";
import type { ProjectLink } from "./projectLink";

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
  interpretation: Pick<InterpretationProps, "states" | "onSelect">;
  creation: CreationProps;
  projectLink: ProjectLink | null;
};

/**
 * 注釈 1 件の解釈の結果と、作る前の手直し（ADR 0024）。キャンバスの上に広く
 * 開く（ADR 0065）。
 *
 * 右のパネルのカードの中に出していたころは、下書きの手直しまで含めると 1 件で
 * 画面数枚分に伸び、タイトルはパネルの幅で切れていた。**読んで直すものは
 * 広い面で開く。** 解釈と作成はブレストが終わったあとの開発者のフェーズなので
 * （中核思想 1）、キャンバスを一時的に覆っても筋が通る。
 *
 * **画面全体は止めない**（モーダルにしない）。解釈は「解釈する → 見比べる →
 * 粒度を変えて解釈し直す」を行き来する作業で、止めると右のパネルの
 * 「解釈する」も右上の「保存」も押せなくなる。
 *
 * **1 度開いた注釈は描いたまま隠す。** 閉じるたびに外すと、手直し中の下書きが
 * 閉じただけで消える（右のパネルのタブと同じ理由、`SidePanel`）。
 */
export function AnnotationDetail({
  openId,
  openRequest,
  onClose,
  annotations,
  interpretation,
  creation,
  projectLink,
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

  // 閉じたらカードの「解釈結果を開く」へ焦点を戻す。**戻さないと、焦点は
  // 隠した面の中に取り残され、次の Tab が画面の先頭から始まる。**
  //
  // 押したボタンを覚えて戻す形にしない。「解釈する」は押した時点で解釈中に
  // なって押せなくなり、焦点を受けられない。
  //
  // **「解釈結果を開く」が無いことがある。** 開いたまま保存すると解釈が捨てられ、
  // ボタンごと消える。そのときは同じカードの「解釈する」へ戻す。どちらも受けられ
  // ないなら戻さない（無いものへ移そうとして、隠した面に焦点を残さないよう、
  // 候補は存在して押せるものだけに絞る）。
  //
  // **開いたまま別のタブへ切り替えると、カードは DOM に残ったまま隠れる**
  // （`SidePanel`）。隠れたボタンは焦点を受けられないので、見えているものだけを
  // 候補にし、無ければ選ばれているタブへ戻す。
  const close = (id: string) => {
    onClose();
    const candidates = [
      document.getElementById(openDetailButtonId(id)),
      document.getElementById(interpretButtonId(id)),
      document.querySelector('[role="tab"][aria-selected="true"]'),
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

  const onKeyDown = (e: KeyboardEvent<HTMLElement>, id: string) => {
    if (e.key !== "Escape") return;
    e.stopPropagation();
    close(id);
  };

  // **1 度も開いていないうちは注釈の一覧に触らない。** 一覧が壊れていると
  // 見出しを組むところで落ち、開いてもいない詳細の境界が右のパネルと並んで
  // 2 つ目の「表示できませんでした」を出す。
  if (visited.size === 0) return null;
  const labels = annotationLabels(annotations);

  return (
    <>
      {[...visited].map((id) => {
        const annotation = annotations.find((a) => a.id === id);
        // 保存で注釈が消えたら面も出さない。描いたまま残すと、一覧に無い注釈の
        // 下書きが開いたまま取り残される。
        if (!annotation) return null;
        const headingId = `annotation-detail-title-${id}`;

        return (
          // 中の入力欄から泡立つ Escape を受けるだけで、面そのものは押させない。
          // 泡立ちを受ける入れ物に合う role は無い（jsx-a11y の説明のとおり）。
          // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
          <section
            key={id}
            ref={(el) => {
              if (el) sections.current.set(id, el);
              else sections.current.delete(id);
            }}
            className="annotation-detail"
            // モードレスの dialog。開いているあいだも右のパネルとキャンバスは
            // 触れる（上の doc）ので、`aria-modal` は付けない。
            role="dialog"
            aria-labelledby={headingId}
            hidden={id !== openId}
            // 開いた直後の焦点を受けるだけ。Tab の順には入れない。
            tabIndex={-1}
            onKeyDown={(e) => onKeyDown(e, id)}
          >
            <header className="annotation-detail-header">
              <h2 id={headingId}>{labels.get(id)}</h2>
              <span className={`badge badge-${annotation.state}`}>
                {STATE_LABEL[annotation.state]}
              </span>
              <button
                type="button"
                className="quiet annotation-detail-close"
                onClick={() => close(id)}
              >
                閉じる
              </button>
            </header>

            <div className="annotation-detail-body">
              <InterpretationSection
                annotationId={id}
                granularity={annotation.granularity}
                state={interpretation.states[id]}
                creation={creation.states[id]}
                saving={creation.saving}
                creationBlocked={creation.blocked}
                projectAccess={creation.projectAccess}
                creationUnavailable={creation.unavailable}
                previous={annotation.items ?? []}
                projectLink={projectLink}
                onSelectInterpretation={(runId) => interpretation.onSelect(id, runId)}
                onCreate={(interpretationId, result) =>
                  creation.onCreate(id, interpretationId, result)
                }
              />
            </div>
          </section>
        );
      })}
    </>
  );
}
