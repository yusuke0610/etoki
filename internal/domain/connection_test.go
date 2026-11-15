package domain_test

import (
	"slices"
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
)

// 矢印のつながりの読み方（ADR 0096）。読むのは Excalidraw が書いた結び先だけで、
// 座標は見ない。

const (
	userBox    = `{"id":"u","type":"rectangle","frameId":"annot-1"}`
	userLabel  = `{"id":"u-label","type":"text","text":"User","containerId":"u"}`
	webBox     = `{"id":"w","type":"rectangle","frameId":"annot-1"}`
	webLabel   = `{"id":"w-label","type":"text","text":"WebApp","containerId":"w"}`
	outsideBox = `{"id":"x","type":"rectangle"}`
	outsideLbl = `{"id":"x-label","type":"text","text":"外の箱","containerId":"x"}`
)

// arrow は注釈の中の矢印を組み立てる。from / to が空ならその端はつながっていない。
func arrow(id, from, to, startHead, endHead, extra string) string {
	bind := func(target string) string {
		if target == "" {
			return "null"
		}
		return `{"elementId":"` + target + `","focus":0,"gap":1}`
	}
	head := func(h string) string {
		if h == "" {
			return "null"
		}
		return `"` + h + `"`
	}
	return `{"id":"` + id + `","type":"arrow","frameId":"annot-1",` +
		`"startBinding":` + bind(from) + `,"endBinding":` + bind(to) + `,` +
		`"startArrowhead":` + head(startHead) + `,"endArrowhead":` + head(endHead) + extra + `}`
}

// 両端の名前と矢印のラベルを、折り返す前の文字で読む。ラベルは図形の中の文字
// （containerId）から引く。
func TestAnnotationConnections_ReadsBoundEnds(t *testing.T) {
	t.Parallel()

	label := `{"id":"a1-label","type":"text","text":"リクエスト\n送信","originalText":"リクエスト送信","containerId":"a1"}`
	s := scene(t, annotFrame, userBox, userLabel, webBox, webLabel, arrow("a1", "u", "w", "", "arrow", ""), label)

	got := s.AnnotationConnections("annot-1")

	want := []domain.Connection{{
		From:   domain.ConnectionEnd{Bound: true, Inside: true, Name: "User"},
		To:     domain.ConnectionEnd{Bound: true, Inside: true, Name: "WebApp"},
		Label:  "リクエスト送信",
		ToHead: true,
	}}
	if !slices.Equal(got, want) {
		t.Errorf("AnnotationConnections() = %+v, want %+v", got, want)
	}
}

// 文字の要素に直接つないだ矢印は、その文字を名前にする。
func TestAnnotationConnections_TextTarget(t *testing.T) {
	t.Parallel()

	note := `{"id":"n","type":"text","text":"通知するチャンネル\nを選べる","originalText":"通知するチャンネルを選べる","frameId":"annot-1"}`
	s := scene(t, annotFrame, webBox, webLabel, note, arrow("a1", "w", "n", "", "arrow", ""))

	got := s.AnnotationConnections("annot-1")
	if len(got) != 1 || got[0].To.Name != "通知するチャンネルを選べる" {
		t.Errorf("AnnotationConnections() = %+v, want 終点の名前が文字の要素の本文", got)
	}
}

// 図形の中の文字（frameId を持たず、containerId で図形に紐づく）につないだ矢印は、
// その図形が囲みの中なら中として名前を返す。childTexts と同じ所属の規則で、外の
// 図形の文字なら外のまま。
func TestAnnotationConnections_TextBoundToShape(t *testing.T) {
	t.Parallel()

	s := scene(t, annotFrame, userBox, userLabel, webBox, webLabel, outsideBox, outsideLbl,
		arrow("a1", "u", "w-label", "", "arrow", ""),
		arrow("a2", "u", "x-label", "", "arrow", ""))

	got := s.AnnotationConnections("annot-1")
	if len(got) != 2 {
		t.Fatalf("AnnotationConnections() = %+v, want 2 本", got)
	}
	if want := (domain.ConnectionEnd{Bound: true, Inside: true, Name: "WebApp"}); got[0].To != want {
		t.Errorf("中の図形の文字につないだ端 = %+v, want %+v", got[0].To, want)
	}
	if want := (domain.ConnectionEnd{Bound: true}); got[1].To != want {
		t.Errorf("外の図形の文字につないだ端 = %+v, want %+v", got[1].To, want)
	}
}

