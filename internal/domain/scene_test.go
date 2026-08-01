package domain_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
)

// scene は要素をつないでシーン JSON を組み立てる。実際に Excalidraw が
// 吐く形に合わせ、JSON を経由してパースまで通す。
func scene(t *testing.T, elements ...string) domain.Scene {
	t.Helper()

	raw := `{"type":"excalidraw","elements":[` + strings.Join(elements, ",") + `]}`
	s, err := domain.ParseScene([]byte(raw))
	if err != nil {
		t.Fatalf("ParseScene: %v", err)
	}
	return s
}

// textIDs は抽出結果を ID の昇順で返す。並び順に依存しない比較のため。
func textIDs(texts []domain.TextElement) []string {
	ids := make([]string, 0, len(texts))
	for _, t := range texts {
		ids = append(ids, t.ID)
	}
	slices.Sort(ids)
	return ids
}

func textByID(texts []domain.TextElement, id string) (domain.TextElement, bool) {
	for _, t := range texts {
		if t.ID == id {
			return t, true
		}
	}
	return domain.TextElement{}, false
}

const (
	// etoki の注釈である frame。
	annotFrame = `{"id":"annot-1","type":"frame","name":"決済まわり",
	                "customData":{"etoki":{"granularity":"epic"}}}`
	// ユーザーがブレスト中に使った、注釈ではない frame。
	plainFrame = `{"id":"plain-frame","type":"frame","name":"ただの枠"}`
	// 注釈の直接の子であるテキスト。
	childText = `{"id":"t1","type":"text","text":"Stripe の SDK が古い","frameId":"annot-1"}`
	// 注釈の子である図形と、そこに紐づくラベル。
	childShape = `{"id":"shape-1","type":"rectangle","frameId":"annot-1"}`
	boundLabel = `{"id":"t2","type":"text","text":"Webhook の冪等化","containerId":"shape-1"}`
	// 注釈の外にあるテキスト。
	outsideText = `{"id":"t3","type":"text","text":"関係ない付箋"}`
)

// 注釈の判定：customData.etoki を持つ frame だけが注釈になる。
func TestAnnotations(t *testing.T) {
	t.Parallel()

	s := scene(t, annotFrame, plainFrame, childText, outsideText)

	got := s.Annotations()
	if len(got) != 1 {
		t.Fatalf("注釈の件数 = %d, want 1 (%+v)", len(got), got)
	}
	if got[0].ID != "annot-1" {
		t.Errorf("ID = %q, want %q", got[0].ID, "annot-1")
	}
	if got[0].Name != "決済まわり" {
		t.Errorf("Name = %q", got[0].Name)
	}
	if got[0].Granularity != domain.GranularityEpic {
		t.Errorf("Granularity = %q, want %q", got[0].Granularity, domain.GranularityEpic)
	}
}

// ブレスト中に使われた素の frame は注釈にしない。
func TestAnnotations_IgnoresPlainFrame(t *testing.T) {
	t.Parallel()

	if got := scene(t, plainFrame).Annotations(); len(got) != 0 {
		t.Errorf("注釈の件数 = %d, want 0 (%+v)", len(got), got)
	}
}

// 削除済みの注釈は拾わない。
func TestAnnotations_IgnoresDeleted(t *testing.T) {
	t.Parallel()

	deleted := `{"id":"annot-1","type":"frame","isDeleted":true,
	             "customData":{"etoki":{"granularity":"epic"}}}`

	if got := scene(t, deleted).Annotations(); len(got) != 0 {
		t.Errorf("注釈の件数 = %d, want 0", len(got))
	}
}

// 粒度未指定（customData.etoki はあるが granularity が空）は auto として扱う。
func TestAnnotations_MissingGranularityIsAuto(t *testing.T) {
	t.Parallel()

	noGranularity := `{"id":"annot-1","type":"frame","customData":{"etoki":{}}}`

	got := scene(t, noGranularity).Annotations()
	if len(got) != 1 {
		t.Fatalf("注釈の件数 = %d, want 1", len(got))
	}
	if got[0].Granularity != domain.GranularityAuto {
		t.Errorf("Granularity = %q, want auto", got[0].Granularity)
	}
}

// 抽出の中核：frameId の直接の子と、containerId で図形に紐づくラベルの両方を集める。
//
// 後者を辿らないと、図形の中に書かれた文字がまるごとハッシュから抜ける。
// frame の名前も入力に含める。
func TestAnnotationTexts_FollowsFrameAndContainer(t *testing.T) {
	t.Parallel()

	s := scene(t, annotFrame, childText, childShape, boundLabel, outsideText)

	got := s.AnnotationTexts("annot-1")

	want := []string{"annot-1", "t1", "t2"} // frame 名 + 直接の子 + ラベル
	if diff := textIDs(got); !slices.Equal(diff, want) {
		t.Fatalf("抽出された ID = %v, want %v", diff, want)
	}

	if label, ok := textByID(got, "t2"); !ok || label.Text != "Webhook の冪等化" {
		t.Errorf("ラベルの本文 = %+v", label)
	}
	if name, ok := textByID(got, "annot-1"); !ok || name.Text != "決済まわり" {
		t.Errorf("frame 名 = %+v", name)
	}
}

