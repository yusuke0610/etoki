// Package domain は etoki のエンティティと、外部に触れない純粋ロジックを持つ。
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// ContentHash は注釈範囲の内容から算出したハッシュ。
type ContentHash string

// Granularity は開発者が注釈に指定する粒度。
type Granularity string

// Granularity の取りうる値。
//
// ゼロ値が GranularityAuto になるよう空文字にしている。粒度の指定は任意であり、
// 「指定しない」が既定の状態であるため。
const (
	// GranularityAuto は粒度を指定せず LLM に委ねることを表す。
	GranularityAuto Granularity = ""
	// GranularityEpic は範囲全体を epic として扱うよう指示する。
	GranularityEpic Granularity = "epic"
	// GranularityIssue は範囲全体を issue として扱うよう指示する。
	GranularityIssue Granularity = "issue"
)

// Valid は g が定義済みの粒度かどうかを返す。
func (g Granularity) Valid() bool {
	switch g {
	case GranularityAuto, GranularityEpic, GranularityIssue:
		return true
	default:
		return false
	}
}

// TextElement は注釈範囲に含まれる Excalidraw のテキスト要素。
type TextElement struct {
	// ID は Excalidraw 要素の ID。
	ID string
	// Text は要素のテキスト。
	Text string
}

// ハッシュ入力の区切りに使う制御文字。
//
// 通常のテキストに現れないバイトを使うことで、要素の境界とテキスト本体が
// 混同されるのを防ぐ。例えば区切りに改行を使うと、改行を含むテキスト 1 件と
// 2 件の要素が同じ入力になりうる。
const (
	unitSep   = 0x1F // 要素 ID とテキストの区切り
	recordSep = 0x1E // 要素と要素の区切り
	groupSep  = 0x1D // 要素列と粒度指定、粒度指定と図の種別の区切り
)

// ComputeContentHash は注釈範囲のテキスト要素と、粒度指定・図の種別から
// ハッシュを算出する。
//
// 算出対象はテキストのみである。図形・矢印・座標の変更は検知しない。これは
// 既知の限界であり、仕様として受け入れている。開発者はいつでも手動で再実行
// できるため、検知漏れが取り返しのつかない状態にはならない。
//
// **粒度と種別は図形ではなくメタデータなので、テキストと同じ入力に含める。**
// どちらも解釈のプロンプトに載る（granularityInstruction / kindInstruction）
// ので、変えれば LLM が読む前提が変わる。含めないと、種別を差し替えても
// created のままになり、前の種別で作った draft issue が最新であるかのように
// 見える。
func ComputeContentHash(elements []TextElement, g Granularity, k DiagramKind) ContentHash {
	// 呼び出し側のスライスを並べ替えないよう複製する。
	sorted := slices.Clone(elements)
	slices.SortStableFunc(sorted, func(a, b TextElement) int {
		return strings.Compare(a.ID, b.ID)
	})

	h := sha256.New()
	for _, e := range sorted {
		h.Write([]byte(e.ID))
		h.Write([]byte{unitSep})
		h.Write([]byte(normalizeText(e.Text)))
		h.Write([]byte{recordSep})
	}
	h.Write([]byte{groupSep})
	h.Write([]byte(g))

	// **区切りを挟む。** 詰めて書くと、粒度と種別の境目が動いただけの別の
	// 組み合わせ（"e" + "pic" と "epic" + 指定なし）が同じ入力になる。
	//
	// **指定なしでも書く。** 書かない形にすれば種別を持つ前の値と一致させられる
	// が、算出の入力が種別の有無で分岐することになる。分岐は「なぜここだけ
	// 特別なのか」を知らないと消せない形で残るので、一度きりの移行のために
	// 恒久的な分岐を抱えない（ADR 0045）。移行の代償は、既存の created な注釈が
	// 一度だけ changed に見えることで、再実行すれば揃う。
	h.Write([]byte{groupSep})
	h.Write([]byte(k))

	return ContentHash(hex.EncodeToString(h.Sum(nil)))
}

// normalizeText は表示上まったく同じテキストが同じバイト列になるよう整える。
//
// 正規化しないと、編集環境の違い（改行コード）や入力方式の違い（合成済みか
// 結合文字か）だけで「変更あり」と誤判定してしまう。
//
// **epic のタイトルの重複判定（normalizeTitle）も同じ正規化を使う**（ADR 0028）。
// 「見た目が同じものは同じ」の定義をここ 1 つに保つため。ハッシュの都合だけで
// 変えると、タイトルの衝突判定も一緒に動く。
func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = norm.NFC.String(s)

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}

	return strings.Join(lines, "\n")
}
