import { useEffect, useMemo, useState } from "react";

import type { BoardSummary } from "../api/types";
import {
  groupBoards,
  UNSELECTED_LABEL,
  type ProjectGroup,
  type RepositoryGroup,
} from "./grouping";

type Props = {
  boards: BoardSummary[];
  /** いま開いているボード。無ければ null。 */
  currentId: string | null;
  onOpen: (id: string) => void;
};

/**
 * ボードをリポジトリ → Project → ボードの入れ子で並べる（ADR 0019）。
 *
 * **既定はすべて開く。** 折りたたみは利用者が畳んだときだけ効く。初期状態で
 * 畳むと、どこに何があるかを見せないまま「探させる」ことになる（中核思想 3）。
 */
export function BoardTree({ boards, currentId, onOpen }: Props) {
  const groups = useMemo(() => groupBoards(boards), [boards]);
  // 畳んだ枝の鍵。持たない＝開いている。
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());

  // 開いたボードの枝は開ける。畳んだままだと、選んだボードが一覧から消える。
  // 畳む操作そのものは妨げない。畳めない枝を作ると、押しても何も起きない
  // ボタンになる。
  useEffect(() => {
    if (currentId === null) return;
    const board = boards.find((b) => b.id === currentId);
    if (!board) return;

    const repository = repositoryKeyOf(board);
    setCollapsed((prev) => {
      const next = new Set(prev);
      next.delete(repository);
      next.delete(projectKeyOf(repository, board.projectId));
      return next.size === prev.size ? prev : next;
    });
  }, [boards, currentId]);

  const toggle = (key: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (!next.delete(key)) next.add(key);
      return next;
    });

  return (
    <ul className="board-list board-tree">
      {groups.map((group) => (
        <li key={repositoryKeyOf(group)}>
          <RepositoryNode
            group={group}
            currentId={currentId}
            onOpen={onOpen}
            isOpen={(key) => !collapsed.has(key)}
            onToggle={toggle}
          />
        </li>
      ))}
    </ul>
  );
}

type NodeProps = {
  currentId: string | null;
  onOpen: (id: string) => void;
  isOpen: (key: string) => boolean;
  onToggle: (key: string) => void;
};

function RepositoryNode({
  group,
  currentId,
  onOpen,
  isOpen,
  onToggle,
}: NodeProps & { group: RepositoryGroup }) {
  const key = repositoryKeyOf(group);
  const open = isOpen(key);

  return (
    <>
      <Branch
        open={open}
        label={group.selected ? key : UNSELECTED_LABEL}
        onToggle={() => onToggle(key)}
      />

      {open && (
        <ul>
          {/*
            未選択の枝に Project は無い。見出しを 2 段重ねても「作成先なし」を
            繰り返すだけなので、ボードを直に並べる。
          */}
          {group.selected
            ? group.projects.map((project) => (
                <li key={project.projectId}>
                  <ProjectNode
                    project={project}
                    repositoryKey={key}
                    currentId={currentId}
                    onOpen={onOpen}
                    isOpen={isOpen}
                    onToggle={onToggle}
                  />
                </li>
              ))
            : group.projects.flatMap((project) =>
                project.boards.map((board) => (
                  <li key={board.id}>
                    <BoardButton
                      board={board}
                      current={board.id === currentId}
                      onOpen={onOpen}
                    />
                  </li>
                )),
              )}
        </ul>
      )}
    </>
  );
}

function ProjectNode({
  project,
  repositoryKey,
  currentId,
  onOpen,
  isOpen,
  onToggle,
}: NodeProps & { project: ProjectGroup; repositoryKey: string }) {
  const key = projectKeyOf(repositoryKey, project.projectId);
  const open = isOpen(key);

  return (
    <>
      <Branch open={open} label={project.label} onToggle={() => onToggle(key)} />

      {open && (
        <ul>
          {project.boards.map((board) => (
            <li key={board.id}>
              <BoardButton
                board={board}
                current={board.id === currentId}
                onOpen={onOpen}
              />
            </li>
          ))}
        </ul>
      )}
    </>
  );
}

/**
 * 折りたためる見出し。
 *
 * 三角は要素として置き、`aria-hidden` を付ける。CSS の `::before` で描くと
 * 読み上げ名に「▾ acme/web」と混ざる。開閉は `aria-expanded` が伝えるので、
 * 名前に入れる意味は無い。
 */
function Branch({
  open,
  label,
  onToggle,
}: {
  open: boolean;
  label: string;
  onToggle: () => void;
}) {
  return (
    <button type="button" className="tree-branch" aria-expanded={open} onClick={onToggle}>
      <span className="tree-mark" aria-hidden="true">
        {open ? "▾" : "▸"}
      </span>
      {label}
    </button>
  );
}

function BoardButton({
  board,
  current,
  onOpen,
}: {
  board: BoardSummary;
  current: boolean;
  onOpen: (id: string) => void;
}) {
  return (
    <button
      type="button"
      // 選択中であることを色だけで表さない。読み上げにも届かせる。
      aria-current={current ? "true" : undefined}
      className={current ? "active" : ""}
      onClick={() => onOpen(board.id)}
    >
      {board.name}
    </button>
  );
}

/**
 * 枝の鍵。
 *
 * ボードと枝の両方から同じ鍵を作れる必要がある。開いたボードの枝を開き直す
 * ときに、ボードから鍵を引くため。未選択はどちらも空文字にまとまる。
 */
function repositoryKeyOf(x: BoardSummary | RepositoryGroup): string {
  if ("projectId" in x) {
    return x.projectId === "" ? "" : `${x.repositoryOwner}/${x.repositoryName}`;
  }
  return x.selected ? `${x.repositoryOwner}/${x.repositoryName}` : "";
}

function projectKeyOf(repositoryKey: string, projectId: string): string {
  return `${repositoryKey} ${projectId}`;
}
