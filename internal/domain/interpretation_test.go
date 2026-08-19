package domain_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
)

// validInterpretation は制約をすべて満たす解釈結果を返す。
//
// i2 に親がないのは意図的。epic に属さない単独の issue は誤りではない。
func validInterpretation() domain.Interpretation {
	return domain.Interpretation{
		Summary: "決済まわりの課題出し。カード決済と返金の 2 系統に分かれている。",
		Items: []domain.InterpretedItem{
			{LocalID: "e1", Kind: domain.KindEpic, Title: "決済フローの見直し", Body: "全体の方針"},
			{LocalID: "i1", Kind: domain.KindIssue, Title: "カード決済のエラー処理", ParentLocalID: ptr("e1")},
			{LocalID: "i2", Kind: domain.KindIssue, Title: "返金導線の整理"},
		},
	}
}

// errorFields は検証エラーの Field を出た順に返す。
func errorFields(t *testing.T, err error) []string {
	t.Helper()

	var errs domain.ValidationErrors
	if !errors.As(err, &errs) {
		t.Fatalf("ValidationErrors ではない: %#v", err)
	}

	fields := make([]string, len(errs))
	for i, e := range errs {
		fields[i] = e.Field
	}
	return fields
}

func TestParseInterpretation(t *testing.T) {
	t.Parallel()

	t.Run("2 階層の出力を読める", func(t *testing.T) {
		t.Parallel()

		raw := []byte(`{
			"summary": "決済まわりの課題出し",
			"items": [
				{"localId": "e1", "kind": "epic", "title": "決済フローの見直し", "body": "全体の方針", "parentLocalId": null},
				{"localId": "i1", "kind": "issue", "title": "カード決済のエラー処理", "body": "", "parentLocalId": "e1"}
			]
		}`)

		in, err := domain.ParseInterpretation(raw, nil)
		if err != nil {
			t.Fatalf("ParseInterpretation() = %v", err)
		}

		if in.Summary != "決済まわりの課題出し" {
			t.Errorf("Summary = %q", in.Summary)
		}
		if len(in.Items) != 2 {
			t.Fatalf("len(Items) = %d, want 2", len(in.Items))
		}
		if in.Items[0].ParentLocalID != nil {
			t.Errorf("Items[0].ParentLocalID = %q, want nil", *in.Items[0].ParentLocalID)
		}
		if in.Items[1].ParentLocalID == nil || *in.Items[1].ParentLocalID != "e1" {
			t.Errorf("Items[1].ParentLocalID = %v, want \"e1\"", in.Items[1].ParentLocalID)
		}
	})

	t.Run("parentLocalId を省いた出力は親なしとして読む", func(t *testing.T) {
		t.Parallel()

		raw := []byte(`{"summary": "s", "items": [{"localId": "e1", "kind": "epic", "title": "t", "body": ""}]}`)

		in, err := domain.ParseInterpretation(raw, nil)
		if err != nil {
			t.Fatalf("ParseInterpretation() = %v", err)
		}
		if in.Items[0].ParentLocalID != nil {
			t.Errorf("ParentLocalID = %q, want nil", *in.Items[0].ParentLocalID)
		}
	})

	// 綴り間違いを素通しにすると、親子関係が黙って失われる。
	t.Run("未知のフィールドを誤りとして扱う", func(t *testing.T) {
		t.Parallel()

		raw := []byte(`{"summary": "s", "items": [{"localId": "i1", "kind": "issue", "title": "t", "parentId": "e1"}]}`)

		if _, err := domain.ParseInterpretation(raw, nil); err == nil {
			t.Fatal("ParseInterpretation() = nil, want error")
		}
	})

	t.Run("JSON の後ろに続く出力を誤りとして扱う", func(t *testing.T) {
		t.Parallel()

		raw := []byte(`{"summary": "s", "items": []} 以上が解釈結果です。`)

		if _, err := domain.ParseInterpretation(raw, nil); err == nil {
			t.Fatal("ParseInterpretation() = nil, want error")
		}
	})

	t.Run("壊れた JSON を誤りとして扱う", func(t *testing.T) {
		t.Parallel()

		if _, err := domain.ParseInterpretation([]byte(`{"summary":`), nil); err == nil {
			t.Fatal("ParseInterpretation() = nil, want error")
		}
	})
}

