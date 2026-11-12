package usecase_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// title が空なので items[0].title で弾かれる。
const invalidLLMOutput = `{"summary":"決済まわりの課題出し","items":[
	{"localId":"e1","kind":"epic","title":"","body":"","parentLocalId":null}]}`

func newInterpretService(t *testing.T, board *port.Board, llm *fakeLLM) (*usecase.InterpretationService, *fakeBoards) {
	t.Helper()

	boards := &fakeBoards{board: board}
	svc := usecase.NewInterpretationService(
		boards, &fakeMappings{}, llm, newLimiter(), usecase.WithMaxAttempts(3))
	return svc, boards
}

func TestInterpret_Succeeds(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, boards := newInterpretService(t, newBoard(interpretScene), llm)

	result, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	in := result.Interpretation

	if len(llm.requests) != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
	}
	if len(in.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(in.Items))
	}
	if in.Items[0].Kind != domain.KindEpic {
		t.Errorf("Items[0].Kind = %q", in.Items[0].Kind)
	}
	if in.Summary == "" {
		t.Error("Summary が空")
	}
	if result.ContentHash != currentContentHash(t) {
		t.Errorf("ContentHash = %q, want %q", result.ContentHash, currentContentHash(t))
	}

	// 解釈は読むだけ。ボードにも実行記録にも何も書かない。
	if boards.writes != 0 {
		t.Errorf("ボードへの書き込み = %d 回, want 0", boards.writes)
	}
}

func TestInterpret_BuildsPrompt(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}

	req := llm.requests[0]

	for _, want := range []string{"Stripe の SDK が古い", "返金導線が無い", "決済まわり"} {
		if !strings.Contains(req.Text, want) {
			t.Errorf("プロンプトに %q が無い:\n%s", want, req.Text)
		}
	}
	// 囲みの外のテキストは注釈の内容ではない。
	if strings.Contains(req.Text, "囲みの外のメモ") {
		t.Errorf("囲みの外のテキストがプロンプトに入っている:\n%s", req.Text)
	}
	if !strings.Contains(req.Text, "粒度の指定はありません") {
		t.Errorf("粒度の指示が無い:\n%s", req.Text)
	}
	if !strings.Contains(req.System, "epic") || !strings.Contains(req.System, "localId") {
		t.Errorf("システム指示にスキーマの説明が無い:\n%s", req.System)
	}
	// 画像なしでも解釈は成立する（ADR 0018）。無いものを添えたことにしない。
	if len(req.Images) != 0 {
		t.Errorf("len(Images) = %d, want 0", len(req.Images))
	}
	if strings.Contains(req.Text, "画像") {
		t.Errorf("画像を渡していないのに画像の話がプロンプトにある:\n%s", req.Text)
	}
}

// pngImage はテスト用の画像。中身は検証していないので PNG である必要はない。
func pngImage(size int) port.Image {
	return port.Image{MediaType: "image/png", Data: make([]byte, size)}
}

func TestInterpret_PassesImageToLLM(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	img := pngImage(16)
	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", []port.Image{img}); err != nil {
		t.Fatalf("interpret() = %v", err)
	}

	req := llm.requests[0]
	if len(req.Images) != 1 {
		t.Fatalf("len(Images) = %d, want 1", len(req.Images))
	}
	if req.Images[0].MediaType != "image/png" || len(req.Images[0].Data) != 16 {
		t.Errorf("Images[0] = %+v", req.Images[0])
	}
	// テキストと画像の役割分担を書かないと、モデルは画像の文字を読み直して
	// 囲みの外の文言まで拾う。
	if !strings.Contains(req.Text, "画像") {
		t.Errorf("画像の役割がプロンプトに書かれていない:\n%s", req.Text)
	}
	// 文言はテキスト一覧が正。ここが崩れると、囲みの範囲を frame で決めている
	// 意味が無くなる。
	if !strings.Contains(req.Text, "テキスト一覧") {
		t.Errorf("文言の正がどちらかがプロンプトに書かれていない:\n%s", req.Text)
	}
}

// 再送は元の指示ごと組み立て直す。画像を落とすと、2 回目以降は入力が変わる。
func TestInterpret_KeepsImageOnRetry(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{invalidLLMOutput, validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	if _, err := interpret(t, svc,
		t.Context(), "board-1", "annot-1", []port.Image{pngImage(16)}); err != nil {
		t.Fatalf("interpret() = %v", err)
	}

	if len(llm.requests) != 2 {
		t.Fatalf("LLM 呼び出し回数 = %d, want 2", len(llm.requests))
	}
	if len(llm.requests[1].Images) != 1 {
		t.Errorf("再送の len(Images) = %d, want 1", len(llm.requests[1].Images))
	}
}

// 上限を超えても縮小せず弾く。黙って劣化させると、渡したはずの情報が消えた
// ことに気づけない（ADR 0018）。
func TestInterpret_RejectsInvalidImages(t *testing.T) {
	t.Parallel()

	tests := map[string][]port.Image{
		"上限を超えた画像": {pngImage(usecase.MaxImageBytes + 1)},
		"枚数が上限を超える": func() []port.Image {
			imgs := make([]port.Image, usecase.MaxImages+1)
			for i := range imgs {
				imgs[i] = pngImage(16)
			}
			return imgs
		}(),
		"PNG 以外": {{MediaType: "image/jpeg", Data: make([]byte, 16)}},
		"中身が空":   {{MediaType: "image/png"}},
	}

	for name, images := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			llm := &fakeLLM{responses: []string{validLLMOutput}}
			svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

			_, err := interpret(t, svc, t.Context(), "board-1", "annot-1", images)
			if !errors.Is(err, usecase.ErrInvalidInput) {
				t.Fatalf("interpret() = %v, want ErrInvalidInput", err)
			}
			// 弾くと決めた入力で LLM を叩くと、弾いたのに課金だけ発生する。
			if len(llm.requests) != 0 {
				t.Errorf("弾いたのに LLM を呼んでいる: %d 回", len(llm.requests))
			}
		})
	}
}

