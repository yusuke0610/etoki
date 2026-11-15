import { formatSceneSize } from "../excalidraw/size";

/**
 * 保存が衝突したときの帯。
 *
 * 上書きしなかったことと、いま何ができるのかを本文で出す。バッジに畳むと
 * 「保存できていない」ことしか伝わらず、相手の変更を消したのかどうかが
 * 読めない（ADR 0020）。
 *
 * **控える手段の場所まで言う**（#244 の追記）。「控えて」だけでは、何で控える
 * のかが分からずここで止まる。手段はメニューの「書き出し」（ADR 0045）で、
 * 保存済みではなくキャンバスから出すので、弾かれた描き足しも入る。帯に同じ
 * ボタンを並べず場所を言うのは、書き出しの口を 1 つに寄せているため。
 * 「≡」はアイコンを見て探す人のためのもので、読み上げではメニューの名前が
 * そのまま入口の名前になる（#299）。
 */
export function SaveConflictAlert() {
  return (
    <p className="conflict" role="alert">
      {"他の人がこのボードを保存しました。上書きしないよう未保存のまま残しています。"}
      {"いまの内容をメニュー"}
      <span aria-hidden="true">（≡）</span>
      {"の「書き出し」で控えてから、開き直してください。"}
    </p>
  );
}

type SceneLimitWarningProps = {
  /** いまのシーンの大きさ。まだ測っていなければ null。 */
  sceneBytes: number | null;
};

/**
 * 保存済みシーンが保存できる上限を超えていて、このままでは保存し直せない
 * ことの帯（issue #103、ADR 0038）。
 *
 * **上限の数値は出さない。** サーバーの判定結果を見せるだけで、フロントは
 * 上限を複製しない。開いた時点で分かるよう、キャンバスを描く前から出す
 * （中核思想 3）。
 */
export function SceneLimitWarning({ sceneBytes }: SceneLimitWarningProps) {
  return (
    <p className="scene-limit-warning" role="alert">
      {
        "このボードは保存できる上限を超えています。保存し直すには貼った画像を減らしてください。"
      }
      {sceneBytes !== null && `（いまの大きさ: ${formatSceneSize(sceneBytes)}）`}
    </p>
  );
}
