import { ERROR_MESSAGES } from "./api/errorMessage";
import type { Capabilities, ErrorCode } from "./api/types";

/**
 * 使えない機能と、その理由を表す code。
 *
 * **文言をここに書かない。** 同じ原因でそのエンドポイントは 503 を返すので、
 * 先に見せる文言と後から返る理由が別々になると、片方だけ古くなる。文言は
 * `ERROR_MESSAGES` が 1 つ持つ（ADR 0029）。
 */
const REASON_CODE: Record<keyof Capabilities, ErrorCode> = {
  interpretation: "llm_not_configured",
  creation: "github_not_configured",
  sharing: "sharing_not_configured",
};

/**
 * その機能が使えない理由。使えるなら null。
 *
 * **まだ引けていない（null）ときも null を返す。** 使えない側に倒すと、
 * 確かめていないことを確かめたように見せることになる（中核思想 3）。押せば
 * 503 が返るので、そのときは受け取った code から同じ文言が出る。
 */
export function unavailableReason(
  capabilities: Capabilities | null,
  feature: keyof Capabilities,
): string | null {
  if (capabilities === null) return null;
  if (capabilities[feature]) return null;

  return ERROR_MESSAGES[REASON_CODE[feature]];
}
