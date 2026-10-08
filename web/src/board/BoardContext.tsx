import type { BoardRole, ProjectAccess } from "../api/types";
import { formatSceneSize } from "../excalidraw/size";
import { ROLE_LABELS } from "./members/roles";

type Props = {
  role: BoardRole;
  /** 作る先の見出し（「owner/repo › #1 Project」）。作成先が未選択なら null。 */
  targetLabel: string | null;
  /** `targetLabel` が null のときに出すリポジトリ名。 */
  repository: string;
  /** 作成先を GitHub で開く先。組めなければ null（`projectLink`）。 */
  linkHref: string | null;
  /** 開く先が Project 本体か（false ならリポジトリの Projects 一覧）。 */
  linkExact: boolean;
  projectAccess: ProjectAccess;
  targetLocked: boolean;
  /** いまのシーンのバイト数。まだ測っていなければ null。 */
  sceneBytes: number | null;
};

/**
 * 下に出す。**常に見えている状態だけ**（ADR 0064 / 0065）。押すものは置かない。
 * #230 で `BoardPage` から切り出し。
 *
 * Excalidraw の `Footer` に載る。**描くたびに作り直さない**（ADR 0065）。
 * 要素を `useMemo` で持つのは `BoardPage`。
 */
export function BoardContext({
  role,
  targetLabel,
  repository,
  linkHref,
  linkExact,
  projectAccess,
  targetLocked,
  sceneBytes,
}: Props) {
  return (
    <div className="board-context etoki-ui">
      {/*
        自分が何をできるのかは、操作して断られる前に見えている必要がある。
        共有すると「開けるが書けない」が普通に起きる（ADR 0017）。
      */}
      <span className="badge badge-role">{ROLE_LABELS[role]}</span>
      {/*
        どこに作られるのかは、作る直前ではなく常に見えている必要がある。
        作った draft issue は取り消せない（ADR 0009）。

        飛び先が組めるならリンクにする。取り消せない操作の結果を確かめる
        導線がここから始まる（ADR 0025）。組めないのは作成先が未選択の
        ボードだけなので、そのときはこれまでどおり文字のまま出す。

        **Project まで書く**（#217）。1 つのリポジトリに Project は複数ありうる
        ので、リポジトリ名だけでは作る先が決まらない。組み立ては詳細の帯の
        「作る先」（`targetLabel`）と同じものを使う。
      */}
      {linkHref !== null ? (
        <a
          className="badge badge-target"
          href={linkHref}
          target="_blank"
          rel="noreferrer"
          title={
            linkExact
              ? "作成先の Project を GitHub で開く"
              : "リポジトリの Projects を GitHub で開く"
          }
        >
          {targetLabel ?? repository}
        </a>
      ) : (
        <span className="badge badge-target">{targetLabel ?? repository}</span>
      )}
      {/*
        作成先の Project に書けるか（ADR 0017）。**書けないと分かったときだけ
        出す**（#217）。`unknown`（まだ確かめていない・確かめられなかった）を
        「書けません」に見せない。なぜ書けないかの本文は詳細の帯にある。
      */}
      {projectAccess === "denied" && (
        <span className="badge badge-denied">書けません</span>
      )}
      {/*
        作成先が固定済みかは**状態**なので、ここにも出す（#62 が読めなければ
        ならないものとして挙げている）。メニューの中の文は「作成先を変更」が
        無い理由で、閉じているあいだは読めない。役目が違うので両方に置く。
      */}
      {targetLocked && <span className="badge badge-locked">作成先は確定</span>}
      {/*
        いまの大きさを出す。**上限との比は出さない。** 比を出すには上限を
        フロントが知る必要があり、それは判定を 2 箇所に持つのと同じこと
        になる（ADR 0018 / 0038）。「大きいときだけ」出さないのも同じ
        理由で、上限を知らない以上どこからが大きいのかを決められない。
      */}
      {sceneBytes !== null && (
        <span className="badge badge-size" title="保存に送るシーンの大きさ">
          {formatSceneSize(sceneBytes)}
        </span>
      )}
    </div>
  );
}
