import type { AnnotationStatus, DetachedAnnotation, Granularity } from "../../api/types";
import type { SelectableFrame } from "../../excalidraw/annotation";
import {
  STATE_LABEL,
  annotationLabels,
  annotationSummary,
  frameLabel,
} from "./annotationLabel";
import { sortByState } from "./annotationOrder";
import { DetachedSection } from "./DetachedSection";
import type { RunsProps } from "./panelShared";
import type { ProjectLink } from "../target/projectLink";

/**
 * カードのボタンの id。詳細を閉じたとき、焦点をここへ戻す（`AnnotationDetail`）。
 */
export function annotationCardButtonId(annotationId: string): string {
  return `annotation-open-${annotationId}`;
}

/** 一覧の見出しの id。開いていた詳細の注釈が消えたとき、焦点をここへ移す。 */
export const ANNOTATION_LIST_HEADING_ID = "annotation-list-heading";

/**
 * キャンバスの frame とのやりとりのうち、右のパネルが持つもの。
 *
 * **ひとまとまりで渡す。** どれも「いまキャンバスで何が選ばれ、どの frame が
 * 在るか」を答えるもので、1 つだけ差し替わることが無い（#146）。粒度・種別の
 * 選択と「キャンバスで見る」は注釈の詳細へ移した（#201）。
 */
type FramesProps = {
  /** 選択中の frame のうち、まだ注釈になっていないもの。 */
  markable: SelectableFrame[];
  /** 選択中の frame のうち、すでに注釈になっているもの。 */
  unmarkable: SelectableFrame[];
  /**
   * キャンバスにいま在る frame の ID。まだ分からなければ null。
   *
   * 状態は保存済みシーンが基準なので、未保存で消したフレームの注釈が一覧に
   * 残る。**分からないうちは無いことにしない。** 空配列と同じ扱いにすると、
   * マウント直後の一瞬だけ全部のカードが「キャンバスにありません」になる。
   */
  canvasIds: string[] | null;
  /** キャンバスで選択中の frame の ID。対応するカードを強調するために使う。 */
  selectedIds: string[];
  onMark: (frameId: string, granularity: Granularity) => void;
  onUnmark: (frameId: string) => void;
};

/**
 * **関心ごとに束ねて受け取る**（#146）。束は `BoardPage` が持つ関心の単位でも
 * あるので、そのまま渡せる。
 */
type Props = {
  annotations: AnnotationStatus[];
  /**
   * シーンから消えたのに GitHub 側にものが残っている注釈（#111）。
   *
   * **`annotations` と混ぜて渡さない。** 3 状態も名前も無いので、注釈の
   * カードと同じ形では出せない。
   */
  detached: DetachedAnnotation[];
  frames: FramesProps;
  runs: RunsProps;
  /** 未保存の変更があるとき、状態表示は古い可能性がある。 */
  stale: boolean;
  /** 編集できるか。viewer は false（ADR 0017）。 */
  canEdit: boolean;
  /** 作成先へのリンク。組めなければ null（ADR 0025）。 */
  projectLink: ProjectLink | null;
  /** 注釈 1 件の詳細（`AnnotationDetail`）を開く。 */
  onOpenDetail: (annotationId: string) => void;
};

/**
 * 注釈の一覧（右のパネルの「注釈」タブ）。
 *
 * **このパネルの主題は、注釈ごとの状態を一望すること。** カードには名前・
 * 状態・要約 1 行だけを置き、粒度と種別の選択、GitHub にあるもの、実行の履歴、
 * 解釈と作成は詳細へ移した（#201）。カードに並べていたころは、注釈が 3 つで
 * パネルが埋まり、ほかの注釈の状態が読めなくなった。
 */
