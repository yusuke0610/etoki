import { partialCreationFailure } from "../api/errorMessage";
import { ErrorNotice } from "../ErrorNotice";
import { partialSummary } from "./itemSummary";
import type { RunHistoryState } from "./panelShared";

/**
 * その注釈の実行履歴（ADR 0007）。
 *
 * **押されるまで引かない。** 開いただけで全注釈ぶん引くと、注釈の数だけ
 * 問い合わせが増える（中核思想 3、作成先の名前の取り直しと同じ形）。
 *
 * **畳み込み（「GitHub にある N 件」）とは別物。** あちらは「いま在るもの」、
 * こちらは「いつ何回に分けて作ったか」。同じものを 2 通りに見せているのでは
 * なく、答えている問いが違う（ADR 0026）。
 */
export function RunHistory({
  state,
  onLoad,
}: {
  /** まだ押していなければ undefined。 */
  state?: RunHistoryState;
  onLoad: () => void;
}) {
  if (state === undefined) {
    return (
      <button type="button" onClick={onLoad}>
        履歴を読み込む
      </button>
    );
  }

  if (state.status === "loading") {
    return <p className="hint">読み込み中…</p>;
  }

  /*
    **読み直す口は、引けたときだけでなく失敗と 0 件にも出す。** 一度引いた注釈は
    キーが残るので、出さないと通信が 1 度失敗しただけでボードを開き直すまで
    履歴を読めない。0 件も同じで、あのあと作った run はここからしか見えない。
  */
  const reload = (
    <button type="button" onClick={onLoad}>
      履歴を読み込み直す
    </button>
  );

  if (state.status === "error") {
    return (
      <>
        <ErrorNotice failure={state.failure} />
        {reload}
      </>
    );
  }

  if (state.runs.length === 0) {
    return (
      <>
        <p className="hint">実行の記録はありません。</p>
        {reload}
      </>
    );
  }

  return (
    <>
      <ul className="plain-list">
        {state.runs.map((run) => (
          <li key={run.id}>
            <span className="hint">{formatRunTimestamp(run.createdAt)}</span>
            {/*
              途中で失敗した run はそれと分かる形にする（ADR 0043）。件数だけを
              並べると、途中で止まった run と「もともとその件数だった run」が
              同じに見え、再実行すべきかどうかを決める材料が無い。

              **outcome が無い run には何も出さない。** 記録していなかった頃の
              run であり、成功したとは言えない。
            */}
            {run.outcome === "incomplete" && (
              <ErrorNotice
                failure={partialCreationFailure(partialSummary(run.items), run.error)}
                live={false}
              />
            )}
            {/*
              その 1 回で何をしたかを出す。**畳んだ結果ではない**ので、
              触らなかった item はここには現れない（ADR 0026）。
            */}
            <ul className="plain-list">
              {run.items.length === 0 ? (
                <li className="hint">作られたものはありません。</li>
              ) : (
                run.items.map((it) => (
                  <li key={it.itemId}>
                    <span className="kind">{it.kind}</span> {it.title}
                    {it.action === "updated" && (
                      <span className="badge badge-updated">更新</span>
                    )}
                  </li>
                ))
              )}
            </ul>
          </li>
        ))}
      </ul>
      {reload}
    </>
  );
}

/** run の実行時刻。日をまたぐので日付まで出す（解釈の履歴とは違う）。 */
export function formatRunTimestamp(at: string): string {
  return new Date(at).toLocaleString("ja-JP");
}