func TestInterpret_PassesGranularityInstruction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		granularity string
		want        string
	}{
		{granularity: "epic", want: "epic 相当"},
		{granularity: "issue", want: "issue 相当"},
	}

	for _, tt := range tests {
		t.Run(tt.granularity, func(t *testing.T) {
			t.Parallel()

			scene := strings.Replace(interpretScene, `"granularity":""`,
				`"granularity":"`+tt.granularity+`"`, 1)

			// epic 指定でも issue 指定でも通る出力を返す必要はない。
			// プロンプトさえ確かめられればよいので、失敗しても構わない。
			llm := &fakeLLM{responses: []string{validLLMOutput}}
			svc, _ := newInterpretService(t, newBoard(scene), llm)

			_, _ = interpret(t, svc, t.Context(), "board-1", "annot-1", nil)

			if len(llm.requests) == 0 {
				t.Fatal("LLM が呼ばれていない")
			}
			if !strings.Contains(llm.requests[0].Text, tt.want) {
				t.Errorf("プロンプトに %q が無い:\n%s", tt.want, llm.requests[0].Text)
			}
		})
	}
}

// 出力例が epic を含んだままだと、issue 指定でも小型モデルは例をなぞって epic を
// 返す（issue #251）。例は指定した粒度で通る形にする。
//
// 制約の文面ではなく「出力例の kind」を見る。issue 指定の制約は epic という語を
// 含むので、語の有無では区別できない。
func TestInterpret_SystemPromptExampleFollowsGranularity(t *testing.T) {
	t.Parallel()

	const (
		epicKind  = `"kind": "epic"`
		issueKind = `"kind": "issue"`
	)

	tests := []struct {
		granularity string
		wantEpic    bool
	}{
		{granularity: "issue", wantEpic: false},
		{granularity: "epic", wantEpic: true},
		// 指定なしは今までと同じ例（epic と issue の両方）。
		{granularity: "", wantEpic: true},
	}

	for _, tt := range tests {
		t.Run("granularity="+tt.granularity, func(t *testing.T) {
			t.Parallel()

			scene := strings.Replace(interpretScene, `"granularity":""`,
				`"granularity":"`+tt.granularity+`"`, 1)
			llm := &fakeLLM{responses: []string{validLLMOutput}}
			svc, _ := newInterpretService(t, newBoard(scene), llm)

			_, _ = interpret(t, svc, t.Context(), "board-1", "annot-1", nil)

			if len(llm.requests) == 0 {
				t.Fatal("LLM が呼ばれていない")
			}
			system := llm.requests[0].System

			if !strings.Contains(system, issueKind) {
				t.Errorf("出力例に issue が無い:\n%s", system)
			}
			if got := strings.Contains(system, epicKind); got != tt.wantEpic {
				t.Errorf("出力例に epic がある = %v, want %v:\n%s", got, tt.wantEpic, system)
			}
		})
	}
}

// 前回の一覧を渡していないときは、出力例のどの項目も previousRef を null にする。
//
// epic を含む例の issue に p1 を残すと、小型モデル（gemma3:4b）はそれを書き写し、
// 3 回とも検証に落ちて解釈が失敗した。issue 指定で起きた #251 と同じ形が、
// epic と指定なしに残っていた。前回の一覧があるときは、ref の書き方を例で見せる。
// examplePreviousRef は出力例の previousRef の値を拾う。
var examplePreviousRef = regexp.MustCompile(`"previousRef":\s*(null|"[^"]*")`)