func TestInterpretation_Validate_Valid(t *testing.T) {
	t.Parallel()

	for _, g := range []domain.Granularity{domain.GranularityAuto, domain.GranularityEpic} {
		if err := validInterpretation().Validate(g); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", g, err)
		}
	}
}

func TestInterpretation_Validate_ItemRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// mutate は妥当な解釈結果を壊す。
		mutate func(*domain.Interpretation)
		// wantFields は報告されてほしい Field。順不同で照合する。
		wantFields []string
	}{
		{
			name:       "summary が空",
			mutate:     func(in *domain.Interpretation) { in.Summary = "  \n " },
			wantFields: []string{"summary"},
		},
		{
			name:       "項目が 1 件もない",
			mutate:     func(in *domain.Interpretation) { in.Items = nil },
			wantFields: []string{"items"},
		},
		{
			name:       "kind が epic でも issue でもない",
			mutate:     func(in *domain.Interpretation) { in.Items[0].Kind = "project" },
			wantFields: []string{"items[0].kind"},
		},
		{
			name:       "kind が空",
			mutate:     func(in *domain.Interpretation) { in.Items[0].Kind = "" },
			wantFields: []string{"items[0].kind"},
		},
		{
			name:       "title が空白だけ",
			mutate:     func(in *domain.Interpretation) { in.Items[1].Title = "   " },
			wantFields: []string{"items[1].title"},
		},
		{
			name:       "localId が空",
			mutate:     func(in *domain.Interpretation) { in.Items[2].LocalID = "" },
			wantFields: []string{"items[2].localId"},
		},
		{
			name:       "localId が重複している",
			mutate:     func(in *domain.Interpretation) { in.Items[2].LocalID = "i1" },
			wantFields: []string{"items[2].localId"},
		},
		{
			name:       "parentLocalId の指す localId がない",
			mutate:     func(in *domain.Interpretation) { in.Items[1].ParentLocalID = ptr("e9") },
			wantFields: []string{"items[1].parentLocalId"},
		},
		{
			name:       "自分自身を親にしている",
			mutate:     func(in *domain.Interpretation) { in.Items[1].ParentLocalID = ptr("i1") },
			wantFields: []string{"items[1].parentLocalId"},
		},
		{
			name:       "epic が親を持っている",
			mutate:     func(in *domain.Interpretation) { in.Items[0].ParentLocalID = ptr("i1") },
			wantFields: []string{"items[0].parentLocalId"},
		},
		{
			// 階層は epic ← issue の 1 本だけ（ADR 0006）。
			name:       "issue を親にしている",
			mutate:     func(in *domain.Interpretation) { in.Items[2].ParentLocalID = ptr("i1") },
			wantFields: []string{"items[2].parentLocalId"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := validInterpretation()
			tt.mutate(&in)

			err := in.Validate(domain.GranularityAuto)
			if err == nil {
				t.Fatal("Validate() = nil, want error")
			}

			got := errorFields(t, err)
			for _, want := range tt.wantFields {
				if !slices.Contains(got, want) {
					t.Errorf("Field %q が報告されていない: %v", want, got)
				}
			}
			if len(got) != len(tt.wantFields) {
				t.Errorf("報告された Field = %v, want %v", got, tt.wantFields)
			}
		})
	}
}

