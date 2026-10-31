import { type KeyboardEvent, useEffect, useRef, useState } from "react";

import type { AnnotationStatus, DiagramKind, Granularity } from "../../api/types";
import { GRANULARITY_LABEL, OFF_CANVAS_NOTICE, STATE_LABEL } from "./annotationLabel";
import type {
  CreationProps,
  FrameEditProps,
  InterpretationProps,
} from "./AnnotationDetail";
import { AnnotationNameLabel } from "./AnnotationNameLabel";
import { DIAGRAM_KIND_LABELS, diagramKinds } from "../diagram/diagramLabels";
import { InterpretationSection } from "./InterpretationSection";
import { ItemBody, ItemLink, ProjectLinkLine, UnconfirmedItems } from "./panelParts";
import type { RunsProps } from "./panelShared";
import type { ProjectLink } from "../target/projectLink";
import { RunHistory } from "./RunHistory";
import { useNameDraft } from "./useNameDraft";

/**
 * 注釈 1 件ぶんの面。開閉の状態（GitHub にあるもの・実行の履歴）はここで持つ。
 * 面は描いたまま隠すので、閉じても残る。
 */
export function AnnotationFace({
  sectionRef,
  annotation: a,
  numberLabel,
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
  numberLabel: string;
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
  const labelId = `annotation-detail-label-${id}`;
  const missingId = `annotation-detail-missing-${id}`;
  const viewerId = `annotation-detail-viewer-${id}`;
  const unmarkedId = `annotation-detail-unmarked-${id}`;
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

  // 名前も同じく**キャンバスの値**を出す（#249）。入力の途中の書きかけは
  // `useNameDraft` が持つ。
  const canvasName = frames.names?.[id];
  const name = canvasName ?? a.name;
  // キャンバスには在るが、もう注釈ではない（保存前に外した）。名前を書く口は注釈に
  // しか書かない（`setAnnotationName`）ので、押せると入力した名前が黙って消える。
  // **まだ聞いていない（null）うちは外したことにしない**（`canvasIds` と同じ）。
  const unmarked = onCanvas && frames.names !== null && canvasName === undefined;
  // 見出しに出す名前もキャンバスの値にする。保存済みの値のままだと、確定した
  // のに見出しが古い名前のまま次の保存まで残る。名前が無いときは番号の見出し。
  const heading = name.trim() === "" ? numberLabel : name;
  // 名前を変える口は見出しの隣のラベル（#305）。押すと欄になる。書きかけと
  // 確定は `useNameDraft`、描き分けは `AnnotationNameLabel`。
  const canRename = canEdit && onCanvas && !unmarked;
  const nameDraft = useNameDraft({
    id,
    name,
    canRename,
    onChangeName: frames.onChangeName,
  });

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
      // 触れる（`AnnotationDetail` の doc）ので、`aria-modal` は付けない。
      role="dialog"
      aria-labelledby={`${headingId} ${labelId}`}
      hidden={!open}
      // 開いた直後の焦点を受けるだけ。Tab の順には入れない。
      tabIndex={-1}
      onKeyDown={onKeyDown}
    >
      <header className="annotation-detail-header">
        <div className="annotation-detail-title">
          {/*
            見出しは「絵解き」。どの注釈かは隣の名前で示す。面は注釈の数だけ描いた
            まま残るので、見出しだけだと同じ名前の面が並ぶ。ダイアログの名前は
            2 つをつないで読ませる（`aria-labelledby`）。
          */}
          <h2 id={headingId}>絵解き</h2>
          <AnnotationNameLabel
            labelId={labelId}
            heading={heading}
            canRename={canRename}
            name={nameDraft}
          />
          <span className={`badge badge-${a.state}`}>{STATE_LABEL[a.state]}</span>
          {/*
            キャンバスでその frame を選ぶ口（ADR 0022）。カードを押すと詳細が
            開くので、ここへ移した。**詳細を閉じてから寄せる。** 詳細はキャンバスの
            中央を覆うので、開いたまま寄せても選んだ frame は裏に隠れる。
          */}
          <button
            type="button"
            className="quiet annotation-detail-to-canvas"
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
        {canEdit && unmarked && (
          <p className="hint" id={unmarkedId}>
            このフレームは絵から外してあります。名前を変えるには、もう一度絵にしてください。
          </p>
        )}
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
            {OFF_CANVAS_NOTICE}
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
            syncState={a.state}
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
            premise={interpretation.premise}
            interpretationBusyElsewhere={Object.entries(interpretation.states).some(
              ([otherId, other]) => otherId !== id && other.running,
            )}
            // 名前の書きかけは、読む前に確定する。帯のボタンは押しても焦点を
            // 奪わない（`DetailBand`）ので、欄を離れたときの確定が走らない。
            onInterpret={() => {
              nameDraft.commit();
              interpretation.onInterpret(id);
            }}
            onSelectInterpretation={(runId) => interpretation.onSelect(id, runId)}
            onCancelInterpretation={() => interpretation.onCancel(id)}
            onCreate={(interpretationId, result) =>
              creation.onCreate(id, interpretationId, result)
            }
          />
        ) : (
          // 解釈は LLM を叩く外部呼び出しで、作成は取り消せない（ADR 0017）。
          // 読むだけの人には帯を出さず、なぜ無いのかを言う。押せない名前・粒度・
          // 種別もここを指す。
          <p className="hint" id={viewerId}>
            読むだけの権限で開いています。名前・粒度・種別は変えられず、絵解きと作成もできません。
          </p>
        )}
      </div>
    </section>
  );
}
