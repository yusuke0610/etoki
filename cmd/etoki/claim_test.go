package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// 引き受ける前に、login が当たった相手を見せて確かめる（ADR 0053、#142）。
// etoki が知っているのは「最後にその login でログインした人」までで、改名で
// 空いた login を取った別人かどうかは、表示名と最終ログインを見て人が決める。
func TestConfirmClaim(t *testing.T) {
	t.Parallel()

	user := port.User{
		ID: "user-1", Login: "bob", DisplayName: "Bob Example",
		UpdatedAt: time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC),
	}

	cases := map[string]struct {
		input string
		want  bool
	}{
		"y で進む":        {input: "y\n", want: true},
		"yes で進む":      {input: "yes\n", want: true},
		"大文字でも進む":      {input: "Y\n", want: true},
		"空は止める（既定は N）": {input: "\n", want: false},
		"n は止める":       {input: "n\n", want: false},
		"入力が無ければ止める":   {input: "", want: false},
		"それ以外は止める":     {input: "ok\n", want: false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			got := confirmClaim(strings.NewReader(tc.input), &out, user, 3)
			if got != tc.want {
				t.Errorf("confirmClaim(%q) = %v, want %v", tc.input, got, tc.want)
			}

			// 判断の材料が出ている。**ID と最終ログインを出す。** 表示名だけでは、
			// 同じ名前の別人を見分けられない。
			for _, want := range []string{"Bob Example", "@bob", "user-1", "2026-08-01", "3"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("確認に %q が出ていない:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestParseClaimArgs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args  []string
		login string
		yes   bool
		err   bool
	}{
		"login だけ":    {args: []string{"bob"}, login: "bob"},
		"--yes が前":    {args: []string{"--yes", "bob"}, login: "bob", yes: true},
		"--yes が後":    {args: []string{"bob", "--yes"}, login: "bob", yes: true},
		"login が無い":   {args: []string{"--yes"}, err: true},
		"login が 2 つ": {args: []string{"bob", "alice"}, err: true},
		"知らないフラグ":     {args: []string{"-f", "bob"}, err: true},
		"何も無い":        {args: nil, err: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			login, yes, err := parseClaimArgs(tc.args)
			if (err != nil) != tc.err {
				t.Fatalf("parseClaimArgs(%v) err = %v, want err %v", tc.args, err, tc.err)
			}
			if login != tc.login || yes != tc.yes {
				t.Errorf("parseClaimArgs(%v) = (%q, %v), want (%q, %v)",
					tc.args, login, yes, tc.login, tc.yes)
			}
		})
	}
}

// 取り消す前に、対象を見せて確かめる（ADR 0076）。既定は止める。
func TestConfirmRevoke(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	rows := []grantRow{
		{grant: port.OAuthGrant{
			ID: "grant-1", ClientName: "Claude Code", ClientID: "client-1",
			CreatedAt: at, LastUsedAt: at,
		}, login: "alice"},
		{grant: port.OAuthGrant{
			ID: "grant-2", ClientID: "https://client.example/meta.json",
			CreatedAt: at, LastUsedAt: at,
		}},
	}

	for input, want := range map[string]bool{
		"y\n": true, "yes\n": true, "Y\n": true, "\n": false, "n\n": false, "": false,
	} {
		var out bytes.Buffer
		if got := confirmRevoke(strings.NewReader(input), &out, rows); got != want {
			t.Errorf("confirmRevoke(%q) = %v, want %v", input, got, want)
		}
		for _, s := range []string{
			"2 件", "grant-1", "@alice", "Claude Code", "client-1",
			"grant-2", "https://client.example/meta.json", "2026-10-09",
		} {
			if !strings.Contains(out.String(), s) {
				t.Errorf("確認に %q が出ていない:\n%s", s, out.String())
			}
		}
	}
}

func TestParseRevokeArgs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args []string
		want revokeTarget
		err  bool
	}{
		"ID だけ":           {args: []string{"g1"}, want: revokeTarget{grantID: "g1"}},
		"--yes と ID":      {args: []string{"--yes", "g1"}, want: revokeTarget{grantID: "g1", yes: true}},
		"--login":         {args: []string{"--login", "bob"}, want: revokeTarget{login: "bob"}},
		"--login と --yes": {args: []string{"--login", "bob", "--yes"}, want: revokeTarget{login: "bob", yes: true}},
		"両方":              {args: []string{"g1", "--login", "bob"}, err: true},
		"どちらも無い":          {args: []string{"--yes"}, err: true},
		"ID が 2 つ":        {args: []string{"g1", "g2"}, err: true},
		"--login の値が無い":   {args: []string{"--login"}, err: true},
		"知らないフラグ":         {args: []string{"-f", "g1"}, err: true},
		"何も無い":            {args: nil, err: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := parseRevokeArgs(tc.args)
			if (err != nil) != tc.err {
				t.Fatalf("parseRevokeArgs(%v) err = %v, want err %v", tc.args, err, tc.err)
			}
			if got != tc.want {
				t.Errorf("parseRevokeArgs(%v) = %+v, want %+v", tc.args, got, tc.want)
			}
		})
	}
}

func TestParseGrantsArgs(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		args  []string
		login string
		err   bool
	}{
		"何も無い":          {args: nil},
		"--login":       {args: []string{"--login", "bob"}, login: "bob"},
		"--login の値が無い": {args: []string{"--login"}, err: true},
		"位置引数":          {args: []string{"bob"}, err: true},
		"--login が 2 つ": {args: []string{"--login", "a", "--login", "b"}, err: true},
	} {
		login, err := parseGrantsArgs(tc.args)
		if (err != nil) != tc.err || login != tc.login {
			t.Errorf("%s: parseGrantsArgs(%v) = (%q, %v), want (%q, err %v)",
				name, tc.args, login, err, tc.login, tc.err)
		}
	}
}
