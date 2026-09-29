import { useMemo, useState } from "react";

import type { BoardSummary } from "../api/types";
import {
  groupBoards,
  UNSELECTED_LABEL,
  type ProjectGroup,
  type RepositoryGroup,
} from "./grouping";

type Props = {
  boards: BoardSummary[];
  onOpen: (id: string) => void;
};

/**
 * ボードをリポジトリ → Project → ボードの入れ子で並べる（ADR 0019）。
 *
 * **既定はすべて開く。** 折りたたみは利用者が畳んだときだけ効く。初期状態で
 * 畳むと、どこに何があるかを見せないまま「探させる」ことになる（中核思想 3）。
 *
 * **「いま開いているボード」は持たない。** 一覧はボードとは別の画面で
 * （ADR 0064）、ボードを開くとこの木ごと外れる。開いているボードが一覧と
 * 同時に画面に出ることが無い。
 */
export function BoardTree({ boards, onOpen }: Props) {
  const groups = useMemo(() => groupBoards(boards), [boards]);
  // 畳んだ枝の鍵。持たない＝開いている。
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());

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
  onOpen: (id: string) => void;
  isOpen: (key: string) => boolean;
  onToggle: (key: string) => void;
};

function RepositoryNode({
  group,
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
                    onOpen={onOpen}
                    isOpen={isOpen}
                    onToggle={onToggle}
                  />
                </li>
              ))
            : group.projects.flatMap((project) =>
                project.boards.map((board) => (
                  <li key={board.id}>
                    <BoardButton board={board} onOpen={onOpen} />
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
              <BoardButton board={board} onOpen={onOpen} />
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
  onOpen,
}: {
  board: BoardSummary;
  onOpen: (id: string) => void;
}) {
  return (
    <button type="button" onClick={() => onOpen(board.id)}>
      {board.name}
    </button>
  );
}

/** 枝の鍵。作成先が未選択の枝は空文字にまとまる。 */
function repositoryKeyOf(group: RepositoryGroup): string {
  return group.selected ? `${group.repositoryOwner}/${group.repositoryName}` : "";
}

function projectKeyOf(repositoryKey: string, projectId: string): string {
  return `${repositoryKey} ${projectId}`;
}
