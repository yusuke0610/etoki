package usecase_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// newDiagramJobs は図のドラフトのジョブを作る。ボードの持ち主は操作者 "u1"。
func newDiagramJobs(
	t *testing.T, llm port.LLMClient, opts ...usecase.JobsOption,
) (*usecase.DiagramJobs, *fakeBoards) {
	t.Helper()

	boards := &fakeBoards{board: newBoard(interpretScene), owner: "u1"}
	svc := usecase.NewDiagramService(boards, llm, newLimiter(), usecase.WithDiagramMaxAttempts(1))
	return usecase.NewDiagramJobs(svc, opts...), boards
}

// waitDiagramJob はジョブが走り終わるのを待って返す。
func waitDiagramJob(
	t *testing.T, jobs *usecase.DiagramJobs, ctx context.Context, id string,
) usecase.DiagramJob {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := jobs.Get(ctx, "board-1", id)
		if err != nil {
			t.Fatalf("Get() = %v", err)
		}
		if job.Status != usecase.JobRunning {
			return job
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("ジョブが終わらない")
	return usecase.DiagramJob{}
}

// chatReq は 2 往復目の入力。往復と、内部の投げ直しの印まで持つ。
func chatReq() usecase.DiagramRequest {
	return usecase.DiagramRequest{
		Kind:   domain.DiagramKindTodo,
		Prompt: "返金の分岐も足して",
		History: []usecase.DiagramTurn{
			{Prompt: "注文から出荷まで", Mermaid: validMermaid},
			{Prompt: "直前の図は変換できませんでした", Mermaid: validMermaid, Internal: true},
		},
	}
}

// 開き直した画面は、どの指示への答えかを会話ごと戻す（#329）。入力をそのまま
// 返さないと、結果だけが出て何への答えか分からない。**内部の印も返す。** 落とすと、
// 変換器の英語を含む投げ直しが利用者の指示として並ぶ。
func TestDiagramJobs_StartEchoesRequestAndReturnsWithoutWaiting(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validMermaid)
	jobs, _ := newDiagramJobs(t, llm)
	ctx := asUser(t.Context(), "u1")
	in := chatReq()

	job, err := jobs.Start(ctx, "board-1", in)
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if job.ID == "" || job.Status != usecase.JobRunning {
		t.Fatalf("job = %+v, want running with ID", job)
	}
	if !reflect.DeepEqual(job.Input, in) {
		t.Errorf("Input = %+v, want %+v", job.Input, in)
	}

	close(llm.release)
	done := waitDiagramJob(t, jobs, ctx, job.ID)
	if done.Status != usecase.JobSucceeded || done.Result == nil {
		t.Fatalf("done = %+v, want succeeded with result", done)
	}
	if done.Result.Mermaid != validMermaid {
		t.Errorf("Mermaid = %q, want %q", done.Result.Mermaid, validMermaid)
	}
	// 2 往復の後の 3 往復目。数えるのはサーバー。
	if want := usecase.MaxDiagramTurns - 3; done.Result.TurnsRemaining != want {
		t.Errorf("TurnsRemaining = %d, want %d", done.Result.TurnsRemaining, want)
	}
	if !reflect.DeepEqual(done.Input, in) {
		t.Errorf("終わったあとの Input = %+v, want %+v", done.Input, in)
	}
}

// ボード一覧へ戻る（リクエストの ctx が終わる）ことで、生成が止まってはならない。
// #329 の本題。止まると、課金した生成を画面を移っただけで捨てる。
func TestDiagramJobs_KeepsRunningAfterRequestContextEnds(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validMermaid)
	jobs, _ := newDiagramJobs(t, llm)

	reqCtx, cancelReq := context.WithCancel(asUser(t.Context(), "u1"))
	job, err := jobs.Start(reqCtx, "board-1", req("段取り"))
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	<-llm.started
	cancelReq()

	select {
	case <-llm.ctxEnded:
		t.Fatal("リクエストの ctx が終わった時点で LLM の呼び出しが止まった")
	case <-time.After(50 * time.Millisecond):
	}

	close(llm.release)
	done := waitDiagramJob(t, jobs, asUser(t.Context(), "u1"), job.ID)
	if done.Status != usecase.JobSucceeded {
		t.Errorf("Status = %q, want succeeded", done.Status)
	}
}

