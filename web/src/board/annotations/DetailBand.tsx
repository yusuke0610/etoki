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

  return (
    <div className="annotation-detail-band">
      <button
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
