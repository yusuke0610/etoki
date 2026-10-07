package domain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
)

// fileReferenceRulePath は Go と TypeScript が共有する判定対象の置き場所。
//
// **web/src/excalidraw/fileReferenceRule.test.ts が同じファイルを読む。**
// 動かすなら両方を直す。
const fileReferenceRulePath = "../../testdata/file-reference-rule.json"

type fileReferenceRule struct {
	Cases []struct {
		Name    string          `json:"name"`
		FileIDs []string        `json:"fileIds"`
		Element json.RawMessage `json:"element"`
	} `json:"cases"`
}

// TestFileIDs_MatchesSharedRule は「どの画像を参照しているか」を共有の
// テストデータで固定する。
//
// **回帰止め。切れると何が起きるか。** 規則は Go（ここ）と TypeScript
// （web/src/excalidraw/files.ts）の 2 箇所にある。フロントは参照している画像の
// うちサーバーがまだ持っていないものを送り、サーバーは参照していない画像を
// 消す（ADR 0074）。片方だけ変えると、送らなかった画像が開き直したときに
// 欠けるか、送った画像が 400 で弾かれる。
func TestFileIDs_MatchesSharedRule(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Clean(fileReferenceRulePath))
	if err != nil {
		t.Fatalf("共有のテストデータを読めない: %v", err)
	}

	var rule fileReferenceRule
	if err := json.Unmarshal(raw, &rule); err != nil {
		t.Fatalf("unmarshal %s: %v", fileReferenceRulePath, err)
	}
	if len(rule.Cases) == 0 {
		t.Fatal("cases が空。読む先を間違えている")
	}

	for _, c := range rule.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			sceneJSON := `{"elements":[` + string(c.Element) + `]}`
			s, err := domain.ParseScene([]byte(sceneJSON))
			if err != nil {
				t.Fatalf("ParseScene: %v", err)
			}

			got := s.FileIDs()
			if !slices.Equal(got, c.FileIDs) {
				t.Errorf("FileIDs = %v, want %v (%s)", got, c.FileIDs, sceneJSON)
			}
		})
	}
}

// 同じ画像を貼った要素が 2 つあっても、ID は 1 つだけ返す。並びは要素の順。
//
// 重複を返すと、保存の「参照している画像」を数えるところで同じ画像を 2 回
// 足すことになる。
func TestFileIDs_DedupesAndKeepsElementOrder(t *testing.T) {
	t.Parallel()

	s := scene(t,
		`{"id":"a","type":"image","fileId":"f-2"}`,
		`{"id":"b","type":"image","fileId":"f-1"}`,
		`{"id":"c","type":"image","fileId":"f-2"}`,
	)

	if got, want := s.FileIDs(), []string{"f-2", "f-1"}; !slices.Equal(got, want) {
		t.Errorf("FileIDs = %v, want %v", got, want)
	}
}

// 画像の実体を抱えているか。Excalidraw は画像が無くても `files: {}` を書くので、
// 空は抱えていないと読む。
func TestHasFiles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"files が無い", `{"elements":[]}`, false},
		{"files が null", `{"elements":[],"files":null}`, false},
		{"files が空", `{"elements":[],"files":{}}`, false},
		{"files に画像がある", `{"elements":[],"files":{"f-1":{"id":"f-1"}}}`, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			s, err := domain.ParseScene([]byte(c.raw))
			if err != nil {
				t.Fatalf("ParseScene: %v", err)
			}
			if got := s.HasFiles(); got != c.want {
				t.Errorf("HasFiles = %v, want %v", got, c.want)
			}
		})
	}
}
