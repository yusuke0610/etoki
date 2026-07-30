package domain_test

import (
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
)

// baseElements はほとんどのケースで出発点にする要素列。
func baseElements() []domain.TextElement {
	return []domain.TextElement{
		{ID: "a", Text: "決済フローを見直す"},
		{ID: "b", Text: "Stripe の SDK が古い"},
	}
}

func hash(elems []domain.TextElement, g domain.Granularity) domain.ContentHash {
	return domain.ComputeContentHash(elems, g)
}

// A-1: テキスト要素が 0 件でも算出できる。
func TestComputeContentHash_NoElements(t *testing.T) {
	t.Parallel()

	got := hash(nil, domain.GranularityAuto)
	if got == "" {
		t.Fatal("hash is empty")
	}
	if got != hash([]domain.TextElement{}, domain.GranularityAuto) {
		t.Error("nil と空スライスで結果が異なる")
	}
}

// A-2: 要素の並び順はハッシュに影響しない。
func TestComputeContentHash_OrderIndependent(t *testing.T) {
	t.Parallel()

	forward := baseElements()
	reversed := []domain.TextElement{forward[1], forward[0]}

	if hash(forward, domain.GranularityAuto) != hash(reversed, domain.GranularityAuto) {
		t.Error("並び順でハッシュが変わっている")
	}
}

// A-3: テキスト内容が変わればハッシュも変わる。
func TestComputeContentHash_TextChangeAltersHash(t *testing.T) {
	t.Parallel()

	changed := baseElements()
	changed[0].Text += "。"

	if hash(baseElements(), domain.GranularityAuto) == hash(changed, domain.GranularityAuto) {
		t.Error("テキストを変えてもハッシュが変わらない")
	}
}

// A-4: テキストが同じでも要素 ID が違えばハッシュは変わる。
func TestComputeContentHash_ElementIDIsPartOfHash(t *testing.T) {
	t.Parallel()

	renamed := baseElements()
	renamed[0].ID = "z"

	if hash(baseElements(), domain.GranularityAuto) == hash(renamed, domain.GranularityAuto) {
		t.Error("要素 ID を変えてもハッシュが変わらない")
	}
}

// A-5: 改行コードの違いは同一視する。
func TestComputeContentHash_NormalizesLineEndings(t *testing.T) {
	t.Parallel()

	lf := []domain.TextElement{{ID: "a", Text: "一行目\n二行目"}}
	crlf := []domain.TextElement{{ID: "a", Text: "一行目\r\n二行目"}}
	cr := []domain.TextElement{{ID: "a", Text: "一行目\r二行目"}}

	want := hash(lf, domain.GranularityAuto)
	if hash(crlf, domain.GranularityAuto) != want {
		t.Error("CRLF が LF と同一視されていない")
	}
	if hash(cr, domain.GranularityAuto) != want {
		t.Error("CR が LF と同一視されていない")
	}
}

// A-6: 行末の空白の有無は同一視する。
func TestComputeContentHash_IgnoresTrailingWhitespace(t *testing.T) {
	t.Parallel()

	plain := []domain.TextElement{{ID: "a", Text: "一行目\n二行目"}}
	padded := []domain.TextElement{{ID: "a", Text: "一行目   \n二行目\t"}}

	if hash(plain, domain.GranularityAuto) != hash(padded, domain.GranularityAuto) {
		t.Error("行末の空白でハッシュが変わっている")
	}
}

// A-7: Unicode の合成済み文字と結合文字を同一視する。
func TestComputeContentHash_NormalizesUnicode(t *testing.T) {
	t.Parallel()

	// この 2 つは見た目が同じで、バイト列だけが違う。エディタや自動整形が
	// 片方を正規化するとテストが「同じ文字列同士の比較」に化けて無言で
	// 通ってしまうため、直下のガードで検出する。
	const (
		nfcGa = "が"  // が（合成済み）
		nfdGa = "が" // か + 結合濁点
	)
	if nfcGa == nfdGa {
		t.Fatal("テストデータが正規化されている")
	}

	nfc := []domain.TextElement{{ID: "a", Text: nfcGa}}
	nfd := []domain.TextElement{{ID: "a", Text: nfdGa}}
	if hash(nfc, domain.GranularityAuto) != hash(nfd, domain.GranularityAuto) {
		t.Error("NFC と NFD が同一視されていない")
	}
}

