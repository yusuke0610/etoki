/**
 * 作成先の Project を選んだときに、作成に要るフィールドについて出す文（ADR 0088）。
 *
 * **足りないときの文は `errorMessage.ts` から引く。** 押した後に 422 で出る文と
 * 同じ部品から組むため（ADR 0030）。ここに置くのは、失敗ではない状態
 * （揃っている・確かめられなかった・確かめている）の文だけ
 * （`.claude/rules/async-ui.md` の「失敗以外の表示ラベルを寄せない」）。
 */
import { missingProjectFieldsMessage } from "../../api/errorMessage";
import type { ProjectFieldsCheck } from "../../api/types";

/** 確かめた結果、またはまだ確かめている最中（null）。 */
export type FieldsCheckState = ProjectFieldsCheck | null;

/**
 * 結果から画面に出す文を引く。
 *
 * **どの結果でも選ぶことは止めない**（中核思想 3）。確かめられなかったときも
 * 「足りない」「揃っている」のどちらにも倒さない（ADR 0017 の `unknown` と同じ）。
 */
export function projectFieldsNotice(check: FieldsCheckState): string {
  if (check === null) return "作成に要るフィールドを確かめています…";

  switch (check.status) {
    case "ok":
      return "この Project には、作成に要るフィールドが揃っています。";
    case "missing":
      return missingProjectFieldsMessage(check.missingFields);
    case "unknown":
      return "この Project のフィールドを確かめられませんでした。このまま選べます。足りなければ、作成のときに止まって何が要るかを出します。";
  }
}
