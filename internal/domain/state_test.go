package domain_test

import (
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
)

func ptr[T any](v T) *T { return &v }

// B-1, B-2, B-3: 3 状態判定の基本。
func TestDecideState(t *testing.T) {
	t.Parallel()

	const current domain.ContentHash = "aaa"

	tests := []struct {
		name   string
		latest *domain.ContentHash
		want   domain.SyncState
	}{
		{
			name:   "B-1 実行記録がなければ未作成",
			latest: nil,
			want:   domain.StateUncreated,
		},
		{
			name:   "B-2 ハッシュが一致すれば作成済み",
			latest: ptr(domain.ContentHash("aaa")),
			want:   domain.StateCreated,
		},
		{
			name:   "B-3 ハッシュが一致しなければ変更あり",
			latest: ptr(domain.ContentHash("bbb")),
			want:   domain.StateChanged,
		},
		{
			name:   "空ハッシュの記録も記録として扱う",
			latest: ptr(domain.ContentHash("")),
			want:   domain.StateChanged,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := domain.DecideState(tt.latest, current); got != tt.want {
				t.Errorf("DecideState() = %q, want %q", got, tt.want)
			}
		})
	}
}

// 実際のハッシュ値を通した往復確認。ボードを編集すると changed になり、
// 元に戻すと created に戻る。
func TestDecideState_WithComputedHashes(t *testing.T) {
	t.Parallel()

	original := []domain.TextElement{{ID: "a", Text: "決済フローを見直す"}}
	edited := []domain.TextElement{{ID: "a", Text: "決済フローを見直す（優先）"}}

	saved := domain.ComputeContentHash(original, domain.GranularityAuto)

	if got := domain.DecideState(&saved, domain.ComputeContentHash(original, domain.GranularityAuto)); got != domain.StateCreated {
		t.Errorf("編集前: %q, want %q", got, domain.StateCreated)
	}
	if got := domain.DecideState(&saved, domain.ComputeContentHash(edited, domain.GranularityAuto)); got != domain.StateChanged {
		t.Errorf("編集後: %q, want %q", got, domain.StateChanged)
	}
	if got := domain.DecideState(&saved, domain.ComputeContentHash(original, domain.GranularityAuto)); got != domain.StateCreated {
		t.Errorf("編集を戻した後: %q, want %q", got, domain.StateCreated)
	}
}
