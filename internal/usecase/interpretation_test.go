package usecase_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// fakeBoards は解釈に必要な Find だけを持つボードリポジトリ。
//
// 書き込み系が呼ばれたら数える。解釈は読むだけで何も残さない、という約束を
// テストから確かめるため。
type fakeBoards struct {
	board *port.Board
	// owner はこのボードのメンバーである操作者。既定は空文字（認証なし）。
	owner string
	// role は操作者のロール。空なら owner として扱う。ロール不足を確かめる
	// テストだけがここを変える。
	role    port.BoardRole
	members []port.BoardMember
	writes  int
	// display は最後に書かれた表示用スナップショット。
	display port.BoardTargetDisplay
}

// Find は操作者も突き合わせる。実装と同じ形にしておかないと、絞り忘れを
// フェイクが吸収してしまう（ADR 0016 / 0017）。
func (f *fakeBoards) Find(_ context.Context, actor, id string) (*port.BoardAccess, error) {
	if f.board == nil || f.board.ID != id || f.owner != actor {
		return nil, nil
	}

	role := f.role
	if role == "" {
		role = port.RoleOwner
	}
	return &port.BoardAccess{Board: *f.board, Role: role}, nil
}

func (f *fakeBoards) Create(context.Context, port.Board, string) error {
	f.writes++
	return nil
}

// UpdateScene は版の照合まで真似る。素通しにすると、ユースケースが基準を
// 渡し忘れても緑のままになる（ADR 0020）。
func (f *fakeBoards) UpdateScene(
	_ context.Context, _, _, _ string, base, updatedAt time.Time,
) error {
	if f.board != nil && !base.Equal(f.board.UpdatedAt) {
		return port.ErrConflict
	}
	f.writes++
	if f.board != nil {
		f.board.UpdatedAt = updatedAt
	}
	return nil
}

func (f *fakeBoards) UpdateTarget(
	context.Context, string, string, port.BoardTarget, time.Time,
) error {
	f.writes++
	return nil
}

// UpdateTargetDisplay は表示用の 3 つだけを書く。作成先には触らない
// （ADR 0037）。実装と同じ形にしておかないと、触ったことをフェイクが吸収する。
func (f *fakeBoards) UpdateTargetDisplay(
	_ context.Context, _, _ string, d port.BoardTargetDisplay, updatedAt time.Time,
) error {
	f.writes++
	f.display = d
	if f.board != nil {
		f.board.Target.ProjectNumber = d.ProjectNumber
		f.board.Target.ProjectTitle = d.ProjectTitle
		f.board.Target.ProjectURL = d.ProjectURL
		f.board.UpdatedAt = updatedAt
	}
	return nil
}

func (f *fakeBoards) List(context.Context, string) ([]port.BoardAccess, error) { return nil, nil }

func (f *fakeBoards) ListMembers(_ context.Context, boardID string) ([]port.BoardMember, error) {
	if f.board == nil || f.board.ID != boardID {
		return nil, nil
	}
	return f.members, nil
}

func (f *fakeBoards) AddMember(_ context.Context, m port.BoardMember) error {
	for _, existing := range f.members {
		if existing.UserID == m.UserID {
			return port.ErrAlreadyExists
		}
	}
	f.writes++
	f.members = append(f.members, m)
	return nil
}

func (f *fakeBoards) UpdateMemberRole(
	_ context.Context, _, userID string, role port.BoardRole,
) error {
	for i, m := range f.members {
		if m.UserID == userID {
			f.writes++
			f.members[i].Role = role
			return nil
		}
	}
	return port.ErrNotFound
}

func (f *fakeBoards) RemoveMember(_ context.Context, _, userID string) error {
	for i, m := range f.members {
		if m.UserID == userID {
			f.writes++
			f.members = append(f.members[:i], f.members[i+1:]...)
			return nil
		}
	}
	return port.ErrNotFound
}

func (f *fakeBoards) CountUnowned(context.Context) (int, error) { return 0, nil }

func (f *fakeBoards) ClaimUnowned(context.Context, string) (int64, error) {
	f.writes++
	return 0, nil
}