// 粒度はプロンプトでも指示するが、LLM が従う保証はないので構造で守る。
func TestInterpretation_Validate_Granularity(t *testing.T) {
	t.Parallel()

	t.Run("issue 指定なら epic を作れない", func(t *testing.T) {
		t.Parallel()

		err := validInterpretation().Validate(domain.GranularityIssue)
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		if got := errorFields(t, err); !slices.Contains(got, "items[0].kind") {
			t.Errorf("報告された Field = %v", got)
		}
	})

	t.Run("issue 指定で issue だけなら通る", func(t *testing.T) {
		t.Parallel()

		in := domain.Interpretation{
			Summary: "返金導線の整理",
			Items: []domain.InterpretedItem{
				{LocalID: "i1", Kind: domain.KindIssue, Title: "返金導線の整理"},
			},
		}
		if err := in.Validate(domain.GranularityIssue); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})

	t.Run("epic 指定なのに epic がない", func(t *testing.T) {
		t.Parallel()

		in := domain.Interpretation{
			Summary: "返金導線の整理",
			Items: []domain.InterpretedItem{
				{LocalID: "i1", Kind: domain.KindIssue, Title: "返金導線の整理"},
			},
		}

		err := in.Validate(domain.GranularityEpic)
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		if got := errorFields(t, err); !slices.Contains(got, "items") {
			t.Errorf("報告された Field = %v", got)
		}
	})

	t.Run("指定なしなら粒度の制約を課さない", func(t *testing.T) {
		t.Parallel()

		if err := validInterpretation().Validate(domain.GranularityAuto); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})
}

// 最初の 1 件で打ち切ると、LLM は 1 往復で 1 箇所しか直せない（ADR 0005）。
func TestInterpretation_Validate_CollectsAllErrors(t *testing.T) {
	t.Parallel()

	in := domain.Interpretation{
		Summary: "",
		Items: []domain.InterpretedItem{
			{LocalID: "", Kind: "project", Title: ""},
			{LocalID: "i1", Kind: domain.KindIssue, Title: "返金導線の整理", ParentLocalID: ptr("e9")},
		},
	}

	err := in.Validate(domain.GranularityEpic)
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}

	want := []string{
		"summary",
		"items[0].kind",
		"items[0].title",
		"items[0].localId",
		"items[1].parentLocalId",
		"items", // epic 指定なのに epic がない
	}

	got := errorFields(t, err)
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("Field %q が報告されていない: %v", w, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("報告件数 = %d, want %d (%v)", len(got), len(want), got)
	}
}

