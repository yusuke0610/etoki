import { useEffect, useRef } from "react";

/** 帯の「絵解く」。 */
export type InterpretControl = {
  /** 説明文の id を注釈ごとに分けるために持つ。詳細は注釈の数だけ描いたまま残る。 */
  annotationId: string;
  /** 1 度でも絵解きしていれば「絵解き直す」。 */
  label: "絵解く" | "絵解き直す";
  running: boolean;
  /**
   * 押せない理由。押せるなら null。設定の不足だけ（ADR 0030）。未保存では止めない。
   * 押した操作の中で保存するので（`BoardPage`、#247）、保存を理由に断らない。
   */
  blocked: string | null;
  onInterpret: () => void;
  /**
   * 走っている解釈を取り消す（#330）。取り消せないあいだ（走っていない、保存の
   * 待ち、ジョブが分かる前）は undefined で、口を出さない。
   */
  onCancel?: () => void;
  /** 取り消しを送って、返事を待っているあいだ（`InterpretationState.cancelling`）。 */
  cancelling?: boolean;
};

/** 帯の「GitHub に作成する」。解釈を 1 件選んでいるときだけ渡す。 */
export type CreateControl =
  /** GitHub が未設定、または Project に書けない。ボタンは出さず、理由だけを出す。 */
  | { kind: "unavailable"; text: string }
  | {
      kind: "ready";
      running: boolean;
      /** 押せない理由。押せるなら null（表は `exclusion.ts` と `blockingReasons`）。 */
      blocked: string | null;
      /** 押せるときに出す、取り消せないことと作る先の文。 */
      notice: string;
      onCreate: () => void;
    };

/**
 * 注釈の詳細の下端に固定する帯（#201）。左に解釈、右に作成、間に文。
 *
 * **押せないボタンの理由は、それぞれ 1 行ずつ本文で出して結ぶ**（ADR 0039 /
 * 0066）。解釈の理由をパネルの上の文で済ませないのは、パネルが畳まれていることが
 * あるから（#202）。
 *
 * **作成を押せるときは、取り消せないことと作る先を出す。** 作るボタンは主となる
 * 操作の緑で、赤にはしない（#203）。取り消せないことはここで言う。
 */
export function DetailBand({
  interpret,
  create,
}: {
  interpret: InterpretControl;
  create?: CreateControl;
}) {
  const interpretBlockedId = `interpret-blocked-${interpret.annotationId}`;
  const createBlockedId = `create-blocked-${interpret.annotationId}`;
  const createNoticeId = `create-notice-${interpret.annotationId}`;
  const createReady = create?.kind === "ready" ? create : null;

  // 取り消しを押すと、押したボタンが消える。焦点を「絵解く」へ戻す。押せるように
  // なるのは描き直したあとなので、押した処理の中ではなく、走るのをやめてから移す。
  const interpretButton = useRef<HTMLButtonElement>(null);
  const refocusAfterCancel = useRef(false);
  useEffect(() => {
    if (interpret.running || !refocusAfterCancel.current) return;
    refocusAfterCancel.current = false;
    interpretButton.current?.focus();
  }, [interpret.running]);
  // 取り消せなかった（走ったまま返事が来た）。ボタンは残るので焦点も動かさない。
  // 印を残すと、あとで解釈が終わったときに焦点が勝手に移る。
  useEffect(() => {
    if (interpret.running && !interpret.cancelling) refocusAfterCancel.current = false;
  }, [interpret.running, interpret.cancelling]);

  return (
    <div className="annotation-detail-band">
      <button
        ref={interpretButton}
        type="button"
        onClick={interpret.onInterpret}
        // **押しても焦点を奪わない。** 名前の欄（#249）は欄を離れたときに確定する
        // ので、焦点が移った瞬間に未保存になり、本文に保存の案内が出て帯が下へ
        // ずれる。押し始めた位置から帯が逃げ、指を離した先にボタンが無い。確定は
        // 押した処理の中で行う（`AnnotationFace` の `onInterpret`）。
        onMouseDown={(e) => e.preventDefault()}
        disabled={interpret.running || interpret.blocked !== null}
        aria-describedby={interpret.blocked !== null ? interpretBlockedId : undefined}
      >
        {interpret.running ? "絵解き中…" : interpret.label}
      </button>
      {/*
        走っているあいだ、押した人が止められる（#330）。止めないと上限（10 分、
        ADR 0085）まで枠を握り、ほかの絵も図のドラフトも押せない。止めるのは
        押したときだけで、画面を離れても止めない（#307）。
      */}
      {interpret.onCancel && (
        <button
          type="button"
          // 作業から離れる操作なので控えめ（`controls.css`）。
          className="quiet"
          onClick={() => {
            refocusAfterCancel.current = true;
            interpret.onCancel?.();
          }}
        >
          {/*
            返事を待つあいだも押せるままにする（2 回目はフックが弾く）。止めると
            焦点が落ち、取り消せなかったときに戻す先が無い。
          */}
          {interpret.cancelling ? "取り消し中…" : "絵解きを取り消す"}
        </button>
      )}

      <div className="annotation-detail-band-text">
        {interpret.blocked !== null && (
          <p className="hint" id={interpretBlockedId}>
            {interpret.blocked}
          </p>
        )}
        {create?.kind === "unavailable" && <p className="hint">{create.text}</p>}
        {createReady &&
          (createReady.blocked !== null ? (
            <p className="hint" id={createBlockedId}>
              {createReady.blocked}
            </p>
          ) : (
            <p className="hint" id={createNoticeId}>
              {createReady.notice}
            </p>
          ))}
      </div>

      {createReady && (
        <button
          type="button"
          className="primary"
          onClick={createReady.onCreate}
          disabled={createReady.running || createReady.blocked !== null}
          aria-describedby={
            createReady.blocked !== null ? createBlockedId : createNoticeId
          }
        >
          {createReady.running ? "作成中…" : "GitHub に作成する"}
        </button>
      )}
    </div>
  );
}