func TestDiagramJobs_SameRequestWhileRunningReturnsSameJob(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validMermaid)
	jobs, _ := newDiagramJobs(t, llm)
	ctx := asUser(t.Context(), "u1")

	first, err := jobs.Start(ctx, "board-1", chatReq())
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	second, err := jobs.Start(ctx, "board-1", chatReq())
	if err != nil {
		t.Fatalf("2 回目の Start() = %v, want 同じジョブ", err)
	}
	if second.ID != first.ID {
		t.Errorf("2 回目の ID = %q, want %q", second.ID, first.ID)
	}

	close(llm.release)
	waitDiagramJob(t, jobs, ctx, first.ID)
	if n := llm.callCount(); n != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1（二度課金しない）", n)
	}
}

// 同じものを返すのは入力が丸ごと同じときだけ。**どこか 1 つでも違えば別の入力**で、
// 走っているジョブを返すと、送った指示と違う答えが返る。枠は 1 つなので 429。
func TestDiagramJobs_DifferentRequestHitsConcurrencyLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*usecase.DiagramRequest)
	}{
		{"prompt", func(r *usecase.DiagramRequest) { r.Prompt = "別の指示" }},
		{"kind", func(r *usecase.DiagramRequest) { r.Kind = domain.DiagramKindSequence }},
		{"history prompt", func(r *usecase.DiagramRequest) { r.History[0].Prompt = "別の始まり" }},
		{"history mermaid", func(r *usecase.DiagramRequest) { r.History[0].Mermaid = "flowchart LR\n  A --> B" }},
		{"history length", func(r *usecase.DiagramRequest) { r.History = r.History[:1] }},
		{"internal", func(r *usecase.DiagramRequest) { r.Internal = true }},
		{"history internal", func(r *usecase.DiagramRequest) { r.History[1].Internal = false }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			llm := newGatedLLM(validMermaid)
			jobs, _ := newDiagramJobs(t, llm)
			ctx := asUser(t.Context(), "u1")

			if _, err := jobs.Start(ctx, "board-1", chatReq()); err != nil {
				t.Fatalf("Start() = %v", err)
			}
			other := chatReq()
			tt.change(&other)
			if _, err := jobs.Start(ctx, "board-1", other); !errors.Is(err, usecase.ErrConcurrencyLimited) {
				t.Errorf("違う入力の Start() = %v, want ErrConcurrencyLimited", err)
			}
			close(llm.release)
		})
	}
}

// 解釈と図のドラフトは 1 つの枠を共有する（ADR 0044）。ジョブにしても、片方が
// 走っているあいだにもう片方を素通しにしない。
func TestDiagramJobs_ShareLimiterWithInterpretation(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validMermaid)
	boards := &fakeBoards{board: newBoard(interpretScene), owner: "u1"}
	limits := newLimiter()
	diagrams := usecase.NewDiagramJobs(usecase.NewDiagramService(boards, llm, limits))
	interpretations := usecase.NewInterpretationJobs(usecase.NewInterpretationService(
		boards, &fakeMappings{}, llm, limits))
	ctx := asUser(t.Context(), "u1")

	if _, err := diagrams.Start(ctx, "board-1", req("段取り")); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if _, err := interpretations.Start(ctx, "board-1", "annot-1", nil); !errors.Is(err, usecase.ErrConcurrencyLimited) {
		t.Errorf("図の生成中の解釈 = %v, want ErrConcurrencyLimited", err)
	}
	close(llm.release)
}