func TestInterpret_SystemPromptExampleRefFollowsPrevious(t *testing.T) {
	t.Parallel()

	const exampleRef = `"previousRef": "p1"`

	for _, granularity := range []string{"", "epic"} {
		for _, hasPrevious := range []bool{false, true} {
			t.Run(fmt.Sprintf("granularity=%q/previous=%v", granularity, hasPrevious), func(t *testing.T) {
				t.Parallel()

				mappings := &fakeMappings{}
				if hasPrevious {
					if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
						BoardID: "board-1", AnnotationID: "annot-1", ContentHash: "old", CreatedAt: baseTime,
						Items: []port.SyncItem{{
							ItemID: "PVTI_first", LocalID: "e1", Kind: port.KindEpic, Title: "決済基盤",
							Action: port.ActionCreated, Confirmed: true, CreatedAt: baseTime,
						}},
					}); err != nil {
						t.Fatalf("seed run: %v", err)
					}
				}

				scene := strings.Replace(interpretScene, `"granularity":""`,
					`"granularity":"`+granularity+`"`, 1)
				llm := &fakeLLM{responses: []string{validLLMOutput}}
				svc := usecase.NewInterpretationService(
					&fakeBoards{board: newBoard(scene)}, mappings, llm, newLimiter(),
					usecase.WithMaxAttempts(1))

				if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
					t.Fatalf("interpret() = %v", err)
				}
				system := llm.requests[0].System
				if got := strings.Contains(system, exampleRef); got != hasPrevious {
					t.Errorf("出力例に %s がある = %v, want %v:\n%s", exampleRef, got, hasPrevious, system)
				}
				if hasPrevious {
					return
				}
				// p1 が無いだけでは足りない。別の ref（p2 など）を見せても上の断言は
				// 通るが、写されれば存在しない ref として弾かれる。出力例の
				// previousRef は全部 null であることを見る。
				refs := examplePreviousRef.FindAllStringSubmatch(system, -1)
				if len(refs) == 0 {
					t.Fatalf("出力例に previousRef が無い:\n%s", system)
				}
				for _, m := range refs {
					if m[1] != "null" {
						t.Errorf("前回が無いのに出力例の previousRef = %s, want null:\n%s", m[1], system)
					}
				}
			})
		}
	}
}

// 解釈には、図形の幅で折り返す前の文字を渡す。折り返したあとの文字の改行を空白に
// 置き換えると、日本語の語の途中に空白が入り（「チームの開 発者」）、小型モデルは
// それを title に写した。originalText を持たない古いシーンは、これまでどおり text。
func TestInterpret_SendsUnwrappedText(t *testing.T) {
	t.Parallel()

	scene := `{"type":"excalidraw","elements":[
	{"id":"annot-1","type":"frame","name":"通知","customData":{"etoki":{"granularity":""}}},
	{"id":"t1","type":"text","text":"チームの開\n発者","originalText":"チームの開発者","frameId":"annot-1"},
	{"id":"t2","type":"text","text":"古い\nシーン","frameId":"annot-1"}]}`
	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(scene), llm)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	sent := llm.requests[0].Text

	for _, want := range []string{"- チームの開発者\n", "- 古い シーン\n"} {
		if !strings.Contains(sent, want) {
			t.Errorf("プロンプトに %q が無い:\n%s", want, sent)
		}
	}
	if strings.Contains(sent, "開 発者") {
		t.Errorf("折り返したあとの文字を渡している:\n%s", sent)
	}
}

// 人がつないだ矢印は、結び先とラベルを文字でも渡す（ADR 0096）。テキストの一覧には
// ラベルしか並ばず、誰から誰へのやり取りかは画像にしか無かった。小型モデルは画像から
// それを読めず、ラベルを言い換えるだけの issue を作った。
//
// 節はテキストの節の後ろに空行で区切って置く。偽の LLM はテキストの節だけを読む。
func TestInterpret_SendsArrowConnections(t *testing.T) {
	t.Parallel()

	scene := `{"type":"excalidraw","elements":[
	{"id":"annot-1","type":"frame","customData":{"etoki":{"granularity":"","kind":"sequence"}}},
	{"id":"u","type":"rectangle","frameId":"annot-1"},
	{"id":"u-l","type":"text","text":"User","containerId":"u"},
	{"id":"w","type":"rectangle","frameId":"annot-1"},
	{"id":"w-l","type":"text","text":"WebApp","containerId":"w"},
	{"id":"a1","type":"arrow","frameId":"annot-1","startBinding":{"elementId":"u"},"endBinding":{"elementId":"w"},"endArrowhead":"arrow"},
	{"id":"a1-l","type":"text","text":"リクエスト送信","containerId":"a1"},
	{"id":"a2","type":"arrow","frameId":"annot-1","startBinding":{"elementId":"w"},"endBinding":{"elementId":"u"}},
	{"id":"a2-l","type":"text","text":"レスポンス","containerId":"a2"}]}`
	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(scene), llm)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	sent := llm.requests[0].Text

	want := "\n\n囲みの中の矢印（描いた人がつないだ先。並びは図の上の順番とは限りません）:\n" +
		"- User → WebApp「リクエスト送信」\n" +
		"- WebApp ― User「レスポンス」\n"
	if !strings.Contains(sent, want) {
		t.Errorf("プロンプトに矢印の節が無い。want:\n%s\ngot:\n%s", want, sent)
	}
	// テキストの節（最初の空行まで）には矢印の行を混ぜない。
	if textSection, _, _ := strings.Cut(sent, "\n\n"); strings.Contains(textSection, "→") {
		t.Errorf("テキストの節に矢印の行がある:\n%s", textSection)
	}
}

// つないだ矢印が 1 本も無ければ節ごと省く。空の一覧を見せると、モデルが
// 「あるはずのもの」を埋め合わせ始める（前回ぶんと同じ）。
func TestInterpret_OmitsConnectionsSectionWhenNone(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	if sent := llm.requests[0].Text; strings.Contains(sent, "囲みの中の矢印") {
		t.Errorf("矢印が無いのに節が出ている:\n%s", sent)
	}
}