func TestValidationErrors_Error(t *testing.T) {
	t.Parallel()

	errs := domain.ValidationErrors{
		{Field: "summary", Message: "説明を入れてください"},
		{Field: "items[0].title", Message: "title を空にはできません"},
	}

	want := "summary: 説明を入れてください\nitems[0].title: title を空にはできません"
	if got := errs.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestItemKind_Valid(t *testing.T) {
	t.Parallel()

	tests := map[domain.ItemKind]bool{
		domain.KindEpic:  true,
		domain.KindIssue: true,
		"":               false,
		"project":        false,
		"Epic":           false,
	}

	for k, want := range tests {
		if got := k.Valid(); got != want {
			t.Errorf("ItemKind(%q).Valid() = %t, want %t", k, got, want)
		}
	}
}

// 対応づけの解決（ADR 0026）。
//
// LLM には `p1` のような短い ref を出させ、境界へ出るのは解決済みの item ID に
// する。node ID は長く不透明で、復唱させると取り違える。
func TestParseInterpretation_ResolvesPreviousRefs(t *testing.T) {
	t.Parallel()

	previous := []domain.PreviousItem{
		{Ref: "p1", ItemID: "PVTI_a", Kind: domain.KindEpic, Title: "決済基盤", Body: "入口"},
		{Ref: "p2", ItemID: "PVTI_b", Kind: domain.KindIssue, Title: "カード決済", Body: ""},
	}

	item := func(localID string, ref string) string {
		if ref == "" {
			return `{"localId":"` + localID + `","kind":"issue","title":"t","body":"",` +
				`"parentLocalId":null,"previousRef":null}`
		}
		return `{"localId":"` + localID + `","kind":"issue","title":"t","body":"",` +
			`"parentLocalId":null,"previousRef":"` + ref + `"}`
	}
	doc := func(items ...string) []byte {
		return []byte(`{"summary":"s","items":[` + strings.Join(items, ",") + `]}`)
	}

	t.Run("既知の ref は item ID に解決される", func(t *testing.T) {
		t.Parallel()

		in, err := domain.ParseInterpretation(doc(item("i1", "p2")), previous)
		if err != nil {
			t.Fatalf("ParseInterpretation() = %v", err)
		}
		if in.Items[0].PreviousItemID == nil {
			t.Fatal("previousItemId が nil のまま")
		}
		if got := *in.Items[0].PreviousItemID; got != "PVTI_b" {
			t.Errorf("previousItemId = %q, want PVTI_b", got)
		}
	})

	// 新しく作るほうが取り返しがつく。既定はこちら。
	t.Run("ref が無ければ新規のまま", func(t *testing.T) {
		t.Parallel()

		in, err := domain.ParseInterpretation(doc(item("i1", "")), previous)
		if err != nil {
			t.Fatalf("ParseInterpretation() = %v", err)
		}
		if in.Items[0].PreviousItemID != nil {
			t.Errorf("previousItemId = %v, want nil", *in.Items[0].PreviousItemID)
		}
	})

	// 綴り間違いはそのまま修正指示になって再送で直る（ADR 0005）。
	t.Run("未知の ref は検証エラー", func(t *testing.T) {
		t.Parallel()

		_, err := domain.ParseInterpretation(doc(item("i1", "p9")), previous)

		var verrs domain.ValidationErrors
		if !errors.As(err, &verrs) {
			t.Fatalf("ParseInterpretation() = %v, want ValidationErrors", err)
		}
		if !strings.Contains(verrs.Error(), "p9") {
			t.Errorf("どの ref が問題かが読めない: %s", verrs.Error())
		}
	})

	// 1 つの draft issue を 2 件で奪い合えない。通すと、あとに処理したほうの
	// 内容だけが残り、もう片方は「作ったつもり」で消える。
	t.Run("同じ ref を 2 件が指すと検証エラー", func(t *testing.T) {
		t.Parallel()

		_, err := domain.ParseInterpretation(doc(item("i1", "p1"), item("i2", "p1")), previous)

		var verrs domain.ValidationErrors
		if !errors.As(err, &verrs) {
			t.Fatalf("ParseInterpretation() = %v, want ValidationErrors", err)
		}
	})

	// 前回ぶんを渡していない解釈で ref が出てきたら、モデルが作り話をしている。
	t.Run("前回ぶんが無いのに ref があれば検証エラー", func(t *testing.T) {
		t.Parallel()

		_, err := domain.ParseInterpretation(doc(item("i1", "p1")), nil)

		var verrs domain.ValidationErrors
		if !errors.As(err, &verrs) {
			t.Fatalf("ParseInterpretation() = %v, want ValidationErrors", err)
		}
	})

	t.Run("前回ぶんが無く ref も無ければ全件新規", func(t *testing.T) {
		t.Parallel()

		in, err := domain.ParseInterpretation(doc(item("i1", ""), item("i2", "")), nil)
		if err != nil {
			t.Fatalf("ParseInterpretation() = %v", err)
		}
		for _, it := range in.Items {
			if it.PreviousItemID != nil {
				t.Errorf("%s が新規になっていない", it.LocalID)
			}
		}
	})
}

// 作成のリクエストは画面から送られてくるので、解釈の出口を通らない。
// 取り合いは Validate 側でも見る必要がある。
func TestValidate_RejectsDuplicatePreviousItemID(t *testing.T) {
	t.Parallel()

	id := "PVTI_a"
	in := domain.Interpretation{
		Summary: "s",
		Items: []domain.InterpretedItem{
			{LocalID: "i1", Kind: domain.KindIssue, Title: "t", PreviousItemID: &id},
			{LocalID: "i2", Kind: domain.KindIssue, Title: "t", PreviousItemID: &id},
		},
	}

	var verrs domain.ValidationErrors
	if err := in.Validate(domain.GranularityAuto); !errors.As(err, &verrs) {
		t.Fatalf("Validate() = %v, want ValidationErrors", err)
	}
}

// 親は epic のタイトル文字列で指す（ADR 0006）。同名の epic が並ぶと、その配下は
// GitHub 上で 1 つにまとまり、どちらの子なのか区別が付かなくなる。エラーにならず
// 作成は最後まで通り、draft issue は削除できないので気づいた時点では取り返せない。
// **作る前に弾く。**
func TestInterpretation_Validate_RejectsDuplicateEpicTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		items []domain.InterpretedItem
		// wantFields は報告されてほしい Field。nil ならエラーなしを期待する。
		wantFields []string
	}{
		{
			name: "同じタイトルの epic が 2 件",
			items: []domain.InterpretedItem{
				{LocalID: "e1", Kind: domain.KindEpic, Title: "認証"},
				{LocalID: "e2", Kind: domain.KindEpic, Title: "認証"},
			},
			wantFields: []string{"items[1].title"},
		},
		{
			// Parent には生の title が入るので GitHub 上では別文字列になるが、
			// 人には見分けが付かない。「なぜか 2 つある」は同じように起きる。
			name: "前後の空白だけが違う epic",
			items: []domain.InterpretedItem{
				{LocalID: "e1", Kind: domain.KindEpic, Title: "認証"},
				{LocalID: "e2", Kind: domain.KindEpic, Title: "  認証\n"},
			},
			wantFields: []string{"items[1].title"},
		},
		{
			// 合成済みと結合文字。入力方式が違うだけで表示は同じ。
			// ComputeContentHash が NFC で揃えているのと同じ扱いにする。
			name: "Unicode の正規化形だけが違う epic",
			items: []domain.InterpretedItem{
				{LocalID: "e1", Kind: domain.KindEpic, Title: "ガード"},
				{LocalID: "e2", Kind: domain.KindEpic, Title: "ガード"},
			},
			wantFields: []string{"items[1].title"},
		},
		{
			// 行末の空白は画面上に現れない。前後の空白と同じ扱いにする。
			// normalizeText を再利用しているので、ここは自動的に畳まれる。
			name: "行末の空白だけが違う epic",
			items: []domain.InterpretedItem{
				{LocalID: "e1", Kind: domain.KindEpic, Title: "認証 \n方式"},
				{LocalID: "e2", Kind: domain.KindEpic, Title: "認証\n方式"},
			},
			wantFields: []string{"items[1].title"},
		},
		{
			// 改行コードの違いも同じ。編集環境の差でしかない。
			name: "改行コードだけが違う epic",
			items: []domain.InterpretedItem{
				{LocalID: "e1", Kind: domain.KindEpic, Title: "認証\r\n方式"},
				{LocalID: "e2", Kind: domain.KindEpic, Title: "認証\n方式"},
			},
			wantFields: []string{"items[1].title"},
		},
		{
			// 行の途中の空白は見分けが付く。畳むと壊れないものを弾くことになる。
			name: "行の途中の空白だけが違う epic",
			items: []domain.InterpretedItem{
				{LocalID: "e1", Kind: domain.KindEpic, Title: "認証 方式"},
				{LocalID: "e2", Kind: domain.KindEpic, Title: "認証方式"},
			},
		},
		{
			// 3 件目も 1 件目と衝突する。最初の 1 件で打ち切ると、LLM は
			// 1 往復で 1 箇所しか直せない（ADR 0005）。
			name: "同じタイトルの epic が 3 件",
			items: []domain.InterpretedItem{
				{LocalID: "e1", Kind: domain.KindEpic, Title: "認証"},
				{LocalID: "e2", Kind: domain.KindEpic, Title: "認証"},
				{LocalID: "e3", Kind: domain.KindEpic, Title: "認証"},
			},
			wantFields: []string{"items[1].title", "items[2].title"},
		},
		{
			// issue は親として指されない。同名でも構造は壊れないので弾かない。
			name: "同じタイトルの issue が 2 件",
			items: []domain.InterpretedItem{
				{LocalID: "i1", Kind: domain.KindIssue, Title: "ログイン"},
				{LocalID: "i2", Kind: domain.KindIssue, Title: "ログイン"},
			},
		},
		{
			name: "epic と issue が同じタイトル",
			items: []domain.InterpretedItem{
				{LocalID: "e1", Kind: domain.KindEpic, Title: "認証"},
				{LocalID: "i1", Kind: domain.KindIssue, Title: "認証"},
			},
		},
		{
			// 見分けが付くうえ、GitHub 側でも別文字列。畳むと壊れないものを弾く。
			name: "大文字小文字だけが違う epic",
			items: []domain.InterpretedItem{
				{LocalID: "e1", Kind: domain.KindEpic, Title: "API 設計"},
				{LocalID: "e2", Kind: domain.KindEpic, Title: "api 設計"},
			},
		},
		{
			// NFKC まで掛ければ同一視されるが、正規化は NFC で止める。
			name: "全角半角だけが違う epic",
			items: []domain.InterpretedItem{
				{LocalID: "e1", Kind: domain.KindEpic, Title: "API 設計"},
				{LocalID: "e2", Kind: domain.KindEpic, Title: "ＡＰＩ 設計"},
			},
		},
		{
			// 空のタイトルは title 自体の指摘で出る。重複としても報告すると、
			// 元を 1 箇所直せば消える指摘が修正指示に混ざる。
			name: "タイトルが空の epic が 2 件",
			items: []domain.InterpretedItem{
				{LocalID: "e1", Kind: domain.KindEpic, Title: ""},
				{LocalID: "e2", Kind: domain.KindEpic, Title: "   "},
			},
			wantFields: []string{"items[0].title", "items[1].title"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := domain.Interpretation{Summary: "この囲みの解釈", Items: tt.items}

			err := in.Validate(domain.GranularityAuto)
			if tt.wantFields == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}

			if err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			got := errorFields(t, err)
			if !slices.Equal(got, tt.wantFields) {
				t.Errorf("報告された Field = %v, want %v", got, tt.wantFields)
			}
		})
	}
}

