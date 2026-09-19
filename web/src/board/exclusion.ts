import { useCallback, useMemo, useRef, useState } from "react";

/**
 * 同時に走らせない操作と、押せない理由（#146）。
 *
 * **排他はここだけを読めば分かる形にする。** 以前は `exclusiveOperation`（ref）・
 * `saving` / `importing`（state）・`creations` からの導出（`creating`）・`placing`
 * （ref）に分かれていて、**どの操作とどの操作が排他かを知るには `BoardPage` を
 * 通して読む**ことになっていた。押せない理由の文言も、ヘッダーと注釈パネルで
 * 別々に組み直していた。
 *
 * **守っている対象が違う 2 つを、区別したうえで同じ場所に置く。**
 *
 * | 何を守るか                             | ここでの名前                     |
 * | -------------------------------------- | -------------------------------- |
 * | 取り消せない操作どうしを並走させない   | `useExclusion`（表は `BLOCKED`） |
 * | 同じ対象への二重押しを弾く             | `useReentryGuard`                |
 *
 * 前者は「保存と作成」のように**別の操作**の組、後者は「同じ図を 2 回置く」の
 * ように**同じ操作**の連打。まとめると、塞いだのがどちらの穴なのかが読めなく
 * なる。
 */

/**
 * 走っているあいだ、他を止める操作。
 *
 * **この 3 つは互いに排他で、同じ操作の二重起動も弾く。** 保存はシーンを書き、
 * 取り込みはキャンバスを置き換え、作成は GitHub に取り消せないものを作る
 * （ADR 0009）。
 *
 * - **作成中に保存させない。** 実行中にシーンが変わると、作られた内容と記録
 *   される `content_hash` が食い違いうる。
 * - **保存中に作成させない。** 保存は `creations` を捨てるので、GitHub には
 *   残ったまま結果だけ消える。作られていないと思って再実行した開発者が
 *   draft issue を重複させる。
 * - **取り込みはキャンバスを丸ごと置き換える**ので、保存とも作成とも同じ
 *   理由で並走させない。
 *
 * （`.claude/rules/async-ui.md`「取り消せない操作と保存は相互に排他する」）
 */
export type RunningOperation = "saving" | "importing" | "creating";

/**
 * 押せない理由を引ける操作。
 *
 * **`changeTarget` は走らない。** 作成先を選び直すとキャンバスごと外れる
 * （導線を持っているのは `App`）ので、`BoardPage` の中で進行中になることが
 * 無い。止められる側にしか現れないが、**押せない理由を組む場所を 2 つに
 * 分けない**ためにここへ並べてある。
 */
export type Operation = RunningOperation | "changeTarget";

/**
 * 何が走っているとき、何を始められないか。**この表がすべて。**
 *
 * 値はそのまま画面に出す本文。押せない理由は `title` に隠さず、ボタンから
 * `aria-describedby` で指す（ADR 0039）ので、**文言も判定と同じ場所に持つ。**
 * 別に持つと、止める条件だけ変えて文言が古いまま残る。
 *
 * **句点の有無が揃っていないのは意図ではなく現状。** 作成の理由だけが注釈
 * パネルの `<p>` に出るので「。」で終わり、ヘッダーの `<span>` に出る 3 つには
 * 無い。**この PR では字面を動かさない**（`web/e2e/a11y.spec.ts` が文言で
 * 引いている）。揃えるなら E2E の期待文言と同じコミットで動かす。
 *
 * **対角（走っているものと同じ操作）は null。** ボタン自身が「保存中…」
 * 「取り込み中…」「作成中…」と名乗っているので、隣に「保存が終わるまで保存
 * できません」と出しても読む人の打ち手は増えない。**弾くこと自体は
 * `useExclusion` が行う**ので、null は「通す」という意味ではない。
 */