// 端の呼び名と矢じりの記号。囲みの外の要素は名前を出さない（ADR 0093）。
// 向きを決めるのは矢じりで、つないだ順ではない。
func TestConnectionsSection_DescribesEndsAndHeads(t *testing.T) {
	t.Parallel()

	user := domain.ConnectionEnd{Bound: true, Inside: true, Name: "User"}
	web := domain.ConnectionEnd{Bound: true, Inside: true, Name: "Web\nApp"}
	tests := []struct {
		name string
		conn domain.Connection
		want string
	}{
		{"終点に矢じり", domain.Connection{From: user, To: web, ToHead: true}, "- User → Web App\n"},
		{"始点に矢じり", domain.Connection{From: user, To: web, FromHead: true}, "- User ← Web App\n"},
		{"両方に矢じり", domain.Connection{From: user, To: web, FromHead: true, ToHead: true}, "- User ↔ Web App\n"},
		{"矢じりなし", domain.Connection{From: user, To: web}, "- User ― Web App\n"},
		{"囲みの外", domain.Connection{From: user, To: domain.ConnectionEnd{Bound: true}, ToHead: true},
			"- User → （囲みの外）\n"},
		{"つながっていない", domain.Connection{From: user, ToHead: true}, "- User → （どこにもつながっていない）\n"},
		{"文字の無い図形", domain.Connection{From: user, To: domain.ConnectionEnd{Bound: true, Inside: true}, ToHead: true},
			"- User → （文字の無い図形）\n"},
		{"ラベル", domain.Connection{From: user, To: web, ToHead: true, Label: "1 対\n多"}, "- User → Web App「1 対 多」\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := usecase.ConnectionsSection([]domain.Connection{tt.conn})
			if !strings.HasSuffix(got, tt.want) {
				t.Errorf("ConnectionsSection() = %q, want 末尾が %q", got, tt.want)
			}
		})
	}
}

// 見積もりは、書いた節の長さを下回らない。下回ると上限の検査をすり抜ける。
// 畳む空白が無く、名前が「（文字の無い図形）」より長ければ見積もりはちょうどに
// なる（式が書き方からずれていない）。短い名前は、空白だけの名前と見分けずに
// その呼び名の長さで見積もるので、ちょうどにはならない。
func TestConnectionsSectionMaxLen_BoundsSection(t *testing.T) {
	t.Parallel()

	user := domain.ConnectionEnd{Bound: true, Inside: true, Name: "利用者（ブラウザから操作する人）"}
	web := domain.ConnectionEnd{Bound: true, Inside: true, Name: "WebApp（注文を受け付けるサーバー）"}
	short := domain.ConnectionEnd{Bound: true, Inside: true, Name: "DB"}
	spaced := domain.ConnectionEnd{Bound: true, Inside: true, Name: "Web\n\n  App（注文を受け付けるサーバー）"}
	blank := domain.ConnectionEnd{Bound: true, Inside: true, Name: "  "}
	tests := []struct {
		name  string
		conns []domain.Connection
		exact bool
	}{
		{"無い", nil, true},
		{"名前とラベル", []domain.Connection{{From: user, To: web, ToHead: true, Label: "1対多"}}, true},
		{"両方に矢じり", []domain.Connection{{From: user, To: web, FromHead: true, ToHead: true}}, true},
		{"囲みの外", []domain.Connection{{From: user, To: domain.ConnectionEnd{Bound: true}}}, true},
		{"つながっていない", []domain.Connection{{From: user, ToHead: true}}, true},
		{"文字の無い図形", []domain.Connection{{From: user, To: domain.ConnectionEnd{Bound: true, Inside: true}}}, true},
		{"空白だけの名前", []domain.Connection{{From: user, To: blank}}, true},
		{"短い名前", []domain.Connection{{From: user, To: short}}, false},
		{"畳まれる空白", []domain.Connection{{From: spaced, To: web, Label: "1 対\n\n多"}}, false},
		{"空白だけのラベル", []domain.Connection{{From: user, To: web, Label: "  "}}, false},
		{"複数", []domain.Connection{{From: user, To: web}, {From: web, To: user, ToHead: true, Label: "応答"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := usecase.ConnectionsSectionMaxLen(tt.conns)
			written := len(usecase.ConnectionsSection(tt.conns))
			if got < written {
				t.Errorf("ConnectionsSectionMaxLen() = %d, 書いた節は %d バイト", got, written)
			}
			if tt.exact && got != written {
				t.Errorf("ConnectionsSectionMaxLen() = %d, want ちょうど %d", got, written)
			}
		})
	}
}

// 矢印の節は、同じ図形の名前を矢印の本数ぶん書く。保存の上限では縛れないので、
// 組み立てる前に測る。ちょうど上限なら通し、1 バイト超えたら LLM を呼ばずに
// 弾く。切り詰めると矢印が黙って欠ける。
func TestInterpret_ConnectionsSectionLimit(t *testing.T) {
	t.Parallel()

	const nameLen = 64 << 10
	name := strings.Repeat("x", nameLen)
	// labelLen が 0 ならラベルを付けない。
	build := func(arrows, labelLen int) string {
		var b strings.Builder
		b.WriteString(`{"type":"excalidraw","elements":[` +
			`{"id":"annot-1","type":"frame","customData":{"etoki":{"granularity":""}}},` +
			`{"id":"u","type":"rectangle","frameId":"annot-1"},` +
			`{"id":"u-l","type":"text","text":"` + name + `","containerId":"u"}`)
		for i := range arrows {
			fmt.Fprintf(&b, `,{"id":"a%d","type":"arrow","frameId":"annot-1","startBinding":{"elementId":"u"},"endArrowhead":"arrow"}`, i)
		}
		if labelLen > 0 {
			b.WriteString(`,{"id":"a0-l","type":"text","text":"` + strings.Repeat("y", labelLen) + `","containerId":"a0"}`)
		}
		b.WriteString(`]}`)
		return b.String()
	}
	measure := func(scene string) int {
		s, err := domain.ParseScene([]byte(scene))
		if err != nil {
			t.Fatalf("ParseScene() = %v", err)
		}
		return usecase.ConnectionsSectionMaxLen(s.AnnotationConnections("annot-1"))
	}

	// 上限にちょうど届く本数とラベルの長さを決める。ラベルは「」で 6 バイト
	// 足すので、余りがそれより小さければ 1 本減らす。
	line := measure(build(2, 0)) - measure(build(1, 0))
	header := measure(build(1, 0)) - line
	arrows := (usecase.MaxConnectionsSectionBytes - header) / line
	rest := usecase.MaxConnectionsSectionBytes - header - arrows*line
	if rest <= len("「」") {
		arrows--
		rest += line
	}
	labelLen := rest - len("「」")

	tests := []struct {
		name     string
		labelLen int
		wantErr  bool
	}{
		{"ちょうど上限", labelLen, false},
		{"1 バイト超え", labelLen + 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scene := build(arrows, tt.labelLen)
			if got, want := measure(scene), usecase.MaxConnectionsSectionBytes+tt.labelLen-labelLen; got != want {
				t.Fatalf("節の見積もり = %d, want %d（組み立てが狙いからずれた）", got, want)
			}
			llm := &fakeLLM{responses: []string{validLLMOutput}}
			svc, _ := newInterpretService(t, newBoard(scene), llm)

			_, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("interpret() = %v", err)
				}
				return
			}
			if !errors.Is(err, usecase.ErrInvalidInput) {
				t.Fatalf("interpret() = %v, want ErrInvalidInput", err)
			}
			if len(llm.requests) != 0 {
				t.Error("矢印の節が上限を超えたのに LLM を呼んでいる")
			}
		})
	}
}