// 入力だけで決まる失敗は始める時点で返し、ジョブにしない。LLM も呼ばない。
func TestDiagramJobs_RejectsBeforeCreatingJob(t *testing.T) {
	t.Parallel()

	tooMany := req("もう 1 回")
	for range usecase.MaxDiagramTurns {
		tooMany.History = append(tooMany.History, usecase.DiagramTurn{Prompt: "p", Mermaid: validMermaid})
	}

	tests := []struct {
		name    string
		boardID string
		in      usecase.DiagramRequest
		want    error
	}{
		{"no board", "nope", req("段取り"), usecase.ErrBoardNotFound},
		{"no kind", "board-1", usecase.DiagramRequest{Prompt: "段取り"}, usecase.ErrInvalidInput},
		{"blank prompt", "board-1", req(" \n "), usecase.ErrInvalidInput},
		{"too many turns", "board-1", tooMany, usecase.ErrDiagramChatTooLong},
		{"too long", "board-1", req(strings.Repeat("あ", usecase.MaxDiagramChatBytes)), usecase.ErrDiagramChatTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			llm := &fakeLLM{responses: []string{validMermaid}}
			jobs, _ := newDiagramJobs(t, llm)
			ctx := asUser(t.Context(), "u1")

			if _, err := jobs.Start(ctx, tt.boardID, tt.in); !errors.Is(err, tt.want) {
				t.Errorf("Start() = %v, want %v", err, tt.want)
			}
			if len(llm.requests) != 0 {
				t.Error("弾く入力で LLM を呼んだ")
			}
			list, err := jobs.List(ctx, "board-1")
			if err != nil || len(list) != 0 {
				t.Errorf("List() = %v, %v, want 空", list, err)
			}
			// 弾いた入力は枠を食わない。
			if _, err := jobs.Start(ctx, "board-1", req("段取り")); err != nil {
				t.Errorf("弾いた後の Start() = %v, want 始められる", err)
			}
		})
	}
}

// 生成は LLM を叩く外部呼び出しで課金も伴う。viewer は始めることも、覗くことも
// できない（ADR 0017）。
func TestDiagramJobs_ViewerCannotUse(t *testing.T) {
	t.Parallel()

	jobs, boards := newDiagramJobs(t, &fakeLLM{responses: []string{validMermaid}})
	ctx := asUser(t.Context(), "u1")
	job, err := jobs.Start(ctx, "board-1", req("段取り"))
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	boards.role = port.RoleViewer

	if _, err := jobs.Start(ctx, "board-1", req("別の段取り")); !errors.Is(err, usecase.ErrForbidden) {
		t.Errorf("Start() = %v, want ErrForbidden", err)
	}
	if _, err := jobs.List(ctx, "board-1"); !errors.Is(err, usecase.ErrForbidden) {
		t.Errorf("List() = %v, want ErrForbidden", err)
	}
	if _, err := jobs.Get(ctx, "board-1", job.ID); !errors.Is(err, usecase.ErrForbidden) {
		t.Errorf("Get() = %v, want ErrForbidden", err)
	}
	if err := jobs.Discard(ctx, "board-1", job.ID); !errors.Is(err, usecase.ErrForbidden) {
		t.Errorf("Discard() = %v, want ErrForbidden", err)
	}
}

func TestDiagramJobs_HidesJobsFromOthers(t *testing.T) {
	t.Parallel()

	jobs, boards := newDiagramJobs(t, &fakeLLM{responses: []string{validMermaid}})
	job, err := jobs.Start(asUser(t.Context(), "u1"), "board-1", req("段取り"))
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}

	// ボードを開ける別の利用者にも、存在を見せない（fakeBoards は owner だけを
	// 通すので、開ける側を u2 に移す）。
	boards.owner = "u2"
	other := asUser(t.Context(), "u2")
	if _, err := jobs.Get(other, "board-1", job.ID); !errors.Is(err, usecase.ErrJobNotFound) {
		t.Errorf("他人の Get() = %v, want ErrJobNotFound", err)
	}
	if list, err := jobs.List(other, "board-1"); err != nil || len(list) != 0 {
		t.Errorf("他人の List() = %v, %v, want 空", list, err)
	}
	if err := jobs.Discard(other, "board-1", job.ID); !errors.Is(err, usecase.ErrJobNotFound) {
		t.Errorf("他人の Discard() = %v, want ErrJobNotFound", err)
	}
}