// 重複の指摘は、どちらと衝突しているのかが読み取れる文にする。この文は
// そのまま LLM への修正指示になる（buildRetryMessage）。
func TestInterpretation_Validate_DuplicateEpicTitleMessage(t *testing.T) {
	t.Parallel()

	in := domain.Interpretation{
		Summary: "この囲みの解釈",
		Items: []domain.InterpretedItem{
			{LocalID: "e1", Kind: domain.KindEpic, Title: "認証"},
			{LocalID: "e2", Kind: domain.KindEpic, Title: "認証"},
		},
	}

	var verrs domain.ValidationErrors
	if err := in.Validate(domain.GranularityAuto); !errors.As(err, &verrs) {
		t.Fatalf("Validate() = %v, want ValidationErrors", err)
	}
	if len(verrs) != 1 {
		t.Fatalf("報告された件数 = %d, want 1 (%v)", len(verrs), verrs)
	}

	// 衝突した相手を localId で名指しする。タイトルだけを出すと、同じ文字列が
	// 並ぶ出力の中でどれを直せばよいかが決まらない。
	for _, want := range []string{`"認証"`, `"e1"`} {
		if !strings.Contains(verrs[0].Message, want) {
			t.Errorf("Message に %s が含まれていない: %q", want, verrs[0].Message)
		}
	}
}