// 再送は「何が悪かったか」を伝えて初めて意味を持つ。
func TestInterpret_RetriesWithCorrections(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{invalidLLMOutput, validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	result, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	in := result.Interpretation
	if len(in.Items) != 2 {
		t.Errorf("len(Items) = %d, want 2", len(in.Items))
	}

	if len(llm.requests) != 2 {
		t.Fatalf("LLM 呼び出し回数 = %d, want 2", len(llm.requests))
	}

	retry := llm.requests[1].Text
	if !strings.Contains(retry, "items[0].title") {
		t.Errorf("再送に検証エラーの箇所が含まれていない:\n%s", retry)
	}
	if !strings.Contains(retry, "title を空にはできません") {
		t.Errorf("再送に検証エラーの内容が含まれていない:\n%s", retry)
	}
	if !strings.Contains(retry, invalidLLMOutput) {
		t.Errorf("再送に直前の出力が含まれていない:\n%s", retry)
	}
	// VisionRequest は会話履歴を運べないので、再送も自己完結している必要がある。
	if !strings.Contains(retry, "Stripe の SDK が古い") {
		t.Errorf("再送に元の入力が含まれていない:\n%s", retry)
	}
}

func TestInterpret_GivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{invalidLLMOutput}}
	boards := &fakeBoards{board: newBoard(interpretScene)}
	svc := usecase.NewInterpretationService(
		boards, &fakeMappings{}, llm, newLimiter(), usecase.WithMaxAttempts(2))

	_, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
	if !errors.Is(err, usecase.ErrInterpretationFailed) {
		t.Fatalf("interpret() = %v, want ErrInterpretationFailed", err)
	}

	if len(llm.requests) != 2 {
		t.Errorf("LLM 呼び出し回数 = %d, want 2", len(llm.requests))
	}
	// 最後の指摘が残っていないと、開発者は何が起きたのか分からない。
	if !strings.Contains(err.Error(), "items[0].title") {
		t.Errorf("エラーに最後の指摘が含まれていない: %v", err)
	}
}

func TestInterpret_AcceptsFencedOutput(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"コードフェンス付き":  "```json\n" + validLLMOutput + "\n```",
		"言語指定なしフェンス": "```\n" + validLLMOutput + "\n```",
		"前置きと後書き付き":  "解釈しました。\n" + validLLMOutput + "\n以上です。",
	}

	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			llm := &fakeLLM{responses: []string{output}}
			svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

			result, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
			if err != nil {
				t.Fatalf("interpret() = %v", err)
			}
			in := result.Interpretation
			if len(in.Items) != 2 {
				t.Errorf("len(Items) = %d, want 2", len(in.Items))
			}
			// 1 往復で読めたなら再送していないはず。
			if len(llm.requests) != 1 {
				t.Errorf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
			}
		})
	}
}