const BLOCKED: Record<Operation, Record<RunningOperation, string | null>> = {
  saving: {
    saving: null,
    importing: "取り込みが終わるまで保存できません",
    creating: "作成が終わるまで保存できません",
  },
  importing: {
    saving: "保存が終わるまで取り込めません",
    importing: null,
    creating: "作成が終わるまで取り込めません",
  },
  creating: {
    saving: "保存が終わるまで作成できません。",
    importing: "取り込みが終わるまで作成できません。",
    // **ここだけは「名乗っているから null」ではない。** 作成中に *別の* 注釈の
    // 作成ボタンを押せてしまい、押しても `useExclusion` が黙って弾く。理由を
    // 出すと押せないボタンが 1 つ増えるので、a11y の spec も一緒に足す必要が
    // ある。**この PR では現状のまま**にし、出すかどうかは別に判断する（#146）。
    creating: null,
  },
  changeTarget: {
    saving: "保存が終わるまで作成先を変更できません",
    // 取り込みと作成の最中は、いまも作成先の変更を止めていない。**キャンバスが
    // 外れる導線なので止める理由はある**が、止めると押せないボタンが増え、
    // 理由を出す先も要る。現状を写しておき、変えるかどうかは別に判断する。
    importing: null,
    creating: null,
  },
};

/**
 * `op` を始められない理由。始められる、または理由を出さないなら null。
 *
 * **判定は 1 引数（いま何が走っているか）だけで決まる。** `dirty` のような
 * 操作以外の前提は入れない。入れると、進行中かどうかを知らない純関数
 * （`blockingReasons`）と同じ罠に落ちる。
 */
export function blockedReason(
  op: Operation,
  running: RunningOperation | null,
): string | null {
  return running === null ? null : BLOCKED[op][running];
}

export type Exclusion = {
  /** いま走っている操作。何も走っていなければ null。**表示に使う。** */
  running: RunningOperation | null;
  /** `op` を始められない理由。押せる、または理由を出さないなら null。 */
  reasonFor: (op: Operation) => string | null;
  /**
   * 排他を取って `f` を走らせる。**取れなければ `f` を呼ばずに false を返す。**
   *
   * 取った / 取れなかったの判定は state ではなく ref で行う。state だと同じ
   * tick に届いた 2 回目がまだ「何も走っていない」を読み、2 本が並走する
   * （`.claude/rules/async-ui.md`）。**押させない側（`disabled`）は代わりに
   * ならない。**
   */
  run: (op: RunningOperation, f: () => Promise<void>) => Promise<boolean>;
};

/** 排他を 1 つだけ持つ。**BoardPage に 1 つ。** */
export function useExclusion(): Exclusion {
  // 表示用。**書くのは必ず `run` を通す。**
  const [running, setRunning] = useState<RunningOperation | null>(null);
  // 判定用。押した時点の値が要るので state とは別に持つ。
  const current = useRef<RunningOperation | null>(null);

  const run = useCallback(
    async (op: RunningOperation, f: () => Promise<void>): Promise<boolean> => {
      if (current.current !== null) return false;
      current.current = op;
      setRunning(op);

      try {
        await f();
        return true;
      } finally {
        current.current = null;
        setRunning(null);
      }
    },
    [],
  );

  const reasonFor = useCallback((op: Operation) => blockedReason(op, running), [running]);

  // **同一性を保つ。** 呼ぶ側は `exclusive.run` を useCallback の依存に置くので、
  // 毎レンダーで作り直すと、排他を取るだけのコールバックまで作り直される。
  return useMemo(() => ({ running, reasonFor, run }), [running, reasonFor, run]);
}

export type ReentryGuard = {
  /** 入れたら true。すでに入っているなら false。 */
  enter: (key?: string) => boolean;
  /** 出る。**必ず `finally` で呼ぶ。** */
  leave: (key?: string) => void;
};

/**
 * 同じ対象への二重押しを弾く。
 *
 * **排他の表とは別物。** あちらは「保存中は作成させない」のように*違う操作*の
 * 組を止める。こちらが止めるのは同じ操作の連打で、対象ごとに独立している
 * （注釈 A の履歴を読んでいても、注釈 B の履歴は読める）。
 *
 * ref で覚えるのは排他と同じ理由。state だと同じ tick に届いた 2 回目がまだ
 * false を読み、同じ図が同じ場所に重なって置かれる（#108）。
 *
 * 対象が 1 つしか無い操作（図を置く）は key を省く。
 */
export function useReentryGuard(): ReentryGuard {
  const entered = useRef(new Set<string>());

  const enter = useCallback((key = "") => {
    if (entered.current.has(key)) return false;
    entered.current.add(key);
    return true;
  }, []);

  const leave = useCallback((key = "") => {
    entered.current.delete(key);
  }, []);

  return useMemo(() => ({ enter, leave }), [enter, leave]);
}
