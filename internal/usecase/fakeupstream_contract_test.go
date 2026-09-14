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