func TestInterpret_RetriesOnUnparsableOutput(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{"JSON は出せません。", validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	if len(llm.requests) != 2 {
		t.Fatalf("LLM 呼び出し回数 = %d, want 2", len(llm.requests))
	}
	if !strings.Contains(llm.requests[1].Text, "指摘:") {
		t.Errorf("再送に指摘が含まれていない:\n%s", llm.requests[1].Text)
	}
}

// 接続やモデル側の失敗は修正指示で直る問題ではないので再送しない。
func TestInterpret_DoesNotRetryTransportErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("api key is invalid")
	llm := &fakeLLM{err: wantErr}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	_, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("interpret() = %v, want %v", err, wantErr)
	}
	// スキーマ違反とは原因も打ち手も違うので、呼び出し側が区別できる必要がある。
	if !errors.Is(err, usecase.ErrLLMUnavailable) {
		t.Errorf("interpret() = %v, want ErrLLMUnavailable", err)
	}
	if errors.Is(err, usecase.ErrInterpretationFailed) {
		t.Errorf("接続の失敗がスキーマ違反として扱われている: %v", err)
	}
	if len(llm.requests) != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
	}
}

func TestInterpret_NotFound(t *testing.T) {
	t.Parallel()

	t.Run("ボードが無い", func(t *testing.T) {
		t.Parallel()

		llm := &fakeLLM{responses: []string{validLLMOutput}}
		svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

		_, err := interpret(t, svc, t.Context(), "no-such-board", "annot-1", nil)
		if !errors.Is(err, usecase.ErrBoardNotFound) {
			t.Fatalf("interpret() = %v, want ErrBoardNotFound", err)
		}
		if len(llm.requests) != 0 {
			t.Error("ボードが無いのに LLM を呼んでいる")
		}
	})

	t.Run("注釈が無い", func(t *testing.T) {
		t.Parallel()

		llm := &fakeLLM{responses: []string{validLLMOutput}}
		svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

		_, err := interpret(t, svc, t.Context(), "board-1", "no-such-annot", nil)
		if !errors.Is(err, usecase.ErrAnnotationNotFound) {
			t.Fatalf("interpret() = %v, want ErrAnnotationNotFound", err)
		}
		if len(llm.requests) != 0 {
			t.Error("注釈が無いのに LLM を呼んでいる")
		}
	})

	// frame でも customData.etoki が無ければ注釈ではない。
	t.Run("注釈ではない frame", func(t *testing.T) {
		t.Parallel()

		scene := `{"elements":[{"id":"f1","type":"frame","name":"ただの枠"}]}`
		llm := &fakeLLM{responses: []string{validLLMOutput}}
		svc, _ := newInterpretService(t, newBoard(scene), llm)

		_, err := interpret(t, svc, t.Context(), "board-1", "f1", nil)
		if !errors.Is(err, usecase.ErrAnnotationNotFound) {
			t.Fatalf("interpret() = %v, want ErrAnnotationNotFound", err)
		}
	})
}

// 図形と矢印だけの囲みがありうるので、テキストが無くても打ち切らない。
// その中身は画像でしか渡せないので、ここで弾くと画像を添えた解釈まで塞ぐ。
func TestInterpret_InterpretsAnnotationWithoutTexts(t *testing.T) {
	t.Parallel()

	// 名前なしの frame。AnnotationTexts は 1 件も返さない。
	scene := `{"elements":[{"id":"annot-1","type":"frame","customData":{"etoki":{"granularity":""}}}]}`
	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(scene), llm)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	if len(llm.requests) != 1 {
		t.Fatalf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
	}
	// 黙って空にすると、モデルが入力の欠落を疑って作り話を始める。
	if !strings.Contains(llm.requests[0].Text, "(なし)") {
		t.Errorf("テキストが無いことが伝わっていない:\n%s", llm.requests[0].Text)
	}
}

// 上限に 0 以下を渡しても、1 度も呼ばずに失敗しては何が起きたか追えない。
func TestInterpret_IgnoresNonPositiveMaxAttempts(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	boards := &fakeBoards{board: newBoard(interpretScene)}
	svc := usecase.NewInterpretationService(
		boards, &fakeMappings{}, llm, newLimiter(), usecase.WithMaxAttempts(0))

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	if len(llm.requests) != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
	}
}

// 種別を選んで始めた囲みは、何の図として読むかをプロンプトで伝える。
//
// **伝えないと、テンプレートがただの絵で終わる。** シーケンス図として描かれた
// ものも ER 図として描かれたものも、同じテキスト一覧として渡ることになる。
func TestInterpret_PassesKindInstruction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind domain.DiagramKind
		want string
	}{
		{kind: domain.DiagramKindTodo, want: "やることの洗い出し"},
		{kind: domain.DiagramKindMindmap, want: "マインドマップ"},
		{kind: domain.DiagramKindSequence, want: "シーケンス図"},
		{kind: domain.DiagramKindER, want: "ER 図"},
		{kind: domain.DiagramKindArchitecture, want: "システム構成図"},
	}

	// 全種類を並べる。1 つだけ確かめると、表に載せ忘れた種別が黙って
	// 「指定なし」と同じ扱いに落ちても緑のままになる。
	if len(tests) != len(domain.DiagramKinds()) {
		t.Fatalf("確かめている種別が %d 件、DiagramKinds は %d 件",
			len(tests), len(domain.DiagramKinds()))
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Parallel()

			scene := strings.Replace(interpretScene, `"granularity":""`,
				`"granularity":"","kind":"`+string(tt.kind)+`"`, 1)

			llm := &fakeLLM{responses: []string{validLLMOutput}}
			svc, _ := newInterpretService(t, newBoard(scene), llm)

			if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
				t.Fatalf("interpret() = %v", err)
			}
			if len(llm.requests) == 0 {
				t.Fatal("LLM が呼ばれていない")
			}
			if !strings.Contains(llm.requests[0].Text, tt.want) {
				t.Errorf("プロンプトに %q が無い:\n%s", tt.want, llm.requests[0].Text)
			}
		})
	}
}

