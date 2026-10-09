import { type KeyboardEvent, useEffect, useRef, useState } from "react";

import type {
  AnnotationStatus,
  DiagramKind,
  Granularity,
  Interpretation,
  ProjectAccess,
} from "../../api/types";
import type { AnnotationMeta } from "../../excalidraw/annotation";
import { boardBarButtonId } from "../BoardBar";
import { GRANULARITY_LABEL, STATE_LABEL, annotationLabels } from "./annotationLabel";
import { annotationCardButtonId } from "./AnnotationPanel";
import { DIAGRAM_KIND_LABELS, diagramKinds } from "../diagram/diagramLabels";
import { InterpretationSection } from "./InterpretationSection";
import type { InterpretationState } from "./interpretationHistory";
import { ItemBody, ItemLink, ProjectLinkLine, UnconfirmedItems } from "./panelParts";
import type { CreationState, RunsProps } from "./panelShared";
import type { ProjectLink } from "../target/projectLink";
import { RunHistory } from "./RunHistory";

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
  /**
   * LLM が未設定なら理由。使えるなら null（ADR 0030）。
   *
   * `creation.projectAccess` とは別物。あちらはこのボードの Project に
   * 書けるか、こちらは etoki に LLM が設定されているか。**混ぜない。**
   */
  unavailable: string | null;
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
  /** キャンバスをそのフレームへ寄せて選択する（ADR 0022）。 */
  onFocus: (frameId: string) => void;
  onChangeGranularity: (frameId: string, granularity: Granularity) => void;
  /** 図の種別を差し替える。`undefined` は「指定なし」に戻す。 */
  onChangeKind: (frameId: string, kind: DiagramKind | undefined) => void;
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
  /** 未保存の変更があるあいだは解釈させない（ADR 0018）。 */
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
  const labels = annotationLabels(annotations);

  return (
    <>
      {[...visited].map((id) => {
        const annotation = annotations.find((a) => a.id === id);
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
            label={labels.get(id) ?? ""}
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

/**
 * 注釈 1 件ぶんの面。開閉の状態（GitHub にあるもの・実行の履歴）はここで持つ。
 * 面は描いたまま隠すので、閉じても残る。
 */
function AnnotationFace({
  sectionRef,
  annotation: a,
  label,
  open,
  onClose,
  onShowOnCanvas,
  frames,
  interpretation,
  creation,
  runs,
  stale,
  canEdit,
  projectLink,
  targetLabel,
}: {
  sectionRef: (el: HTMLElement | null) => void;
  annotation: AnnotationStatus;
  label: string;
  open: boolean;
  onClose: () => void;
  onShowOnCanvas: () => void;
  frames: FrameEditProps;
  interpretation: InterpretationProps;
  creation: CreationProps;
  runs: RunsProps;
  stale: boolean;
  canEdit: boolean;
  projectLink: ProjectLink | null;
  targetLabel: string | null;
}) {
  const id = a.id;
  const headingId = `annotation-detail-title-${id}`;
  const missingId = `annotation-detail-missing-${id}`;
  const viewerId = `annotation-detail-viewer-${id}`;
  const itemsRegionId = `annotation-detail-items-${id}`;
  const runsRegionId = `annotation-detail-runs-${id}`;
  const onCanvas = frames.canvasIds === null || frames.canvasIds.includes(id);
  const items = a.items ?? [];
  const runHistory = runs.states[id];

  const [itemsOpen, setItemsOpen] = useState(false);
  const [runsOpen, setRunsOpen] = useState(false);

  // 粒度と種別の選択欄が出すのは**キャンバスに書いた値**。選ぶと書き換わるのは
  // キャンバスの要素で、保存済みの値（a）は次の保存まで古い。保存済みの値に
  // 「選んだ値」を重ねて出す作りは、保存中に元の値へ選び直す・「元に戻す」で
  // キャンバスと食い違った（#124、#214）。
  // キャンバスに無い注釈（未保存で消した frame）だけは保存済みの値を出す。
  // **「キャンバスの注釈に種別が無い」とは分ける。** 混ぜると「指定なし」に
  // 選び直した直後に保存済みの種別が出る。
  const onCanvasMeta = frames.metas?.[id];
  const granularity = onCanvasMeta ? onCanvasMeta.granularity : a.granularity;
  const kind = onCanvasMeta ? onCanvasMeta.kind : a.kind;

  const onKeyDown = (e: KeyboardEvent<HTMLElement>) => {
    if (e.key !== "Escape") return;
    e.stopPropagation();
    onClose();
  };

  // 帯の「解釈する」「GitHub に作成する」は、押すと進行中になって押せなくなる。
  // **押せなくなったボタンからは焦点が外れて body に落ちる。** 落ちたままだと
  // Escape も詳細に届かず、キーボードの利用者は面の中で迷子になる。進行中に
  // 変わったときに焦点が行き場を失っていたら、面へ戻す。
  const element = useRef<HTMLElement | null>(null);
  const busy =
    (interpretation.states[id]?.running ?? false) ||
    creation.states[id]?.status === "running";
  useEffect(() => {
    if (!busy || !open) return;
    const active = document.activeElement;
    const lost =
      active === null ||
      active === document.body ||
      (element.current?.contains(active) === true &&
        (active as HTMLButtonElement).disabled === true);
    if (lost) element.current?.focus();
  }, [busy, open]);

  return (
    // 中の入力欄から泡立つ Escape を受けるだけで、面そのものは押させない。
    // 泡立ちを受ける入れ物に合う role は無い（jsx-a11y の説明のとおり）。
    // eslint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
    <section
      ref={(el) => {
        element.current = el;
        sectionRef(el);
      }}
      className="annotation-detail"
      // モードレスの dialog。開いているあいだも右のパネルとキャンバスは
      // 触れる（上の doc）ので、`aria-modal` は付けない。
      role="dialog"
      aria-labelledby={headingId}
      hidden={!open}
      // 開いた直後の焦点を受けるだけ。Tab の順には入れない。
      tabIndex={-1}
      onKeyDown={onKeyDown}
    >
      <header className="annotation-detail-header">
        <div className="annotation-detail-title">
          <h2 id={headingId}>{label}</h2>
          <span className={`badge badge-${a.state}`}>{STATE_LABEL[a.state]}</span>
          {/*
            キャンバスでその frame を選ぶ口（ADR 0022）。カードを押すと詳細が
            開くので、ここへ移した。**詳細を閉じてから寄せる。** 詳細はキャンバスの
            中央を覆うので、開いたまま寄せても選んだ frame は裏に隠れる。
          */}
          <button
            type="button"
            className="quiet"
            onClick={onShowOnCanvas}
            disabled={!onCanvas}
            aria-describedby={onCanvas ? undefined : missingId}
          >
            キャンバスで見る
          </button>
          <button
            type="button"
            className="quiet annotation-detail-close"
            aria-label="閉じる"
            onClick={onClose}
          >
            <span aria-hidden="true">×</span>
          </button>
        </div>

        <div className="annotation-detail-meta">
          <label className="granularity">
            粒度
            {/* 押せない理由は本文の下にある読むだけの案内を指す（ADR 0039）。 */}
            <select
              value={granularity}
              disabled={!canEdit}
              aria-describedby={canEdit ? undefined : viewerId}
              onChange={(e) =>
                frames.onChangeGranularity(id, e.target.value as Granularity)
              }
            >
              {(Object.keys(GRANULARITY_LABEL) as Granularity[]).map((g) => (
                <option key={g} value={g}>
                  {GRANULARITY_LABEL[g]}
                </option>
              ))}
            </select>
          </label>

          {/*
            何の図として読ませるかを選ばせる。**ひな形は絵を置くだけ**
            （ADR 0047）で、どこを囲むかも何の図かも人が決めるので、種別が載る先は
            ここしかない。粒度と同じ形（`<select>` + 表を引く）にしてあるのは、
            同じメタデータに載る 2 つが画面で別物に見えないため。
          */}
          <label className="granularity">
            種別
            <select
              value={kind ?? ""}
              disabled={!canEdit}
              aria-describedby={canEdit ? undefined : viewerId}
              onChange={(e) => {
                const nextKind = (e.target.value || undefined) as DiagramKind | undefined;
                frames.onChangeKind(id, nextKind);
              }}
            >
              {/*
                「指定なし」は種別の語彙（DiagramKind）に無い値なので、ここだけ
                空文字で表す。選ばれたら customData からキーごと落ちる
                （setAnnotationKind）。
              */}
              <option value="">指定なし</option>
              {diagramKinds().map((k) => (
                <option key={k} value={k}>
                  {DIAGRAM_KIND_LABELS[k]}
                </option>
              ))}
            </select>
          </label>

          {items.length > 0 && (
            <button
              type="button"
              aria-expanded={itemsOpen}
              aria-controls={itemsRegionId}
              onClick={() => setItemsOpen((v) => !v)}
            >
              GitHub にある {items.length} 件
            </button>
          )}

          {/*
            履歴は一度でも実行した注釈にだけ出す。**未実行の注釈にも出すと、
            常に空の枠が並ぶ。** lastSyncedAt があることと run が 1 件以上
            あることは同じ（最新 run から来る）。**件数は読み込んだあとだけ
            添える。** 履歴は押されるまで引かない（`web/CLAUDE.md`）ので、
            開く前は件数を知らない。
          */}
          {a.lastSyncedAt !== undefined && (
            <button
              type="button"
              aria-expanded={runsOpen}
              aria-controls={runsRegionId}
              onClick={() => setRunsOpen((v) => !v)}
            >
              {runHistory?.status === "done"
                ? `実行の履歴 ${runHistory.runs.length} 件`
                : "実行の履歴"}
            </button>
          )}
        </div>
      </header>

      <div className="annotation-detail-body">
        {items.length > 0 && (
          <div
            id={itemsRegionId}
            className="annotation-detail-region"
            hidden={!itemsOpen}
          >
            <ul className="plain-list">
              {items.map((it) => (
                <li key={it.itemId}>
                  <span className="kind">{it.kind}</span> {it.title}
                  <ItemLink link={projectLink} item={it} />
                  <ItemBody body={it.body} />
                </li>
              ))}
            </ul>
            <ProjectLinkLine link={projectLink} />
          </div>
        )}
        {a.lastSyncedAt !== undefined && (
          <div
            id={runsRegionId}
            className="annotation-detail-region run-history"
            hidden={!runsOpen}
          >
            <RunHistory state={runHistory} onLoad={() => runs.onLoad(id)} />
          </div>
        )}

        {/*
         **押さなくても見えている必要があるもの**は畳まずに出す。
         */}
        {!onCanvas && (
          <p className="hint" id={missingId}>
            このフレームはキャンバスにありません。保存すると一覧からも消えます。
          </p>
        )}
        {/* 開発者が手を打つまで消えない（ADR 0056）。 */}
        <UnconfirmedItems items={a.unconfirmedItems ?? []} link={projectLink} />
        {/*
          前回の実行が途中で失敗したこと（ADR 0043）。**状態（3 状態）は変えない。**
          作れたぶんは記録するので created のままであり、そこに件数以外の手掛かりが
          無いのが問題だった。理由の本文は実行の履歴が持つ。
        */}
        {a.lastRunOutcome === "incomplete" && (
          <p className="hint">
            前回の実行は途中で失敗しました。作れたところまでは GitHub 側に残っています。
          </p>
        )}

        {canEdit ? (
          <InterpretationSection
            annotationId={id}
            granularity={a.granularity}
            state={interpretation.states[id]}
            creation={creation.states[id]}
            saving={creation.saving}
            creationBlocked={creation.blocked}
            projectAccess={creation.projectAccess}
            creationUnavailable={creation.unavailable}
            previous={items}
            projectLink={projectLink}
            targetLabel={targetLabel}
            stale={stale}
            interpretationUnavailable={interpretation.unavailable}
            onInterpret={() => interpretation.onInterpret(id)}
            onSelectInterpretation={(runId) => interpretation.onSelect(id, runId)}
            onCreate={(interpretationId, result) =>
              creation.onCreate(id, interpretationId, result)
            }
          />
        ) : (
          // 解釈は LLM を叩く外部呼び出しで、作成は取り消せない（ADR 0017）。
          // 読むだけの人には帯を出さず、なぜ無いのかを言う。押せない粒度と種別も
          // ここを指す。
          <p className="hint" id={viewerId}>
            読むだけの権限で開いています。粒度と種別は変えられず、解釈と作成もできません。
          </p>
        )}
      </div>
    </section>
  );
}
