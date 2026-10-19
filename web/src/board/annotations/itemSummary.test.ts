import { describe, expect, it } from "vitest";

import { creationNotice } from "./itemSummary";

const TARGET = "acme/web › #1 ロードマップ";

// 押す直前に帯で読む文（#289）。更新は前の内容を消すので、作成と同じ言い方に
// 畳むと、書き換えることを押す前に読めない（ADR 0026）。
describe("creationNotice", () => {
  it("新規だけなら、作ると言う", () => {
    expect(creationNotice({ create: 2, update: 0 }, TARGET, 0)).toBe(
      "作成は取り消せません。選んだ 2 件を acme/web › #1 ロードマップ に作ります。",
    );
  });

  it("更新だけなら、書き換えると言い、作るとは言わない", () => {
    const notice = creationNotice({ create: 0, update: 2 }, TARGET, 0);

    expect(notice).toBe(
      "更新は取り消せません。acme/web › #1 ロードマップ の draft issue 2 件を書き換えます。",
    );
    expect(notice).not.toContain("作ります");
  });

  it("両方あれば、作る数と書き換える数を分けて言う", () => {
    expect(creationNotice({ create: 1, update: 2 }, TARGET, 0)).toBe(
      "作成も更新も取り消せません。acme/web › #1 ロードマップ に 1 件を作り、2 件を書き換えます。",
    );
  });

  // 取り残しは本文にも一覧で出すが、帯は下端に固定しているので件数をここにも
  // 添える（作成ボタンの手前に出す約束、ADR 0026）。
  it("取り残しがあれば件数を添える", () => {
    expect(creationNotice({ create: 0, update: 1 }, TARGET, 1)).toBe(
      "更新は取り消せません。acme/web › #1 ロードマップ の draft issue 1 件を書き換えます。" +
        "前回作った 1 件は書き換わりません。",
    );
  });
});
