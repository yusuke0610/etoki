import { useState } from "react";

import type { DiagramKind } from "../api/types";
import { ErrorNotice } from "../ErrorNotice";
import { canSend, turnsRemaining, type DiagramChat } from "./diagramChat";
import { DIAGRAM_KIND_LABELS, DIAGRAM_KINDS } from "./diagramLabels";

/**
 * 「LLM が未設定」の説明文の id。
 *
 * パネルに 1 つしか出さない。押せないボタンからここを指す（`AnnotationPanel`
 * と同じ形）。
 */
const UNAVAILABLE_ID = "diagram-unavailable";

type Props = {
  chat: DiagramChat;
  /** 図の種類を変える。**会話ごと捨てる**（`changeKind` を参照）。 */
  onChangeKind: (kind: DiagramKind) => void;
  /**
   * 指示を送って生成する。生成できたかどうかを返す。
   *
   * **返り値で入力を消すかどうかを決める。** 失敗したときに消すと、書いた
   * 指示ごと失われ、同じことをもう一度打たせることになる。
   */
  onSend: (prompt: string) => Promise<boolean>;
  /** いまのドラフトをキャンバスに置く。 */
  onPlace: () => void;
  onClose: () => void;
  /**
   * 生成が使えない理由。使えるなら null（ADR 0030）。
   *
   * **ボタンを黙って消さない。** 押した後に 503 で返る理由と同じ文が、押す前に
   * 出ているだけ（中核思想 3）。
   */
  unavailable: string | null;
};

/**
 * プロンプトから図のドラフトを作るチャット。
 *
 * **置くまでキャンバスは変わらない**（#58 の原則）。生成した瞬間に流し込まず、
 * 見てから開発者が「置く」を押す（中核思想 3）。置いても保存はしない。
 *
 * **未保存でも使える。** 保存済みシーンを読まないので、解釈の「保存してから
 * 解釈できます」と同じ制約をかける理由が無い（ADR 0041）。ここを揃えると、
 * 説明のつく非対称が「なんとなく揃えた制約」に変わる。
 */
export function DiagramChatPanel({
  chat,
  onChangeKind,
  onSend,
  onPlace,
  onClose,
  unavailable,
}: Props) {
  const [prompt, setPrompt] = useState("");

  const generating = chat.pending !== null;
  const remaining = turnsRemaining(chat);
  const sendable = unavailable === null && canSend(chat, prompt);

  const send = async () => {
    if (!sendable) return;
    // 生成できたときだけ消す。会話は下に積まれるので、成功したのに残すと
    // 同じ指示を 2 度送りやすい。失敗したぶんは残して、直して送り直せる
    // ようにする（上限に当たった 413 もここを通る）。
    if (await onSend(prompt)) setPrompt("");
  };

  return (
    <section className="panel diagram-chat" aria-label="図のドラフト">
      <div className="diagram-chat-header">
        <h2>図のドラフト</h2>
        <button type="button" onClick={onClose}>
          閉じる
        </button>
      </div>

      {/*
        押す前に理由を出す。**`title` に隠さない。** `disabled` なボタンは
        フォーカスも当たらないので、キーボードと読み上げの利用者に届かない
        （ADR 0030、AnnotationPanel と同じ形）。
      */}
      {unavailable !== null && (
        <p className="hint" id={UNAVAILABLE_ID}>
          {unavailable}
        </p>
      )}

      <div className="panel-section">
        <h3>何の図を描くか</h3>
        {/*
          種類はテンプレート（#52）と同じ語彙。どの mermaid 記法で書かれるかは
          サーバーが決めるので、ここには出さない（ADR 0041）。
        */}
        <select
          aria-label="図の種類"
          value={chat.kind}
          disabled={generating}
          onChange={(e) => onChangeKind(e.target.value as DiagramKind)}
        >
          {DIAGRAM_KINDS.map((kind) => (
            <option key={kind} value={kind}>
              {DIAGRAM_KIND_LABELS[kind]}
            </option>
          ))}
        </select>
        {chat.turns.length > 0 && (
          <p className="hint">種類を変えると、ここまでのやりとりは捨てられます。</p>
        )}
      </div>

      <div className="panel-section">
        <h3>{chat.turns.length === 0 ? "どんな図が欲しいか" : "どこを直すか"}</h3>
        <form
          className="diagram-chat-form"
          onSubmit={(e) => {
            e.preventDefault();
            void send();
          }}
        >
          <textarea
            aria-label="図への指示"
            rows={3}
            value={prompt}
            disabled={generating || unavailable !== null}
            aria-describedby={unavailable !== null ? UNAVAILABLE_ID : undefined}
            placeholder={
              chat.turns.length === 0
                ? "例: 注文から出荷までの流れ"
                : "例: 返金の分岐も足して"
            }
            onChange={(e) => setPrompt(e.target.value)}
          />
          <button
            type="submit"
            disabled={!sendable}
            aria-describedby={unavailable !== null ? UNAVAILABLE_ID : undefined}
          >
            {generating ? "生成中…" : chat.turns.length === 0 ? "生成" : "直す"}
          </button>
        </form>
        {/*
          あと何往復できるかは押す前に見えている必要がある。上限に当たってから
          知らせるのでは、積み上げた指示を捨てる判断が間に合わない（ADR 0041）。
        */}
        {remaining !== null && (
          <p className="hint">
            {remaining > 0
              ? `あと ${remaining} 回まで直せます。`
              : "この会話ではこれ以上直せません。種類を選び直すと最初から始められます。"}
          </p>
        )}
      </div>

      {chat.failure !== null && <ErrorNotice failure={chat.failure} />}

      {chat.draft !== null && (
        <div className="panel-section">
          <h3>できた図</h3>
          {/*
            **mermaid のまま見せる。** 変換後の図を先に見せるには、置かない
            ままキャンバスに描く道具が要る。読めない形ではないので、置く前の
            確認は文字で足りる。直すときに指す手がかりにもなる。
          */}
          <pre className="diagram-mermaid">{chat.draft.mermaid}</pre>
          {/*
            **ここが「置く」を挟む唯一の場所。** 生成した瞬間に流し込むと、
            開発者が見てから決める形（中核思想 3）が消える。
          */}
          <button type="button" onClick={onPlace} disabled={generating}>
            キャンバスに置く
          </button>
          <p className="hint">
            置いても保存はしません。既存の絵の右外に、重ならないように置きます。
          </p>
        </div>
      )}

      {chat.turns.length > 0 && (
        <div className="panel-section">
          <h3>ここまでのやりとり</h3>
          <ol className="diagram-turns">
            {chat.turns.map((turn, i) => (
              // 並びは追記のみで、入れ替えも削除もしない。添字で足りる。
              <li key={i}>{turn.prompt}</li>
            ))}
          </ol>
        </div>
      )}
    </section>
  );
}
