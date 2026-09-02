import type {
  Granularity,
  Interpretation,
  InterpretedItem,
  ItemKind,
  SyncItem,
} from "../api/types";

/** 解釈結果の 1 件と、それを作るかどうか。 */
export type DraftItem = {
  item: InterpretedItem;
  selected: boolean;

  /**
   * 前回作ったものを書き換えるか、新しく作るか。
   *
   * **`item.previousItemId` は LLM が答えたままにしておく。** 消して表すと
   * 「新しく作る」に倒した操作を戻せず、LLM が何と答えたのかも読めなくなる。
   * 対応づけを解釈させたうえで**決めるのは開発者**（ADR 0026）なので、
   * 答えのほうは残したまま、従うかどうかだけをここで持つ。
   *
   * 既定は LLM の答え。**触られたときだけ変わる。**
   */
  updatesPrevious: boolean;
};

/**
 * 作成前に開発者が手を入れた解釈結果。
 *
 * `summary` と `contentHash` は持ち回るだけで編集させない。`summary` は GitHub に
 * 作らない確認材料（ADR 0006）、`contentHash` は解釈時点の保存済みシーンを指す
 * 値で、フロントが組み立ててよいものではない（ADR 0010）。
 */
export type Draft = {
  summary: string;
  contentHash: string;
  items: DraftItem[];
};

/** 解釈結果をそのまま下書きにする。既定は全件作る。 */
export function createDraft(result: Interpretation): Draft {
  return {
    summary: result.summary,
    contentHash: result.contentHash,
    items: result.items.map((item) => ({
      item,
      selected: true,
      updatesPrevious: Boolean(item.previousItemId),
    })),
  };
}

/**
 * その項目が実際に持てる親の localId。持てないなら undefined。
 *
 * **この 1 つの規則で、親を失う 3 つの経路がまとめて片づく。** 親の epic を
 * 選択から外した / 親を issue に変えた / そもそも親が指定されていない。
 *
 * サーバーの `Interpretation.Validate` は、指す先の無い parentLocalId も、
 * epic が親を持つことも、issue を親にすることも拒む
 * （`internal/domain/interpretation.go`）。ここを通した親だけを送れば、
 * 画面から作れる組み合わせがその検証に引っかかることはない。
 */
function effectiveParentOf(
  items: DraftItem[],
  target: InterpretedItem,
): string | undefined {
  // epic は親を持てない。kind を epic に変えた項目もここで落ちる。
  if (target.kind === "epic") return undefined;

  const parentLocalId = target.parentLocalId;
  if (parentLocalId === undefined) return undefined;

  const parent = items.find((d) => d.item.localId === parentLocalId);
  if (!parent) return undefined;
  if (!parent.selected) return undefined;
  if (parent.item.kind !== "epic") return undefined;

  return parentLocalId;
}

/**
 * 作る / 作らないを切り替える。
 *
 * **epic を外すと、その epic を親に持つ issue も一緒に外れる。** 親だけ消えて
 * 子が残ると、開発者が選んだつもりのない「親なしの issue」が GitHub にできる。
 *
 * 戻すときは連動させない。外したものを勝手に戻すと、外した操作がどこまで
 * 効いているのか分からなくなる。子は 1 件ずつ戻せる（戻したものは親なしの
 * issue として作られる。それは画面に出す）。
 */
export function toggleItem(draft: Draft, localId: string): Draft {
  const target = draft.items.find((d) => d.item.localId === localId);
  if (!target) return draft;

  const selected = !target.selected;
  const cascades = !selected && target.item.kind === "epic";

  return {
    ...draft,
    items: draft.items.map((d) => {
      if (d.item.localId === localId) return { ...d, selected };
      if (cascades && d.item.parentLocalId === localId) return { ...d, selected: false };
      return d;
    }),
  };
}

/** 1 件だけを差し替える。 */
function editItem(
  draft: Draft,
  localId: string,
  edit: (item: InterpretedItem) => InterpretedItem,
): Draft {
  return {
    ...draft,
    items: draft.items.map((d) =>
      d.item.localId === localId ? { ...d, item: edit(d.item) } : d,
    ),
  };
}

/**
 * 種別を変える。
 *
 * **`parentLocalId` は消さない。** epic に変えた項目の親は送信時に落ちるだけ
 * なので、issue に戻せば親も戻る。ここで消すと、変えた操作が取り消せなくなる。
 */
export function setKind(draft: Draft, localId: string, kind: ItemKind): Draft {
  return editItem(draft, localId, (item) => ({ ...item, kind }));
}

export function setTitle(draft: Draft, localId: string, title: string): Draft {
  return editItem(draft, localId, (item) => ({ ...item, title }));
}

export function setBody(draft: Draft, localId: string, body: string): Draft {
  return editItem(draft, localId, (item) => ({ ...item, body }));
}

