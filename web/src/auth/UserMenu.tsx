import { useEffect, useId, useRef, useState } from "react";

import type { AuthUser } from "../api/types";
import { initialsOf } from "./initials";

type Props = {
  user: AuthUser;
  onLogout: () => void;
};

/**
 * 一覧の上の帯の右に置く、利用者のメニュー（#200）。中身は「ログアウト」だけ。
 *
 * **開くまでしまう。** ログアウトは毎回押すものではないので、帯に並べておく
 * 理由が無い。しまっても、誰としてログインしているかはボタンの名前で常に読める。
 *
 * **メニュー（`role="menu"`）ではなく開閉（disclosure）の形にする。** 項目が
 * 1 つしか無く、矢印キーで項目を渡る作りを持つほどではない。開いた中身は
 * ふつうのボタンで、Tab で届く。
 */
export function UserMenu({ user, onLogout }: Props) {
  const [open, setOpen] = useState(false);
  const panelId = useId();
  const root = useRef<HTMLDivElement>(null);
  const toggle = useRef<HTMLButtonElement>(null);
  // 名前が空の利用者もいる（GitHub の表示名は任意）。丸を空にしないよう login で埋める。
  const name = user.displayName.trim() === "" ? user.login : user.displayName;

  useEffect(() => {
    if (!open) return;

    // **外側を押したら閉じるが、焦点は動かさない。** 押した先（カードなど）に
    // 焦点が移るのが自然で、ボタンへ引き戻すとその操作を横取りする。
    const onPointerDown = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false);
    };
    // Esc は焦点をボタンへ戻す。開いた中身の中で押されると、閉じた瞬間に
    // 焦点の行き場が無くなる。
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setOpen(false);
      toggle.current?.focus();
    };
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  return (
    <div className="user-menu" ref={root}>
      <button
        ref={toggle}
        type="button"
        className="user-menu-toggle"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen((v) => !v)}
      >
        {/* 頭文字は飾り。名前は隣の文字が持つ。 */}
        <span className="user-menu-avatar" aria-hidden="true">
          {initialsOf(name)}
        </span>
        <span className="user-menu-name">{name}</span>
      </button>
      <div id={panelId} className="user-menu-panel" hidden={!open}>
        <button type="button" onClick={onLogout}>
          ログアウト
        </button>
      </div>
    </div>
  );
}
