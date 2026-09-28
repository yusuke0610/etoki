import { describe, expect, it } from "vitest";

import { boardLocationUrl, NO_BOARD, parseBoardLocation } from "./location";

describe("parseBoardLocation", () => {
  it("board が無ければ開いていない", () => {
    expect(parseBoardLocation("")).toEqual(NO_BOARD);
    expect(parseBoardLocation("?other=1")).toEqual(NO_BOARD);
  });

  // 空文字の board を「ID が空のボード」として通すと、開けない ID で
  // 404 のエラーを出すことになる。
  it("board が空文字なら開いていない", () => {
    expect(parseBoardLocation("?board=")).toEqual(NO_BOARD);
  });

  it("board を読む", () => {
    expect(parseBoardLocation("?board=board-1")).toEqual({
      boardId: "board-1",
      picking: false,
    });
  });

  it("picking=1 のときだけ選び直しとして読む", () => {
    expect(parseBoardLocation("?board=board-1&picking=1").picking).toBe(true);
    expect(parseBoardLocation("?board=board-1&picking=0").picking).toBe(false);
    expect(parseBoardLocation("?board=board-1&picking=yes").picking).toBe(false);
  });

  // 知らない param でボードが開けなくなる理由は無い。
  it("知らない param は無視する", () => {
    expect(parseBoardLocation("?utm_source=slack&board=board-1")).toEqual({
      boardId: "board-1",
      picking: false,
    });
  });

  it("? が付いていなくても読める", () => {
    expect(parseBoardLocation("board=board-1").boardId).toBe("board-1");
  });
});

describe("boardLocationUrl", () => {
  it("開いていなければ / ", () => {
    expect(boardLocationUrl(NO_BOARD)).toBe("/");
  });

  it("ボードを載せる", () => {
    expect(boardLocationUrl({ boardId: "board-1", picking: false })).toBe(
      "/?board=board-1",
    );
  });

  it("選び直しも載せる", () => {
    expect(boardLocationUrl({ boardId: "board-1", picking: true })).toBe(
      "/?board=board-1&picking=1",
    );
  });

  // ボードが無ければ選び直しも意味を持たない。載せると、開けないのに
  // 選び直しだけが立った URL ができる。
  it("ボードが無ければ picking は載らない", () => {
    expect(boardLocationUrl({ boardId: null, picking: true })).toBe("/");
  });

  // ID に URL で意味を持つ文字が入っても、読み直せる形で載る。
  it("ID を escape する", () => {
    const url = boardLocationUrl({ boardId: "a b&c=d", picking: false });
    expect(parseBoardLocation(url.slice(url.indexOf("?"))).boardId).toBe("a b&c=d");
  });
});

// 書いた URL をそのまま読み直せること。ここが割れると、自分で組み立てた
// URL を自分で開けない。
describe("往復", () => {
  it.each([
    { boardId: null, picking: false },
    { boardId: "board-1", picking: false },
    { boardId: "board-1", picking: true },
  ])("%o", (location) => {
    const url = boardLocationUrl(location);
    const search = url.includes("?") ? url.slice(url.indexOf("?")) : "";
    expect(parseBoardLocation(search)).toEqual(location);
  });
});