/**
 * 前回作ったものを書き換えるか、新しく作るかを決める。
 *
 * **LLM が対応づけた先が GitHub から消えていると、更新のままでは作成が必ず
 * 失敗する。** その item は畳み込みに残り続けるので、解釈をやり直しても同じ
 * ところで止まりうる。ここが無いと、出口はその項目ごと外すことしか無い。
 *
 * **`previousItemId` は書き換えない。** 戻せる形にしておく（`DraftItem`）。
 * LLM が「新しく作る」と答えた項目には指す先が無いので、true に倒しても
 * 送るものは増えない（`buildInterpretation`）。
 */
export function setUpdatesPrevious(
  draft: Draft,
  localId: string,
  updatesPrevious: boolean,
): Draft {
  return {
    ...draft,
    items: draft.items.map((d) =>
      d.item.localId === localId ? { ...d, updatesPrevious } : d,
    ),
  };
}

/**
 * 作成リクエストに載せる内容。選んだものだけを、持てる親だけつけて返す。
 *
 * 項目は 1 つずつ組み立て直す。まるごと写すと、契約に増えたフィールドが
 * 黙って通り抜ける。ここが落ちるなら、増えたフィールドを画面で扱うかどうかを
 * 決めるべきところ。
 */
export function buildInterpretation(draft: Draft): Interpretation {
  const items = draft.items
    .filter((d) => d.selected)
    .map((d) => {
      const parentLocalId = effectiveParentOf(draft.items, d.item);
      const item: InterpretedItem = {
        localId: d.item.localId,
        kind: d.item.kind,
        title: d.item.title,
        body: d.item.body,
      };
      if (parentLocalId !== undefined) item.parentLocalId = parentLocalId;
      // 対応づけは開発者が確かめたものを送り返す。**新しく作るに倒したら
      // 送らない**（ADR 0026）。サーバー側の検査（previous_item_unknown）は
      // そのまま効く。
      if (d.updatesPrevious && d.item.previousItemId) {
        item.previousItemId = d.item.previousItemId;
      }
      return item;
    });

  return { summary: draft.summary, contentHash: draft.contentHash, items };
}

/**
 * 親を失ったまま作られる issue の localId。
 *
 * 画面に出すために持つ。**構造が変わったことを黙って起こさない**のが目的で、
 * 便利のための表示ではない。
 */
export function orphanedLocalIds(draft: Draft): Set<string> {
  const out = new Set<string>();

  for (const d of draft.items) {
    if (!d.selected) continue;
    // epic はもともと親を持たない。持っていた parentLocalId は無視される。
    if (d.item.kind === "epic") continue;
    if (d.item.parentLocalId === undefined) continue;
    if (effectiveParentOf(draft.items, d.item) === undefined) out.add(d.item.localId);
  }

  return out;
}

/**
 * このまま作成させない理由。空なら押させてよい。
 *
 * **正本はサーバーの `Interpretation.Validate`。** ここに置いてあるのは、画面から
 * 作れてしまう組み合わせを押す前に止めるためのもので、判定を移したわけではない。
 * 食い違ったときに起きるのは「押せて 400 が返る」までで、画面が通したものが
 * 検証を素通りすることはない。
 *
 * 粒度が issue の注釈では kind を変えさせないので、「issue 指定なのに epic が
 * ある」はここに要らない。
 */
export function blockingReasons(draft: Draft, granularity: Granularity): string[] {
  const selected = draft.items.filter((d) => d.selected);
  const reasons: string[] = [];

  if (selected.length === 0) {
    reasons.push("作るものが 1 件も選ばれていません。");
  }
  if (selected.some((d) => d.item.title.trim() === "")) {
    reasons.push("タイトルが空の項目があります。");
  }
  if (granularity === "epic" && !selected.some((d) => d.item.kind === "epic")) {
    reasons.push(
      "粒度に epic を指定しているので、epic を少なくとも 1 件選んでください。",
    );
  }

  return reasons;
}

/**
 * 今回の作成で取り残される draft issue の itemId（ADR 0026）。
 *
 * 前回までに作ったもののうち、選ばれている項目のどれからも指されていないもの。
 *
 * **消す判断はしない。** GitHub の draft issue は削除できないので、etoki に
 * できるのは「これは GitHub 側に残ります」と見せるところまで。黙って落とすと、
 * 開発者は自分が何を置き去りにしたのかを確かめられない（中核思想 3）。
 *
 * **新しく作るに倒した項目の更新先も取り残しに戻る。** 選択を外したときと
 * 同じ扱いにする。どちらも「そこへは書かない」という同じ結果になるので、
 * 片方だけ取り残しに出さないと、押す前に見せている数が実際と食い違う。
 */
export function leftBehindItemIds(draft: Draft, previous: SyncItem[]): Set<string> {
  const claimed = new Set(
    draft.items
      .filter((d) => d.selected && d.updatesPrevious && d.item.previousItemId)
      .map((d) => d.item.previousItemId as string),
  );

  return new Set(previous.map((it) => it.itemId).filter((id) => !claimed.has(id)));
}