// どちらの端もつながっていない矢印は返さない。片方だけつながっていれば返し、
// つながっていない端はそうと分かる形にする。
func TestAnnotationConnections_SkipsUnboundArrows(t *testing.T) {
	t.Parallel()

	s := scene(t, annotFrame, userBox, userLabel,
		arrow("free", "", "", "", "arrow", ""),
		arrow("half", "u", "", "", "arrow", ""))

	got := s.AnnotationConnections("annot-1")

	want := []domain.Connection{{
		From:   domain.ConnectionEnd{Bound: true, Inside: true, Name: "User"},
		To:     domain.ConnectionEnd{},
		ToHead: true,
	}}
	if !slices.Equal(got, want) {
		t.Errorf("AnnotationConnections() = %+v, want %+v", got, want)
	}
}

// 囲みの外につないだ端は、つながっていることだけを返し、名前は出さない。
// 解釈の入力は囲みの中身だけ（ADR 0093）。
func TestAnnotationConnections_HidesOutsideTarget(t *testing.T) {
	t.Parallel()

	s := scene(t, annotFrame, userBox, userLabel, outsideBox, outsideLbl, arrow("a1", "u", "x", "", "arrow", ""))

	got := s.AnnotationConnections("annot-1")
	if len(got) != 1 {
		t.Fatalf("AnnotationConnections() = %+v, want 1 本", got)
	}
	if want := (domain.ConnectionEnd{Bound: true}); got[0].To != want {
		t.Errorf("To = %+v, want %+v（囲みの外の名前を出さない）", got[0].To, want)
	}
}

// 削除した矢印は返さない。削除した要素につないだ端は、つながっていない扱いにする。
func TestAnnotationConnections_IgnoresDeleted(t *testing.T) {
	t.Parallel()

	deletedArrow := arrow("gone", "u", "w", "", "arrow", `,"isDeleted":true`)
	deletedBox := `{"id":"d","type":"rectangle","frameId":"annot-1","isDeleted":true}`
	s := scene(t, annotFrame, userBox, userLabel, webBox, webLabel, deletedBox,
		deletedArrow, arrow("a1", "u", "d", "", "arrow", ""))

	got := s.AnnotationConnections("annot-1")

	want := []domain.Connection{{
		From:   domain.ConnectionEnd{Bound: true, Inside: true, Name: "User"},
		ToHead: true,
	}}
	if !slices.Equal(got, want) {
		t.Errorf("AnnotationConnections() = %+v, want %+v", got, want)
	}
}

// 囲みの外の矢印と、入れ子の frame の中の矢印は返さない（childTexts と同じ 1 段）。
func TestAnnotationConnections_OnlyArrowsDirectlyInFrame(t *testing.T) {
	t.Parallel()

	outsideArrow := `{"id":"o","type":"arrow","startBinding":{"elementId":"u"},"endBinding":{"elementId":"w"}}`
	inner := `{"id":"inner","type":"frame","frameId":"annot-1"}`
	nestedArrow := `{"id":"n","type":"arrow","frameId":"inner","startBinding":{"elementId":"u"},"endBinding":{"elementId":"w"}}`
	s := scene(t, annotFrame, userBox, userLabel, webBox, webLabel, outsideArrow, inner, nestedArrow)

	if got := s.AnnotationConnections("annot-1"); len(got) != 0 {
		t.Errorf("AnnotationConnections() = %+v, want なし", got)
	}
}

// 矢じりの付き方をそのまま返す。向きを決めるのは矢じりで、つないだ順ではない。
func TestAnnotationConnections_Arrowheads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		start, end       string
		fromHead, toHead bool
	}{
		{start: "", end: "arrow", toHead: true},
		{start: "arrow", end: "", fromHead: true},
		{start: "triangle", end: "dot", fromHead: true, toHead: true},
		{start: "", end: ""},
	}
	for _, tt := range tests {
		s := scene(t, annotFrame, userBox, userLabel, webBox, webLabel, arrow("a1", "u", "w", tt.start, tt.end, ""))
		got := s.AnnotationConnections("annot-1")
		if len(got) != 1 || got[0].FromHead != tt.fromHead || got[0].ToHead != tt.toHead {
			t.Errorf("start=%q end=%q: %+v, want FromHead=%v ToHead=%v",
				tt.start, tt.end, got, tt.fromHead, tt.toHead)
		}
	}
}

// 並びは要素の順のまま。上下の位置で並べ替えると、座標から順序を決めることになる。
func TestAnnotationConnections_KeepsElementOrder(t *testing.T) {
	t.Parallel()

	lower := arrow("lower", "w", "u", "", "arrow", `,"y":500`)
	upper := arrow("upper", "u", "w", "", "arrow", `,"y":100`)
	s := scene(t, annotFrame, userBox, userLabel, webBox, webLabel, lower, upper)

	got := s.AnnotationConnections("annot-1")
	if len(got) != 2 || got[0].From.Name != "WebApp" || got[1].From.Name != "User" {
		t.Errorf("AnnotationConnections() = %+v, want 要素の順（lower, upper）", got)
	}
}