// 種別を選んでいない囲みには、種別の話をいっさい足さない。
//
// 「指定されていません」と書くと、モデルが図の種類を当てにいく余地を作る。
// 指定なしの入力は、この機能が入る前と同じでなければならない。
func TestInterpret_SaysNothingAboutKindWhenUnspecified(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}

	if strings.Contains(llm.requests[0].Text, "として描かれています") {
		t.Errorf("種別を選んでいないのに図の種類の話がある:\n%s", llm.requests[0].Text)
	}
}

// 知らない種別は既定に倒さず弾く。粒度と同じ扱い。
func TestInterpret_RejectsUnknownKind(t *testing.T) {
	t.Parallel()

	scene := strings.Replace(interpretScene, `"granularity":""`,
		`"granularity":"","kind":"gantt"`, 1)
	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(scene), llm)

	_, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("interpret() = %v, want ErrInvalidInput", err)
	}
	if len(llm.requests) != 0 {
		t.Error("種別が不正なのに LLM を呼んでいる")
	}
}

// 既定に倒して進めると、開発者が指定したつもりの制約が黙って外れる。
func TestInterpret_RejectsUnknownGranularity(t *testing.T) {
	t.Parallel()

	scene := strings.Replace(interpretScene, `"granularity":""`, `"granularity":"project"`, 1)
	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(scene), llm)

	_, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("interpret() = %v, want ErrInvalidInput", err)
	}
	if len(llm.requests) != 0 {
		t.Error("粒度が不正なのに LLM を呼んでいる")
	}
}

// 粒度指定はプロンプトでも伝えるが、従わない出力は検証で弾いて再送する。
func TestInterpret_EnforcesGranularityOnOutput(t *testing.T) {
	t.Parallel()

	scene := strings.Replace(interpretScene, `"granularity":""`, `"granularity":"issue"`, 1)
	onlyIssues := `{"summary":"返金導線の整理","items":[
		{"localId":"i1","kind":"issue","title":"返金導線の整理","body":"","parentLocalId":null}]}`

	// 1 回目は epic を含む出力（issue 指定では通らない）。
	llm := &fakeLLM{responses: []string{validLLMOutput, onlyIssues}}
	svc, _ := newInterpretService(t, newBoard(scene), llm)

	result, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	if len(llm.requests) != 2 {
		t.Fatalf("LLM 呼び出し回数 = %d, want 2", len(llm.requests))
	}
	for _, it := range result.Interpretation.Items {
		if it.Kind == domain.KindEpic {
			t.Error("issue 指定なのに epic が通っている")
		}
	}
}

// 前回ぶんはプロンプトに載り、LLM は node ID ではなく短い ref を見る（ADR 0026）。
//
// ここが抜けると、対応づけの入力そのものが届いていなくても他のテストは緑のまま
// 通る。ref の採番とプロンプトの節を固定する。
func TestInterpret_ShowsPreviousItemsAsRefs(t *testing.T) {
	t.Parallel()

	mappings := &fakeMappings{}
	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1", ContentHash: "old", CreatedAt: baseTime,
		Items: []port.SyncItem{
			{
				ItemID: "PVTI_first", LocalID: "e1", Kind: port.KindEpic,
				Title: "決済基盤", Body: "入口をまとめる",
				Action: port.ActionCreated, Confirmed: true, CreatedAt: baseTime,
			},
			{
				ItemID: "PVTI_second", LocalID: "i1", Kind: port.KindIssue,
				Title: "カード決済", Action: port.ActionCreated, Confirmed: true,
				CreatedAt: baseTime,
			},
		},
	}); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc := usecase.NewInterpretationService(
		&fakeBoards{board: newBoard(interpretScene)}, mappings, llm, newLimiter(),
		usecase.WithMaxAttempts(1))

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	if len(llm.requests) != 1 {
		t.Fatalf("LLM の呼び出し = %d 回, want 1", len(llm.requests))
	}

	sent := llm.requests[0].Text

	// 畳み込みの並び順に p1 から振る。番号がずれると対応づけ先が入れ替わる。
	for _, want := range []string{"p1", "決済基盤", "入口をまとめる", "p2", "カード決済"} {
		if !strings.Contains(sent, want) {
			t.Errorf("プロンプトに %q が無い:\n%s", want, sent)
		}
	}

	// **node ID は見せない。** 長く不透明で、復唱させると取り違える。
	for _, unwanted := range []string{"PVTI_first", "PVTI_second"} {
		if strings.Contains(sent, unwanted) {
			t.Errorf("プロンプトに node ID が漏れている（%q）:\n%s", unwanted, sent)
		}
	}
}

// 前回ぶんが無ければ節ごと省く。空の一覧を見せると、モデルが「あるはずのもの」を
// 埋め合わせ始める。
func TestInterpret_OmitsPreviousSectionWhenEmpty(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}

	if sent := llm.requests[0].Text; strings.Contains(sent, "前回までにこの囲みから作ったもの") {
		t.Errorf("前回ぶんが無いのに節が出ている:\n%s", sent)
	}
}