// fakeLLM は決められた応答を順に返す LLMClient。
//
// 応答を使い切ったら最後のものを返し続ける。上限まで失敗し続ける場合を
// 書きやすくするため。
type fakeLLM struct {
	responses []string
	// usages は応答ごとに返すトークン数。空なら報告しない実装を真似る
	// （port.Usage は埋めるかどうかが任意）。
	usages   []port.Usage
	err      error
	requests []port.VisionRequest
}

func (f *fakeLLM) Complete(_ context.Context, req port.VisionRequest) (port.VisionResponse, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return port.VisionResponse{}, f.err
	}

	i := min(len(f.requests)-1, len(f.responses)-1)
	resp := port.VisionResponse{Text: f.responses[i]}
	if len(f.usages) > 0 {
		resp.Usage = f.usages[min(len(f.requests)-1, len(f.usages)-1)]
	}
	return resp, nil
}

const interpretScene = `{"type":"excalidraw","elements":[
	{"id":"annot-1","type":"frame","name":"決済まわり","customData":{"etoki":{"granularity":""}}},
	{"id":"t1","type":"text","text":"Stripe の SDK が古い","frameId":"annot-1"},
	{"id":"t2","type":"text","text":"返金導線が無い","frameId":"annot-1"},
	{"id":"t9","type":"text","text":"囲みの外のメモ"}]}`

const validLLMOutput = `{"summary":"決済まわりの課題出し。SDK 更新と返金導線の 2 件。","items":[
	{"localId":"e1","kind":"epic","title":"決済まわりの整備","body":"","parentLocalId":null},
	{"localId":"i1","kind":"issue","title":"Stripe SDK の更新","body":"","parentLocalId":"e1"}]}`

// title が空なので items[0].title で弾かれる。
const invalidLLMOutput = `{"summary":"決済まわりの課題出し","items":[
	{"localId":"e1","kind":"epic","title":"","body":"","parentLocalId":null}]}`

// newBoard は作成先を設定済みのボードを返す。
//
// 未選択のボードには draft issue を作れない（ADR 0014）。作成先の有無そのものを
// 見るテストだけが、ここから Target を落として使う。
func newBoard(scene string) *port.Board {
	return &port.Board{
		ID:    "board-1",
		Name:  "設計会",
		Scene: scene,
		// 保存は基準にした版を伴う（ADR 0020）。ゼロ値のままにすると、保存を
		// 通したいテストが「基準が無い」で 400 に落ちる。
		UpdatedAt: baseTime,
		Target:    port.BoardTarget{RepositoryOwner: "acme", RepositoryName: "web", ProjectID: "PVT_1"},
	}
}

func newInterpretService(t *testing.T, board *port.Board, llm *fakeLLM) (*usecase.InterpretationService, *fakeBoards) {
	t.Helper()

	boards := &fakeBoards{board: board}
	svc := usecase.NewInterpretationService(boards, &fakeMappings{}, llm, usecase.WithMaxAttempts(3))
	return svc, boards
}

func TestInterpret_Succeeds(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, boards := newInterpretService(t, newBoard(interpretScene), llm)

	result, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("Interpret() = %v", err)
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

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("Interpret() = %v", err)
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
	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1", []port.Image{img}); err != nil {
		t.Fatalf("Interpret() = %v", err)
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

	if _, err := svc.Interpret(
		t.Context(), "board-1", "annot-1", []port.Image{pngImage(16)}); err != nil {
		t.Fatalf("Interpret() = %v", err)
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

			_, err := svc.Interpret(t.Context(), "board-1", "annot-1", images)
			if !errors.Is(err, usecase.ErrInvalidInput) {
				t.Fatalf("Interpret() = %v, want ErrInvalidInput", err)
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

			_, _ = svc.Interpret(t.Context(), "board-1", "annot-1", nil)

			if len(llm.requests) == 0 {
				t.Fatal("LLM が呼ばれていない")
			}
			if !strings.Contains(llm.requests[0].Text, tt.want) {
				t.Errorf("プロンプトに %q が無い:\n%s", tt.want, llm.requests[0].Text)
			}
		})
	}
}

