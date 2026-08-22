import type { AnnotationBox } from "../excalidraw/annotationOverlay";
import { GRANULARITY_LABEL } from "./annotationLabel";

type Props = {
  /** 重ねる枠。いまの見え方に合わせて算出済みのもの（annotationOverlay.ts）。 */
  boxes: AnnotationBox[];
};

/**
 * 注釈にした frame をキャンバス上で見分けられるようにする。
 *
 * 注釈の判定規則は「frame かつ customData.etoki を持つ」であり、ユーザーが
 * ブレスト中に使った frame と混在するのが前提（CLAUDE.md）。混在させておいて
 * 見分ける手段が無いのは、状態を見せるという中核思想 3 に届いていない。
 *
 * **キャンバスには重ねるだけで、要素には触らない。** frame は Excalidraw の
 * フレームツールで作らせると決めてあり（CLAUDE.md）、見た目のために名前や
 * customData 以外を書き換えると、ユーザーの持ちものを etoki が変えることになる。
 *
 * **押せない。** 飛び先はパネルのカードが持っている（ADR 0022）。ここに操作を
 * 足すと、キャンバス上のクリックが Excalidraw と取り合いになる。
 */
export function AnnotationOverlay({ boxes }: Props) {
  return (
    // 読み上げには出さない。同じ情報はパネルが一覧として持っており、
    // こちらは位置を見せるためだけの飾り。二重に読み上げると、注釈の数だけ
    // 同じ見出しが並ぶ。
    <div className="annotation-overlay" aria-hidden="true">
      {boxes.map((box) => (
        <div
          key={box.id}
          className="annotation-overlay-frame"
          style={{
            left: `${box.left}px`,
            top: `${box.top}px`,
            width: `${box.width}px`,
            height: `${box.height}px`,
          }}
        >
          <span className="annotation-overlay-badge">
            {box.granularity === ""
              ? "注釈"
              : `注釈 ${GRANULARITY_LABEL[box.granularity]}`}
          </span>
        </div>
      ))}
    </div>
  );
}