// 注釈の外のテキストは拾わない。
func TestAnnotationTexts_ExcludesOutside(t *testing.T) {
	t.Parallel()

	s := scene(t, annotFrame, childText, outsideText)

	if ids := textIDs(s.AnnotationTexts("annot-1")); slices.Contains(ids, "t3") {
		t.Errorf("外のテキストを拾っている: %v", ids)
	}
}

// 別の注釈に属するテキストは混ざらない。
func TestAnnotationTexts_IsolatesAnnotations(t *testing.T) {
	t.Parallel()

	other := `{"id":"annot-2","type":"frame","customData":{"etoki":{}}}`
	otherText := `{"id":"t9","type":"text","text":"別の囲み","frameId":"annot-2"}`

	s := scene(t, annotFrame, childText, other, otherText)

	if ids := textIDs(s.AnnotationTexts("annot-1")); slices.Contains(ids, "t9") {
		t.Errorf("別の注釈のテキストが混ざっている: %v", ids)
	}
	if ids := textIDs(s.AnnotationTexts("annot-2")); !slices.Equal(ids, []string{"t9"}) {
		t.Errorf("annot-2 の抽出 = %v, want [t9]", ids)
	}
}

// 削除済みの要素は抽出しない。
func TestAnnotationTexts_IgnoresDeleted(t *testing.T) {
	t.Parallel()

	deletedText := `{"id":"t8","type":"text","text":"消した付箋","frameId":"annot-1","isDeleted":true}`

	s := scene(t, annotFrame, childText, deletedText)

	if ids := textIDs(s.AnnotationTexts("annot-1")); slices.Contains(ids, "t8") {
		t.Errorf("削除済みのテキストを拾っている: %v", ids)
	}
}

// 図形の外（注釈の外の図形）に紐づくラベルは拾わない。
func TestAnnotationTexts_ExcludesLabelOfOutsideShape(t *testing.T) {
	t.Parallel()

	outsideShape := `{"id":"shape-9","type":"rectangle"}`
	outsideLabel := `{"id":"t7","type":"text","text":"外の図形のラベル","containerId":"shape-9"}`

	s := scene(t, annotFrame, childText, outsideShape, outsideLabel)

	if ids := textIDs(s.AnnotationTexts("annot-1")); slices.Contains(ids, "t7") {
		t.Errorf("注釈外の図形のラベルを拾っている: %v", ids)
	}
}

// frameId と containerId の両方に該当しても重複させない。
func TestAnnotationTexts_NoDuplicates(t *testing.T) {
	t.Parallel()

	both := `{"id":"t4","type":"text","text":"両方","frameId":"annot-1","containerId":"shape-1"}`

	s := scene(t, annotFrame, childShape, both)

	if ids := textIDs(s.AnnotationTexts("annot-1")); !slices.Equal(ids, []string{"annot-1", "t4"}) {
		t.Errorf("抽出 = %v, want [annot-1 t4]", ids)
	}
}

// 名前のない frame は入力に空文字を混ぜない。
func TestAnnotationTexts_UnnamedFrame(t *testing.T) {
	t.Parallel()

	unnamed := `{"id":"annot-1","type":"frame","customData":{"etoki":{}}}`

	s := scene(t, unnamed, childText)

	if ids := textIDs(s.AnnotationTexts("annot-1")); !slices.Equal(ids, []string{"t1"}) {
		t.Errorf("抽出 = %v, want [t1]", ids)
	}
}

// シーンから直接ハッシュを出せる。ラベルを書き換えるとハッシュが変わる。
func TestAnnotationHash(t *testing.T) {
	t.Parallel()

	before := scene(t, annotFrame, childShape, boundLabel)
	after := scene(t, annotFrame, childShape,
		`{"id":"t2","type":"text","text":"Webhook の冪等化（優先）","containerId":"shape-1"}`)

	a := before.Annotations()[0]

	if before.AnnotationHash(a) == after.AnnotationHash(after.Annotations()[0]) {
		t.Error("図形のラベルを変えてもハッシュが変わらない")
	}
}

// 粒度を変えるとハッシュが変わる（シーン経由でも同じ）。
func TestAnnotationHash_GranularityMatters(t *testing.T) {
	t.Parallel()

	epic := scene(t, annotFrame, childText)
	issue := scene(t,
		`{"id":"annot-1","type":"frame","name":"決済まわり","customData":{"etoki":{"granularity":"issue"}}}`,
		childText)

	if epic.AnnotationHash(epic.Annotations()[0]) ==
		issue.AnnotationHash(issue.Annotations()[0]) {
		t.Error("粒度を変えてもハッシュが変わらない")
	}
}

func TestParseScene_Invalid(t *testing.T) {
	t.Parallel()

	if _, err := domain.ParseScene([]byte(`{`)); err == nil {
		t.Error("ParseScene: want error for malformed json, got nil")
	}
}

// customData を持たない要素があってもパースは壊れない。
func TestParseScene_TolerantOfUnknownFields(t *testing.T) {
	t.Parallel()

	raw := `{"type":"excalidraw","version":2,"source":"https://excalidraw.com",
	         "elements":[{"id":"x","type":"rectangle","strokeColor":"#000","seed":1}],
	         "appState":{"viewBackgroundColor":"#fff"}}`

	s, err := domain.ParseScene([]byte(raw))
	if err != nil {
		t.Fatalf("ParseScene: %v", err)
	}
	if len(s.Elements) != 1 {
		t.Errorf("要素数 = %d, want 1", len(s.Elements))
	}
}