// 再送は「何が悪かったか」を伝えて初めて意味を持つ。
func TestInterpret_RetriesWithCorrections(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{invalidLLMOutput, validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	result, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("Interpret() = %v", err)
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
	svc := usecase.NewInterpretationService(boards, &fakeMappings{}, llm, usecase.WithMaxAttempts(2))

	_, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil)
	if !errors.Is(err, usecase.ErrInterpretationFailed) {
		t.Fatalf("Interpret() = %v, want ErrInterpretationFailed", err)
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

			result, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil)
			if err != nil {
				t.Fatalf("Interpret() = %v", err)
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

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("Interpret() = %v", err)
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

	_, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Interpret() = %v, want %v", err, wantErr)
	}
	// スキーマ違反とは原因も打ち手も違うので、呼び出し側が区別できる必要がある。
	if !errors.Is(err, usecase.ErrLLMUnavailable) {
		t.Errorf("Interpret() = %v, want ErrLLMUnavailable", err)
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

		_, err := svc.Interpret(t.Context(), "no-such-board", "annot-1", nil)
		if !errors.Is(err, usecase.ErrBoardNotFound) {
			t.Fatalf("Interpret() = %v, want ErrBoardNotFound", err)
		}
		if len(llm.requests) != 0 {
			t.Error("ボードが無いのに LLM を呼んでいる")
		}
	})

	t.Run("注釈が無い", func(t *testing.T) {
		t.Parallel()

		llm := &fakeLLM{responses: []string{validLLMOutput}}
		svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

		_, err := svc.Interpret(t.Context(), "board-1", "no-such-annot", nil)
		if !errors.Is(err, usecase.ErrAnnotationNotFound) {
			t.Fatalf("Interpret() = %v, want ErrAnnotationNotFound", err)
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

		_, err := svc.Interpret(t.Context(), "board-1", "f1", nil)
		if !errors.Is(err, usecase.ErrAnnotationNotFound) {
			t.Fatalf("Interpret() = %v, want ErrAnnotationNotFound", err)
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

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("Interpret() = %v", err)
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
	svc := usecase.NewInterpretationService(boards, &fakeMappings{}, llm, usecase.WithMaxAttempts(0))

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("Interpret() = %v", err)
	}
	if len(llm.requests) != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
	}
}

// 既定に倒して進めると、開発者が指定したつもりの制約が黙って外れる。
func TestInterpret_RejectsUnknownGranularity(t *testing.T) {
	t.Parallel()

	scene := strings.Replace(interpretScene, `"granularity":""`, `"granularity":"project"`, 1)
	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(scene), llm)

	_, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil)
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("Interpret() = %v, want ErrInvalidInput", err)
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

	result, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("Interpret() = %v", err)
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
				Action: port.ActionCreated, CreatedAt: baseTime,
			},
			{
				ItemID: "PVTI_second", LocalID: "i1", Kind: port.KindIssue,
				Title: "カード決済", Action: port.ActionCreated, CreatedAt: baseTime,
			},
		},
	}); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc := usecase.NewInterpretationService(
		&fakeBoards{board: newBoard(interpretScene)}, mappings, llm, usecase.WithMaxAttempts(1))

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("Interpret() = %v", err)
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

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("Interpret() = %v", err)
	}

	if sent := llm.requests[0].Text; strings.Contains(sent, "前回までにこの囲みから作ったもの") {
		t.Errorf("前回ぶんが無いのに節が出ている:\n%s", sent)
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
		&fakeBoards{board: newBoard(interpretScene)}, &fakeMappings{}, llm,
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

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("Interpret() = %v", err)
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

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("Interpret() = %v", err)
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

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil); err == nil {
		t.Fatal("Interpret() = nil, want error")
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

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil); err == nil {
		t.Fatal("Interpret() = nil, want error")
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
	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1", images); err == nil {
		t.Fatal("Interpret() = nil, want error")
	}

	if out := buf.String(); strings.Contains(out, "interpretation llm usage") {
		t.Errorf("呼んでいないのに実績ログが出ている: %q", out)
	}
}
