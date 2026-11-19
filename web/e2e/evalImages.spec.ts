import { readdirSync, readFileSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";

import { expect, test } from "@playwright/test";

import type { InterpretRequest } from "../src/api/types";
import { annotationsOfScene } from "./helpers/boardData";
import { openAnnotationDetail, openBoardWithMock } from "./helpers/board";
import { BOARD_ID, baseMock, board } from "./helpers/fixtures";

/**
 * 評価セット（ADR 0098）の画像を、画面の書き出しと同じ経路で作り直す。
 *
 * **テストではなく道具。** `make eval-images` だけが `ETOKI_EVAL_IMAGES` を立てて
 * 回し、`make test-e2e` では飛ばす。画像は絵解くときに画面が囲みから書き出すもの
 * （ADR 0018）で、シーンから Go で描き直すと本番で渡す画像とは別物になる。ここでは
 * ケースのシーンをモックのボードに載せて「絵解く」を押し、送られた画像をそのまま
 * `image.png` に書く。
 *
 * 置き場所は `web/` からの相対で引く（`make` と Playwright はどちらも `web/` で
 * 走る。`screenshots.spec.ts` の出力先と同じ）。
 */
const ROOT = "../internal/usecase/testdata/eval";

/** 1 ケースだけ作り直すときのディレクトリ名（`make eval-images CASE=…`）。 */
const only = process.env.ETOKI_EVAL_CASE ?? "";

/** ディレクトリ 1 つが 1 ケース。README.md のようなファイルは飛ばす。 */
function caseNames(): string[] {
  if (!process.env.ETOKI_EVAL_IMAGES) return [];
  const names = readdirSync(ROOT).filter((name) =>
    statSync(path.join(ROOT, name)).isDirectory(),
  );
  if (only === "") return names;
  if (!names.includes(only)) throw new Error(`ケース ${only} が ${ROOT} に無い`);
  return [only];
}

test.describe("評価セットの画像を書き出す", () => {
  test.skip(!process.env.ETOKI_EVAL_IMAGES, "make eval-images のときだけ書き出す");

  for (const name of caseNames()) {
    test(name, async ({ page }) => {
      const dir = path.join(ROOT, name);
      const scene = readFileSync(path.join(dir, "scene.json"), "utf8");
      const { annotationId } = JSON.parse(
        readFileSync(path.join(dir, "case.json"), "utf8"),
      ) as { annotationId: string };

      // 注釈の一覧はサーバーと同じ規則でシーンから組む（`annotationsOfScene`）。
      const annotations = annotationsOfScene(scene);
      const annotation = annotations.find((a) => a.id === annotationId);
      if (!annotation)
        throw new Error(`${name}: scene.json に注釈 ${annotationId} が無い`);

      const mock = baseMock();
      mock.details[BOARD_ID] = { ...board(), scene };
      mock.annotations[BOARD_ID] = annotations;
      await openBoardWithMock(page, mock);

      const detail = await openAnnotationDetail(page, annotation.name);
      const sent = page.waitForRequest(
        (r) =>
          r.method() === "POST" &&
          new URL(r.url()).pathname ===
            `/api/boards/${BOARD_ID}/annotations/${annotationId}/interpret`,
      );
      await detail.getByRole("button", { name: "絵解く" }).click();

      // 保存済みのシーンを開いただけなので、保存を挟まずにそのまま読む（ADR 0077）。
      const { image } = (await sent).postDataJSON() as InterpretRequest;
      if (!image) throw new Error(`${name}: 画像が送られていない`);
      expect(image.mediaType).toBe("image/png");
      writeFileSync(path.join(dir, "image.png"), Buffer.from(image.data, "base64"));
    });
  }
});
