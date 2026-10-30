package port_test

import (
	"testing"

	"github.com/yusuke0610/etoki/port"
)

// ロールの上下を知っているのは BoardRole だけ（ADR 0017）。認可の入口は
// すべてここを通るので、並びと未定義の扱いをここで直接固定する。usecase の
// テストは間接に通るが、未定義のロールの経路は通っていない。

func TestBoardRole_AtLeast(t *testing.T) {
	roles := []port.BoardRole{port.RoleOwner, port.RoleEditor, port.RoleViewer}
	// want[r][min]: r が min 以上か。owner > editor > viewer。
	want := map[port.BoardRole]map[port.BoardRole]bool{
		port.RoleOwner:  {port.RoleOwner: true, port.RoleEditor: true, port.RoleViewer: true},
		port.RoleEditor: {port.RoleOwner: false, port.RoleEditor: true, port.RoleViewer: true},
		port.RoleViewer: {port.RoleOwner: false, port.RoleEditor: false, port.RoleViewer: true},
	}
	for _, r := range roles {
		for _, min := range roles {
			if got := r.AtLeast(min); got != want[r][min] {
				t.Errorf("%q.AtLeast(%q) = %v, want %v", r, min, got, want[r][min])
			}
		}
	}
}

// 未定義のロールは、どちらの側に来ても通さない。「とりあえず通す」方に
// 倒すと、保存先に紛れ込んだ値や綴り違いが権限になる。
func TestBoardRole_AtLeast_UndefinedIsNeverEnough(t *testing.T) {
	undefined := []port.BoardRole{"", "admin", "Owner", "owner "}
	defined := []port.BoardRole{port.RoleOwner, port.RoleEditor, port.RoleViewer}
	for _, u := range undefined {
		for _, d := range defined {
			if u.AtLeast(d) {
				t.Errorf("%q.AtLeast(%q) = true, want false", u, d)
			}
			if d.AtLeast(u) {
				t.Errorf("%q.AtLeast(%q) = true, want false", d, u)
			}
		}
		if u.AtLeast(u) {
			t.Errorf("%q.AtLeast(%q) = true, want false", u, u)
		}
	}
}

func TestBoardRole_Valid(t *testing.T) {
	for _, r := range []port.BoardRole{port.RoleOwner, port.RoleEditor, port.RoleViewer} {
		if !r.Valid() {
			t.Errorf("%q.Valid() = false, want true", r)
		}
	}
	for _, r := range []port.BoardRole{"", "admin", "Owner", "owner "} {
		if r.Valid() {
			t.Errorf("%q.Valid() = true, want false", r)
		}
	}
}