// A-8, A-9: 粒度指定はハッシュに含まれる。
func TestComputeContentHash_GranularityIsPartOfHash(t *testing.T) {
	t.Parallel()

	auto := hash(baseElements(), domain.GranularityAuto)
	epic := hash(baseElements(), domain.GranularityEpic)
	issue := hash(baseElements(), domain.GranularityIssue)

	if auto == epic {
		t.Error("指定なしと epic が同じハッシュになっている")
	}
	if epic == issue {
		t.Error("epic と issue が同じハッシュになっている")
	}
	if auto == issue {
		t.Error("指定なしと issue が同じハッシュになっている")
	}
}

// A-10: 要素を追加すればハッシュは変わる。
func TestComputeContentHash_AddingElementAltersHash(t *testing.T) {
	t.Parallel()

	added := append(baseElements(), domain.TextElement{ID: "c", Text: "Webhook の冪等化"})

	if hash(baseElements(), domain.GranularityAuto) == hash(added, domain.GranularityAuto) {
		t.Error("要素を追加してもハッシュが変わらない")
	}
}

// A-11: 図形だけの変更は検知しない。
//
// これはバグではなく仕様である。ハッシュの入力はテキスト要素だけなので、
// 矢印や座標をいくら動かしてもハッシュは変わらない。ADR に記録した既知の
// 限界を、後から善意で「直されて」しまわないようテストで固定する。
func TestComputeContentHash_IgnoresNonTextChanges(t *testing.T) {
	t.Parallel()

	// 座標や図形は TextElement に含まれない。つまり図形だけを動かした
	// ボードから抽出される要素列は、変更前とまったく同一になる。
	before := baseElements()
	afterMovingShapesOnly := baseElements()

	if hash(before, domain.GranularityAuto) != hash(afterMovingShapesOnly, domain.GranularityAuto) {
		t.Error("テキスト以外の変更を検知してしまっている（仕様と異なる）")
	}
}

// A-12: 要素間でテキストを入れ替えるとハッシュは変わる。
func TestComputeContentHash_SwappingTextsAltersHash(t *testing.T) {
	t.Parallel()

	original := baseElements()
	swapped := []domain.TextElement{
		{ID: original[0].ID, Text: original[1].Text},
		{ID: original[1].ID, Text: original[0].Text},
	}

	if hash(original, domain.GranularityAuto) == hash(swapped, domain.GranularityAuto) {
		t.Error("ID とテキストの対応が変わってもハッシュが変わらない")
	}
}

// 区切り文字が正しく効いていることの確認。要素の分割位置が変わっても
// 同じハッシュにならないこと。
func TestComputeContentHash_ElementBoundaryMatters(t *testing.T) {
	t.Parallel()

	one := []domain.TextElement{{ID: "a", Text: "XY"}}
	two := []domain.TextElement{{ID: "a", Text: "X"}, {ID: "aY", Text: ""}}

	if hash(one, domain.GranularityAuto) == hash(two, domain.GranularityAuto) {
		t.Error("要素の境界が区別されていない")
	}
}

func TestGranularityValid(t *testing.T) {
	t.Parallel()

	valid := []domain.Granularity{
		domain.GranularityAuto,
		domain.GranularityEpic,
		domain.GranularityIssue,
	}
	for _, g := range valid {
		if !g.Valid() {
			t.Errorf("Granularity(%q).Valid() = false, want true", g)
		}
	}

	if domain.Granularity("project").Valid() {
		t.Error(`Granularity("project").Valid() = true, want false`)
	}
}