// 制約を運ぶのは system 指示だけ。user 側のメッセージは、その場の材料（前回ぶんの
// 一覧や粒度）に添える判断しか持たない（ADR 0029）。この経路が切れると、LLM は
// 制約を知らないまま出力して検査にだけ弾かれ、直しようのない再送を上限まで繰り返す。
func TestInterpret_CarriesConstraintsInSystemPrompt(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}

	// 文言そのものは domain.Rules が持つ。ここに書き写すと 3 箇所目になる。
	want := domain.InterpretationConstraints()
	if sys := llm.requests[0].System; !strings.Contains(sys, want) {
		t.Errorf("システム指示に制約一覧が入っていない:\n%s", sys)
	}
}

// captureLogs は実績ログを受け取るバッファとロガーを返す。
func captureLogs() (*bytes.Buffer, *slog.Logger) {
	var buf bytes.Buffer
	return &buf, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// newLoggingInterpretService は実績ログを捕まえられる解釈サービスを返す。
func newLoggingInterpretService(
	t *testing.T, llm *fakeLLM, maxAttempts int,
) (*usecase.InterpretationService, *bytes.Buffer) {
	t.Helper()

	buf, logger := captureLogs()
	svc := usecase.NewInterpretationService(
		&fakeBoards{board: newBoard(interpretScene)}, &fakeMappings{}, llm, newLimiter(),
		usecase.WithMaxAttempts(maxAttempts), usecase.WithLogger(logger),
	)
	return svc, buf
}

// 解釈は課金を伴う外部呼び出しなのに、成功したぶんは took 以外どこにも
// 残っていなかった（ADR 0031）。#45 で決める上限の根拠になる。
func TestInterpret_LogsUsage(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{
		responses: []string{validLLMOutput},
		usages:    []port.Usage{{InputTokens: 1500, OutputTokens: 200}},
	}
	svc, buf := newLoggingInterpretService(t, llm, 3)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"attempts=1", "inputTokens=1500", "outputTokens=200", "ok=true",
		"boardId=board-1", "annotationId=annot-1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("実績ログに %s が無い: %q", want, out)
		}
	}
}

// スキーマ違反による再送はそのぶん課金される。1 回ぶんだけを見ても払った量に
// ならないので、解釈 1 回ぶんに積んでから残す。
func TestInterpret_LogsUsageAcrossRetries(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{
		responses: []string{invalidLLMOutput, validLLMOutput},
		usages: []port.Usage{
			{InputTokens: 1000, OutputTokens: 100},
			// 再送は修正指示を足すぶん入力が増える。ここが見えないと
			// interpretationSystemPrompt を直す判断ができない。
			{InputTokens: 1200, OutputTokens: 150},
		},
	}
	svc, buf := newLoggingInterpretService(t, llm, 3)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}

	out := buf.String()
	for _, want := range []string{"attempts=2", "inputTokens=2200", "outputTokens=250", "ok=true"} {
		if !strings.Contains(out, want) {
			t.Errorf("実績ログに %s が無い: %q", want, out)
		}
	}
}

// 直らなかったぶんも課金されている。成功したときだけ残すと、実績が実際より
// 小さく見える。
func TestInterpret_LogsUsageWhenSchemaNeverSatisfied(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{
		responses: []string{invalidLLMOutput},
		usages:    []port.Usage{{InputTokens: 900, OutputTokens: 80}},
	}
	svc, buf := newLoggingInterpretService(t, llm, 2)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err == nil {
		t.Fatal("interpret() = nil, want error")
	}

	out := buf.String()
	for _, want := range []string{"attempts=2", "inputTokens=1800", "outputTokens=160", "ok=false"} {
		if !strings.Contains(out, want) {
			t.Errorf("実績ログに %s が無い: %q", want, out)
		}
	}
}

// 呼び出しそのものが落ちた回も「呼んだ」に含める。含めないと、上限に対して
// 実際に何回叩いているのかが読めない。
func TestInterpret_LogsAttemptWhenCallFails(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}, err: errors.New("connection refused")}
	svc, buf := newLoggingInterpretService(t, llm, 3)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err == nil {
		t.Fatal("interpret() = nil, want error")
	}

	out := buf.String()
	// 接続が落ちた回は再送しないので 1 回で終わる。トークンは報告されない。
	for _, want := range []string{"attempts=1", "inputTokens=0", "outputTokens=0", "ok=false"} {
		if !strings.Contains(out, want) {
			t.Errorf("実績ログに %s が無い: %q", want, out)
		}
	}
}

// LLM を呼ぶ前に弾いた入力では、払っていないので実績も出ない（ADR 0018）。
func TestInterpret_DoesNotLogUsageWhenRejectedBeforeCalling(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, buf := newLoggingInterpretService(t, llm, 3)

	images := []port.Image{{MediaType: "image/gif", Data: []byte{1}}}
	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", images); err == nil {
		t.Fatal("interpret() = nil, want error")
	}

	if out := buf.String(); strings.Contains(out, "interpretation llm usage") {
		t.Errorf("呼んでいないのに実績ログが出ている: %q", out)
	}
}
