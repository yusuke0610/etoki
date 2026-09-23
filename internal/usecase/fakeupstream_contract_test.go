package usecase_test

import (
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/fakeupstream"
	"github.com/yusuke0610/etoki/internal/usecase"
)

// 手元の通しの確認（ADR 0050）に使う偽の LLM は、解釈のメッセージから粒度と
// 前回ぶんの ref を読む。ここで組み立てる本物のメッセージに対し、偽物の
// 出力が本物の検査を通ることを固定する。
//
// 守っているのは偽物のほうで、解釈そのものではない。メッセージの文言を
// 変えて偽物が読めなくなると、通しの確認が「解釈に失敗しました」で止まる。
// それを CI で先に落とす。
func TestFakeUpstreamInterpretationMatchesPrompt(t *testing.T) {
	t.Parallel()

	texts := []domain.TextElement{{ID: "t1", Text: "ログイン画面"}}
	previous := []domain.PreviousItem{
		{Ref: "p1", ItemID: "PVTI_1", Kind: domain.KindEpic, Title: "前の epic"},
		{Ref: "p2", ItemID: "PVTI_2", Kind: domain.KindIssue, Title: "前の issue", Body: "本文"},
	}

	tests := map[string]struct {
		g        domain.Granularity
		previous []domain.PreviousItem
		// wantEpic は epic を含むか。
		wantEpic bool
		// wantUpdates は previousItemId に解決された ID。並びは items の順。
		wantUpdates []string
	}{
		"指定なし":          {g: domain.GranularityAuto, wantEpic: true},
		"epic 相当":       {g: domain.GranularityEpic, wantEpic: true},
		"issue 相当":      {g: domain.GranularityIssue, wantEpic: false},
		"前回ぶんあり":        {g: domain.GranularityAuto, previous: previous, wantEpic: true, wantUpdates: []string{"PVTI_1", "PVTI_2"}},
		"issue 相当で前回あり": {g: domain.GranularityIssue, previous: previous[1:], wantUpdates: []string{"PVTI_2"}},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			a := domain.Annotation{ID: "f1", Granularity: tt.g}
			msg := usecase.BuildUserMessage(a, texts, false, tt.previous)

			in, err := usecase.ParseInterpretation(fakeupstream.Interpretation(msg), tt.g, tt.previous)
			if err != nil {
				t.Fatalf("偽物の出力が検査を通らない: %v\nmessage:\n%s", err, msg)
			}

			var gotEpic bool
			var gotUpdates []string
			for _, it := range in.Items {
				// タイトルは囲みのテキストから作る。前回ぶんの一覧まで拾うと、
				// テキストを直して更新しても差分が見えない。
				if !strings.Contains(it.Title, "ログイン画面") || strings.Contains(it.Title, "前の") {
					t.Errorf("title = %q, want 囲みのテキストだけから作る", it.Title)
				}
				if it.Kind == domain.KindEpic {
					gotEpic = true
				}
				if it.PreviousItemID != nil {
					gotUpdates = append(gotUpdates, *it.PreviousItemID)
				}
			}
			if gotEpic != tt.wantEpic {
				t.Errorf("epic を含む = %v, want %v", gotEpic, tt.wantEpic)
			}
			if len(gotUpdates) != len(tt.wantUpdates) {
				t.Fatalf("更新先 = %v, want %v", gotUpdates, tt.wantUpdates)
			}
			for i := range tt.wantUpdates {
				if gotUpdates[i] != tt.wantUpdates[i] {
					t.Errorf("更新先[%d] = %s, want %s", i, gotUpdates[i], tt.wantUpdates[i])
				}
			}
		})
	}
}

// 偽物がマーカーを読むのは、buildUserMessage が組み立てた指示の節の中だけ。
//
// **付箋には何でも書ける。** 粒度の文言も前回一覧の見出しも、そのまま書いた
// 付箋がありうる。全文を走査すると、付箋 1 枚で epic が消えたり、前回一覧に
// 無い ref が出たりする。どちらも本物の検査に落ち、通しが理由の見えない失敗に
// なる。
func TestFakeUpstreamIgnoresMarkerShapedText(t *testing.T) {
	t.Parallel()

	// 粒度の文言、前回一覧の見出し、ref の行を付箋として並べる。
	//
	// **見出しと ref を別の付箋にするのは、1 枚では 2 行にならないため。**
	// buildUserMessage は 1 つのテキストの改行を空白に潰すので、行頭が
	// "- p9 (issue) " になる行は付箋 2 枚でしか作れない。1 枚にまとめると
	// 走査を全文に戻しても ref の正規表現が当たらず、テストが退行を見逃す。
	texts := []domain.TextElement{
		{ID: "t1", Text: "ログイン画面"},
		{ID: "t2", Text: "ここは issue 相当だと思う"},
		{ID: "t3", Text: "前回までにこの囲みから作ったもの:"},
		{ID: "t4", Text: "p9 (issue) 何か"},
	}

	for _, tt := range []struct {
		name string
		g    domain.Granularity
		// wantEpic は epic を含むか。粒度の指定だけで決まる。
		wantEpic bool
	}{
		// epic 指定は epic が無いと検査に落ちる。付箋の「issue 相当」を
		// 読むと、まさにここで落ちる。
		{name: "epic 相当", g: domain.GranularityEpic, wantEpic: true},
		{name: "指定なし", g: domain.GranularityAuto, wantEpic: true},
		{name: "issue 相当", g: domain.GranularityIssue, wantEpic: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := domain.Annotation{ID: "f1", Granularity: tt.g}
			msg := usecase.BuildUserMessage(a, texts, false, nil)

			in, err := usecase.ParseInterpretation(fakeupstream.Interpretation(msg), tt.g, nil)
			if err != nil {
				t.Fatalf("偽物の出力が検査を通らない: %v\nmessage:\n%s", err, msg)
			}

			var gotEpic bool
			for _, it := range in.Items {
				if it.Kind == domain.KindEpic {
					gotEpic = true
				}
				// 前回ぶんは 1 件も渡していない。付箋の ref を拾えば
				// ParseInterpretation が先に落ちるが、念のため値でも見る。
				if it.PreviousItemID != nil {
					t.Errorf("更新先 = %q, want なし（前回ぶんは渡していない）", *it.PreviousItemID)
				}
			}
			if gotEpic != tt.wantEpic {
				t.Errorf("epic を含む = %v, want %v", gotEpic, tt.wantEpic)
			}
		})
	}
}