export function AnnotationPanel({
  annotations,
  detached,
  frames,
  runs,
  stale,
  canEdit,
  projectLink,
  onOpenDetail,
}: Props) {
  // 見出しは 2 つの欄で共有する。同じ注釈が片方は名前、もう片方は番号で
  // 出ると、同じものが 2 つあるように見える。
  const labels = annotationLabels(annotations);

  return (
    <aside className="panel">
      {/* 見出しは見た目だけ隠す。右のパネルのタブに同じ名前が出ている（`SidePanel`）。 */}
      <h2 className="visually-hidden">注釈</h2>

      {!canEdit && (
        <p className="hint" role="status">
          {"読むだけの権限で開いています。編集・解釈・作成はできません。"}
        </p>
      )}

      <section className="panel-section">
        {/*
          開いていた詳細の注釈が保存で消えたとき、焦点をここへ移す。押せる
          ものではないので Tab の順には入れない。
        */}
        <h3 id={ANNOTATION_LIST_HEADING_ID} tabIndex={-1}>
          注釈 {annotations.length} 件
          {stale && <span className="stale"> （未保存の変更あり）</span>}
        </h3>

        {annotations.length === 0 ? (
          <p className="hint">保存済みの注釈はありません。</p>
        ) : (
          <ul className="annotation-list">
            {sortByState(annotations).map((a) => {
              const onCanvas =
                frames.canvasIds === null || frames.canvasIds.includes(a.id);
              const selected = frames.selectedIds.includes(a.id);
              const nameId = `annotation-name-${a.id}`;
              const stateId = `annotation-state-${a.id}`;
              const summaryId = `annotation-summary-${a.id}`;
              const unconfirmed = a.unconfirmedItems?.length ?? 0;

              return (
                <li
                  key={a.id}
                  className={`annotation${selected ? " selected" : ""}`}
                  // キャンバスで選択したフレームがどのカードなのかを、色だけに
                  // 頼らず読み上げにも届く形で示す（ADR 0022）。
                  aria-current={selected ? "true" : undefined}
                >
                  {/*
                    カード全体を 1 つのボタンにして、押したら詳細を開く。
                    **キャンバスに無い注釈も押せる。** 詳細で GitHub にあるものや
                    履歴を読めるようにするため。押せないのは詳細の「キャンバスで
                    見る」のほう（ADR 0022）。

                    名前は注釈の見出しだけにし、状態と要約は説明として結ぶ。
                    全部を名前にすると、名前で引く読み上げと E2E の両方で長すぎる。
                  */}
                  <button
                    type="button"
                    className="annotation-open"
                    id={annotationCardButtonId(a.id)}
                    aria-labelledby={nameId}
                    aria-describedby={`${stateId} ${summaryId}`}
                    onClick={() => onOpenDetail(a.id)}
                  >
                    <span className="annotation-open-head">
                      <span id={nameId} className="annotation-open-name">
                        {labels.get(a.id)}
                      </span>
                      <span id={stateId} className={`badge badge-${a.state}`}>
                        {STATE_LABEL[a.state]}
                      </span>
                    </span>
                    <span id={summaryId} className="annotation-open-summary">
                      {annotationSummary(a)}
                    </span>
                    <span className="annotation-open-chevron" aria-hidden="true">
                      ›
                    </span>
                  </button>

                  {/*
                    **押さなくても見えている必要があるもの**は、ボタンの外に
                    畳まずに出す。中身の一覧と続きの説明は詳細に出す。
                  */}
                  {!onCanvas && (
                    <p className="hint">
                      このフレームはキャンバスにありません。保存すると一覧からも消えます。
                    </p>
                  )}
                  {/*
                   **畳まない**（ADR 0056）。開発者が手を打つまで消えない。
                   */}
                  {unconfirmed > 0 && (
                    <p className="hint annotation-warning">
                      {`届いたか分からない書き込みが ${unconfirmed} 件あります。`}
                    </p>
                  )}
                  {/*
                    前回の実行が途中で失敗したことは、詳細を開かなくても見える
                    ところに出す（ADR 0043）。**状態（3 状態）は変えない。**
                  */}
                  {a.lastRunOutcome === "incomplete" && (
                    <p className="hint">前回の実行は途中で失敗しました。</p>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </section>

      <DetachedSection annotations={detached} runs={runs} projectLink={projectLink} />

      {/*
        付け外しの口は一覧の下に置く。いちばん読むのは一覧で、こちらは
        フレームを選んだときにだけ使う。
      */}
      <section className="panel-section">
        <h3>選択中のフレーム</h3>
        {!canEdit ? (
          <p className="hint">注釈を付け外しできるのは編集できる人だけです。</p>
        ) : frames.markable.length === 0 && frames.unmarkable.length === 0 ? (
          <p className="hint">
            フレームツール（F）で囲んでから、そのフレームを選択してください。
          </p>
        ) : (
          <ul className="plain-list">
            {/*
              どのフレームに対する操作なのかを項目ごとに出す。複数を選んだとき、
              ボタンの文言だけでは項目が区別できない（ADR 0022）。
            */}
            {frames.markable.map((frame) => (
              <li key={frame.id}>
                <button type="button" onClick={() => frames.onMark(frame.id, "")}>
                  {frameLabel(frame.name)}
                  <span className="kind">を注釈にする</span>
                </button>
              </li>
            ))}
            {frames.unmarkable.map((frame) => (
              <li key={frame.id}>
                <button type="button" onClick={() => frames.onUnmark(frame.id)}>
                  {labels.get(frame.id) ?? frameLabel(frame.name)}
                  <span className="kind">の注釈を外す</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </aside>
  );
}
