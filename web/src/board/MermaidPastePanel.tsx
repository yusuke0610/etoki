import { useState } from "react";

import type { Failure } from "../api/errorMessage";
import { ErrorNotice } from "../ErrorNotice";
import { ACCEPTED_KINDS_LABEL, canPaste } from "../excalidraw/mermaidPaste";

/**
 * 「置く」を押した結果。
 *
 * **置けたかどうかで入力を消すかを決める。** 置けなかったときに消すと、
 * 直すはずの文字列ごと失われる。`failure` が null で置けていないのは、
 * 前の置く操作がまだ終わっていなくて弾いたとき（二重押し）。
 */
export type PasteOutcome = { placed: true } | { placed: false; failure: Failure | null };

type Props = {
  /**
   * 貼られている文字列。**持つのは `BoardPage`。** パネルは図のドラフトへ
   * 切り替えたときや「閉じる」で外れるので、ここで持つと直している途中の
   * 入力が消える。
   */
  text: string;
  onChangeText: (text: string) => void;
  /** 貼られた文字列を変換して置く。置けたら入力を消すのもこちら。 */
  onPlace: (text: string) => Promise<PasteOutcome>;
  onClose: () => void;
};

/**
 * 既存の設計（mermaid）を写しとして貼り、図形にして置くパネル（ADR 0062）。
 *
 * **LLM を通さない。** なので LLM を設定していなくても使える
 * （`capabilities` で止めない）。viewer には出さない（描いても保存できない）。
 *
 * **置くまでキャンバスは変わらない。置いても保存はしない**（中核思想 3）。
 * 種別（`kind`）も付けない。`erDiagram` を貼っても、ER 図だと決めるのは
 * 注釈パネルで種別を選ぶ人（ADR 0047）。
 */
export function MermaidPastePanel({ text, onChangeText, onPlace, onClose }: Props) {
  const [placing, setPlacing] = useState(false);
  const [failure, setFailure] = useState<Failure | null>(null);

  const placeable = !placing && canPaste(text);

  const place = async () => {
    if (!placeable) return;
    setPlacing(true);
    // 前の失敗は押した時点で消す。新しい結果の隣に古い理由が残らないように。
    setFailure(null);
    try {
      // 置けたら入力を消すのは `onPlace`（`BoardPage`）。待っているあいだに
      // 閉じて開き直せば書き換えられるので、送った文字列のままのときだけ消す。
      const outcome = await onPlace(text);
      if (!outcome.placed) setFailure(outcome.failure);
    } finally {
      setPlacing(false);
    }
  };

  return (
    <section className="panel mermaid-paste" aria-label="mermaid を貼る">
      <div className="diagram-chat-header">
        <h2>mermaid を貼る</h2>
        <button type="button" onClick={onClose}>
          閉じる
        </button>
      </div>

      <div className="panel-section">
        {/*
          写しであることを先に言う。etoki は版も逆方向の同期も持たないので、
          ここに置いたものを正本にすると必ず古くなる（ADR 0062）。
        */}
        <p className="hint">
          既存の設計を、議論のための写しとして置きます。正本は元の場所のままです。
        </p>
        <form
          className="diagram-chat-form"
          onSubmit={(e) => {
            e.preventDefault();
            void place();
          }}
        >
          <textarea
            aria-label="貼る mermaid"
            rows={10}
            value={text}
            disabled={placing}
            spellCheck={false}
            placeholder={"例:\nerDiagram\n  CUSTOMER ||--o{ ORDER : places"}
            onChange={(e) => onChangeText(e.target.value)}
          />
          <button type="submit" disabled={!placeable}>
            {placing ? "置いています…" : "キャンバスに置く"}
          </button>
        </form>
        <p className="hint">置けるのは {ACCEPTED_KINDS_LABEL} です。</p>
        <p className="hint">
          置いても保存はしません。既存の絵の右外に、重ならないように置きます。
        </p>
      </div>

      {failure !== null && <ErrorNotice failure={failure} />}
    </section>
  );
}