// 一覧は入力を載せ、結果は載せない。開き直した画面は一覧から会話を戻し、
// 結果は 1 件ずつの取得で取る。
func TestDiagramJobs_ListCarriesRequestButNotResult(t *testing.T) {
	t.Parallel()

	jobs, _ := newDiagramJobs(t, &fakeLLM{responses: []string{validMermaid}})
	ctx := asUser(t.Context(), "u1")

	job, _ := jobs.Start(ctx, "board-1", chatReq())
	waitDiagramJob(t, jobs, ctx, job.ID)

	list, err := jobs.List(ctx, "board-1")
	if err != nil || len(list) != 1 {
		t.Fatalf("List() = %+v, %v, want 1 件", list, err)
	}
	if list[0].Status != usecase.JobSucceeded || list[0].Result != nil {
		t.Errorf("List()[0] = %+v, want 成功・結果なし", list[0])
	}
	if !reflect.DeepEqual(list[0].Input, chatReq()) {
		t.Errorf("Input = %+v, want %+v", list[0].Input, chatReq())
	}
}

func TestDiagramJobs_FailureKeepsReasonAndLogs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		llm     port.LLMClient
		want    error
		wantLog string
	}{
		{"transport", &fakeLLM{err: errors.New("connection refused")},
			usecase.ErrLLMUnavailable, `level=ERROR msg="llm call failed"`},
		{"truncated", &fakeLLM{err: port.ErrLLMTruncated},
			usecase.ErrLLMTruncated, `level=ERROR msg="llm call failed"`},
		// 図が返らなかったことは、図の側の sentinel で記録する。解釈の sentinel を
		// 渡し違えると、出力を弾いた失敗が記録から消える。
		{"no diagram", &fakeLLM{responses: []string{"説明だけです"}},
			usecase.ErrDiagramFailed, `level=WARN msg="llm output rejected"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buf, logger := captureLogs()
			svc := usecase.NewDiagramService(
				&fakeBoards{board: newBoard(interpretScene), owner: "u1"}, tt.llm, newLimiter(),
				usecase.WithDiagramMaxAttempts(1), usecase.WithDiagramLogger(logger))
			jobs := usecase.NewDiagramJobs(svc)
			ctx := asUser(t.Context(), "u1")

			job, err := jobs.Start(ctx, "board-1", req("段取り"))
			if err != nil {
				t.Fatalf("Start() = %v", err)
			}
			done := waitDiagramJob(t, jobs, ctx, job.ID)
			if done.Status != usecase.JobFailed || !errors.Is(done.Err, tt.want) {
				t.Errorf("done = %+v, want failed with %v", done, tt.want)
			}
			if done.Result != nil {
				t.Error("失敗したジョブに Result が入っている")
			}
			if got := buf.String(); !strings.Contains(got, tt.wantLog) {
				t.Errorf("log = %q, want %q", got, tt.wantLog)
			}
		})
	}
}

// 種類を変えると画面は会話ごと捨てる。走っている生成も止めて枠を戻す。
func TestDiagramJobs_DiscardRunningCancelsAndFreesSlot(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validMermaid)
	jobs, _ := newDiagramJobs(t, llm)
	ctx := asUser(t.Context(), "u1")

	job, _ := jobs.Start(ctx, "board-1", req("段取り"))
	<-llm.started

	if err := jobs.Discard(ctx, "board-1", job.ID); err != nil {
		t.Fatalf("Discard() = %v", err)
	}
	select {
	case <-llm.ctxEnded:
	case <-time.After(5 * time.Second):
		t.Fatal("取り消しても LLM の呼び出しの ctx が終わらない")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := jobs.Start(ctx, "board-1", req("別の段取り"))
		if err == nil {
			break
		}
		if !errors.Is(err, usecase.ErrConcurrencyLimited) || time.Now().After(deadline) {
			t.Fatalf("取り消した後の Start() = %v, want 枠が戻る", err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	close(llm.release)
}
