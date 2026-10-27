package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// gatedLLM は release が閉じるか ctx が終わるまで応答を返さない LLMClient。
//
// 「解釈が走っている最中」を作るために使う。fakeLLM は即座に返すので、走っている
// 間の振る舞いを確かめられない。
type gatedLLM struct {
	text    string
	err     error
	release chan struct{}

	mu       sync.Mutex
	calls    int
	started  chan struct{}
	ctxEnded chan struct{}
}

func newGatedLLM(text string) *gatedLLM {
	return &gatedLLM{
		text:     text,
		release:  make(chan struct{}),
		started:  make(chan struct{}, 8),
		ctxEnded: make(chan struct{}, 8),
	}
}

func (g *gatedLLM) Complete(ctx context.Context, _ port.VisionRequest) (port.VisionResponse, error) {
	g.mu.Lock()
	g.calls++
	g.mu.Unlock()
	g.started <- struct{}{}

	select {
	case <-g.release:
	case <-ctx.Done():
		g.ctxEnded <- struct{}{}
		return port.VisionResponse{}, ctx.Err()
	}
	if g.err != nil {
		return port.VisionResponse{}, g.err
	}
	return port.VisionResponse{Text: g.text}, nil
}

func (g *gatedLLM) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

// newJobs はジョブを作る。ボードの持ち主は操作者 "u1"。
func newJobs(
	t *testing.T, llm port.LLMClient, opts ...usecase.JobsOption,
) (*usecase.InterpretationJobs, *fakeBoards) {
	t.Helper()

	boards := &fakeBoards{board: newBoard(interpretScene), owner: "u1"}
	svc := usecase.NewInterpretationService(
		boards, &fakeMappings{}, llm, newLimiter(), usecase.WithMaxAttempts(1))
	return usecase.NewInterpretationJobs(svc, opts...), boards
}

func asUser(ctx context.Context, id string) context.Context {
	return port.ContextWithUserID(ctx, id)
}

// waitDone はジョブが走り終わるのを待って返す。get は Get を 1 回呼ぶ。
//
// **Get を通して待つ。** 結果はジョブの goroutine が書くので、鍵を握って読む Get を
// 挟まないと、フェイクに積まれた呼び出しやログを読むテストが競合する。
func waitDone[In, R any](
	t *testing.T, get func() (usecase.Job[In, R], error),
) usecase.Job[In, R] {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := get()
		if err != nil {
			t.Fatalf("Get() = %v", err)
		}
		if job.Status != usecase.JobRunning {
			return job
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("ジョブが終わらない")
	return usecase.Job[In, R]{}
}

// waitJob はジョブが走り終わるのを待って返す。
func waitJob(
	t *testing.T, jobs *usecase.InterpretationJobs, ctx context.Context, id string,
) usecase.InterpretationJob {
	t.Helper()

	return waitDone(t, func() (usecase.InterpretationJob, error) {
		return jobs.Get(ctx, "board-1", id)
	})
}

// interpret は解釈を本番と同じ経路（InterpretationJobs）で走らせ、終わるまで待つ。
//
// 始める時点で弾かれたものはそのエラーを、走って失敗したものはジョブの Err を返す。
// 解釈そのものの振る舞い（出力の検証、再送、プロンプト）を確かめるテストが使う。
// **リクエストの中で待つ口は本番に無い**（ADR 0085）ので、テストにも持たない。
func interpret(
	t *testing.T, svc *usecase.InterpretationService, ctx context.Context,
	boardID, annotationID string, images []port.Image,
) (usecase.InterpretationResult, error) {
	t.Helper()

	jobs := usecase.NewInterpretationJobs(svc)
	job, err := jobs.Start(ctx, boardID, annotationID, images)
	if err != nil {
		return usecase.InterpretationResult{}, err
	}

	job = waitDone(t, func() (usecase.InterpretationJob, error) {
		return jobs.Get(ctx, boardID, job.ID)
	})
	if job.Status == usecase.JobFailed {
		return usecase.InterpretationResult{}, job.Err
	}
	return *job.Result, nil
}

func TestJobs_StartReturnsWithoutWaitingForLLM(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validLLMOutput)
	jobs, _ := newJobs(t, llm)
	ctx := asUser(t.Context(), "u1")

	job, err := jobs.Start(ctx, "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if job.ID == "" || job.Status != usecase.JobRunning {
		t.Fatalf("job = %+v, want running with ID", job)
	}

	close(llm.release)
	done := waitJob(t, jobs, ctx, job.ID)
	if done.Status != usecase.JobSucceeded || done.Result == nil {
		t.Fatalf("done = %+v, want succeeded with result", done)
	}
	if len(done.Result.Interpretation.Items) != 2 {
		t.Errorf("len(Items) = %d, want 2", len(done.Result.Interpretation.Items))
	}
	if done.Result.ContentHash != currentContentHash(t) {
		t.Errorf("ContentHash = %q, want %q", done.Result.ContentHash, currentContentHash(t))
	}
}

// 画面を離れる（リクエストの ctx が終わる）ことで、解釈が止まってはならない。#307 の本題。
func TestJobs_KeepsRunningAfterRequestContextEnds(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validLLMOutput)
	jobs, _ := newJobs(t, llm)

	reqCtx, cancelReq := context.WithCancel(asUser(t.Context(), "u1"))
	job, err := jobs.Start(reqCtx, "board-1", "annot-1", nil)
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
	done := waitJob(t, jobs, asUser(t.Context(), "u1"), job.ID)
	if done.Status != usecase.JobSucceeded {
		t.Errorf("Status = %q, want succeeded", done.Status)
	}
}

func TestJobs_StartAgainWhileRunningReturnsSameJob(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validLLMOutput)
	jobs, _ := newJobs(t, llm)
	ctx := asUser(t.Context(), "u1")

	first, err := jobs.Start(ctx, "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	second, err := jobs.Start(ctx, "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("2 回目の Start() = %v, want 同じジョブ", err)
	}
	if second.ID != first.ID {
		t.Errorf("2 回目の ID = %q, want %q", second.ID, first.ID)
	}

	close(llm.release)
	waitJob(t, jobs, ctx, first.ID)
	if n := llm.callCount(); n != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1（二度課金しない）", n)
	}
}

func TestJobs_StartAfterFinishedMakesNewJob(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	jobs, _ := newJobs(t, llm)
	ctx := asUser(t.Context(), "u1")

	first, err := jobs.Start(ctx, "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	waitJob(t, jobs, ctx, first.ID)

	second, err := jobs.Start(ctx, "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if second.ID == first.ID {
		t.Error("終わったジョブを実行中として返した")
	}
}

func TestJobs_OtherAnnotationHitsConcurrencyLimit(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validLLMOutput)
	jobs, boards := newJobs(t, llm)
	boards.board.Scene = `{"type":"excalidraw","elements":[
		{"id":"annot-1","type":"frame","customData":{"etoki":{"granularity":""}}},
		{"id":"annot-2","type":"frame","customData":{"etoki":{"granularity":""}}}]}`
	ctx := asUser(t.Context(), "u1")

	if _, err := jobs.Start(ctx, "board-1", "annot-1", nil); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	_, err := jobs.Start(ctx, "board-1", "annot-2", nil)
	if !errors.Is(err, usecase.ErrConcurrencyLimited) {
		t.Errorf("別の注釈の Start() = %v, want ErrConcurrencyLimited", err)
	}
	close(llm.release)
}

func TestJobs_RejectsBeforeCreatingJob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		boardID    string
		annotation string
		want       error
	}{
		{"no board", "nope", "annot-1", usecase.ErrBoardNotFound},
		{"no annotation", "board-1", "nope", usecase.ErrAnnotationNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// 回数の枠を 1 にする。無制限のままだと、弾いた Start が枠を食っても
			// 最後の Start が通ってしまう。
			llm := &fakeLLM{responses: []string{validLLMOutput}}
			jobs := usecase.NewInterpretationJobs(usecase.NewInterpretationService(
				&fakeBoards{board: newBoard(interpretScene), owner: "u1"}, &fakeMappings{}, llm,
				usecase.NewLLMLimiter(usecase.LLMLimits{RateLimit: 1}), usecase.WithMaxAttempts(1)))
			ctx := asUser(t.Context(), "u1")

			if _, err := jobs.Start(ctx, tt.boardID, tt.annotation, nil); !errors.Is(err, tt.want) {
				t.Errorf("Start() = %v, want %v", err, tt.want)
			}
			if len(llm.requests) != 0 {
				t.Error("弾く入力で LLM を呼んだ")
			}
			list, err := jobs.List(ctx, "board-1")
			if err != nil || len(list) != 0 {
				t.Errorf("List() = %v, %v, want 空", list, err)
			}
			// 弾いた入力は枠を食わない（TestDiagramJobs_RejectsBeforeCreatingJob と同じ）。
			if _, err := jobs.Start(ctx, "board-1", "annot-1", nil); err != nil {
				t.Errorf("弾いた後の Start() = %v, want 始められる", err)
			}
		})
	}
}

func TestJobs_ViewerCannotStart(t *testing.T) {
	t.Parallel()

	jobs, boards := newJobs(t, &fakeLLM{responses: []string{validLLMOutput}})
	boards.role = port.RoleViewer

	_, err := jobs.Start(asUser(t.Context(), "u1"), "board-1", "annot-1", nil)
	if !errors.Is(err, usecase.ErrForbidden) {
		t.Errorf("Start() = %v, want ErrForbidden", err)
	}
}

func TestJobs_FailureKeepsReasonKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		llm  port.LLMClient
		want error
	}{
		{"transport", &fakeLLM{err: errors.New("connection refused")}, usecase.ErrLLMUnavailable},
		{"schema", &fakeLLM{responses: []string{invalidLLMOutput}}, usecase.ErrInterpretationFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			jobs, _ := newJobs(t, tt.llm)
			ctx := asUser(t.Context(), "u1")

			job, err := jobs.Start(ctx, "board-1", "annot-1", nil)
			if err != nil {
				t.Fatalf("Start() = %v", err)
			}
			done := waitJob(t, jobs, ctx, job.ID)
			if done.Status != usecase.JobFailed || !errors.Is(done.Err, tt.want) {
				t.Errorf("done = %+v, want failed with %v", done, tt.want)
			}
			if done.Result != nil {
				t.Error("失敗したジョブに Result が入っている")
			}
		})
	}
}

// ジョブの失敗はリクエストに結びつかないので、HTTP 層の記録を通らない。
// 原因ごとに分けた sentinel（ADR 0087）も、ジョブの側で呼び出しの失敗として残す。
// 記録の側に足し忘れると、その原因だけ記録から消える。
func TestJobs_LogsEveryLLMCallFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{"cause unknown", errors.New("connection refused")},
		{"truncated", fmt.Errorf("stop: %w", port.ErrLLMTruncated)},
		{"refused", fmt.Errorf("stop: %w", port.ErrLLMRefused)},
		{"overloaded", fmt.Errorf("529: %w", port.ErrLLMOverloaded)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buf, logger := captureLogs()
			svc := usecase.NewInterpretationService(
				&fakeBoards{board: newBoard(interpretScene), owner: "u1"}, &fakeMappings{},
				&fakeLLM{err: tt.err}, newLimiter(),
				usecase.WithMaxAttempts(1), usecase.WithLogger(logger))
			jobs := usecase.NewInterpretationJobs(svc)
			ctx := asUser(t.Context(), "u1")

			job, err := jobs.Start(ctx, "board-1", "annot-1", nil)
			if err != nil {
				t.Fatalf("Start() = %v", err)
			}
			// 記録は結果を書くのと同じロックの中で残すので、終わったと読めた時点で
			// 書き終わっている。
			waitJob(t, jobs, ctx, job.ID)

			if got := buf.String(); !strings.Contains(got, `level=ERROR msg="llm call failed"`) {
				t.Errorf("log = %q, want llm call failed", got)
			}
		})
	}
}

func TestJobs_GetDoesNotConsume(t *testing.T) {
	t.Parallel()

	jobs, _ := newJobs(t, &fakeLLM{responses: []string{validLLMOutput}})
	ctx := asUser(t.Context(), "u1")

	job, _ := jobs.Start(ctx, "board-1", "annot-1", nil)
	waitJob(t, jobs, ctx, job.ID)

	for range 2 {
		got, err := jobs.Get(ctx, "board-1", job.ID)
		if err != nil || got.Result == nil {
			t.Fatalf("Get() = %+v, %v, 何度読んでも結果が取れる", got, err)
		}
	}
}

func TestJobs_ExpiresFinishedJobsAfterRetention(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	jobs, _ := newJobs(t, &fakeLLM{responses: []string{validLLMOutput}},
		usecase.WithJobClock(clock), usecase.WithJobRetention(time.Hour))
	ctx := asUser(t.Context(), "u1")

	job, _ := jobs.Start(ctx, "board-1", "annot-1", nil)
	waitJob(t, jobs, ctx, job.ID)

	mu.Lock()
	now = now.Add(59 * time.Minute)
	mu.Unlock()
	if _, err := jobs.Get(ctx, "board-1", job.ID); err != nil {
		t.Fatalf("59 分後の Get() = %v, want 取れる", err)
	}

	mu.Lock()
	now = now.Add(2 * time.Minute)
	mu.Unlock()
	if _, err := jobs.Get(ctx, "board-1", job.ID); !errors.Is(err, usecase.ErrJobNotFound) {
		t.Errorf("61 分後の Get() = %v, want ErrJobNotFound", err)
	}
}

func TestJobs_RunningJobIsNotExpired(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validLLMOutput)
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	jobs, _ := newJobs(t, llm,
		usecase.WithJobClock(func() time.Time { mu.Lock(); defer mu.Unlock(); return now }),
		usecase.WithJobRetention(time.Hour))
	ctx := asUser(t.Context(), "u1")

	job, _ := jobs.Start(ctx, "board-1", "annot-1", nil)
	mu.Lock()
	now = now.Add(3 * time.Hour)
	mu.Unlock()

	if _, err := jobs.Get(ctx, "board-1", job.ID); err != nil {
		t.Errorf("走っているジョブの Get() = %v, want 取れる", err)
	}
	close(llm.release)
}

func TestJobs_HidesJobsFromOthers(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validLLMOutput)
	jobs, boards := newJobs(t, llm)
	boards.members = []port.BoardMember{{BoardID: "board-1", UserID: "u2", Role: port.RoleEditor}}
	owner := asUser(t.Context(), "u1")

	job, err := jobs.Start(owner, "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}

	// u2 はボードのメンバーではない（fakeBoards は owner だけを通す）。ボード
	// ごと見えないので、ジョブの存在も分からない。
	other := asUser(t.Context(), "u2")
	if _, err := jobs.Get(other, "board-1", job.ID); !errors.Is(err, usecase.ErrBoardNotFound) {
		t.Errorf("他人の Get() = %v, want ErrBoardNotFound", err)
	}
	if _, err := jobs.List(other, "board-1"); !errors.Is(err, usecase.ErrBoardNotFound) {
		t.Errorf("他人の List() = %v, want ErrBoardNotFound", err)
	}
	if err := jobs.Discard(other, "board-1", job.ID); !errors.Is(err, usecase.ErrBoardNotFound) {
		t.Errorf("他人の Discard() = %v, want ErrBoardNotFound", err)
	}
	close(llm.release)
}

func TestJobs_ListIsScopedToBoardAndActor(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validLLMOutput)
	jobs, _ := newJobs(t, llm)
	ctx := asUser(t.Context(), "u1")

	job, _ := jobs.Start(ctx, "board-1", "annot-1", nil)

	list, err := jobs.List(ctx, "board-1")
	if err != nil || len(list) != 1 || list[0].ID != job.ID || list[0].Input.AnnotationID != "annot-1" {
		t.Fatalf("List() = %+v, %v, want 1 件", list, err)
	}
	if list[0].Status != usecase.JobRunning {
		t.Errorf("Status = %q, want running", list[0].Status)
	}

	// 同じ ID でも別のボードの URL からは引けない。
	if _, err := jobs.Get(ctx, "board-2", job.ID); !errors.Is(err, usecase.ErrBoardNotFound) {
		t.Errorf("別ボードの Get() = %v, want ErrBoardNotFound", err)
	}
	close(llm.release)
}

func TestJobs_ListOmitsResult(t *testing.T) {
	t.Parallel()

	jobs, _ := newJobs(t, &fakeLLM{responses: []string{validLLMOutput}})
	ctx := asUser(t.Context(), "u1")

	job, _ := jobs.Start(ctx, "board-1", "annot-1", nil)
	waitJob(t, jobs, ctx, job.ID)

	list, _ := jobs.List(ctx, "board-1")
	if len(list) != 1 || list[0].Status != usecase.JobSucceeded || list[0].Result != nil {
		t.Errorf("List() = %+v, want 成功・結果なし", list)
	}
}

func TestJobs_DiscardRunningCancelsAndFreesSlot(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validLLMOutput)
	jobs, _ := newJobs(t, llm)
	ctx := asUser(t.Context(), "u1")

	job, _ := jobs.Start(ctx, "board-1", "annot-1", nil)
	<-llm.started

	if err := jobs.Discard(ctx, "board-1", job.ID); err != nil {
		t.Fatalf("Discard() = %v", err)
	}
	select {
	case <-llm.ctxEnded:
	case <-time.After(5 * time.Second):
		t.Fatal("取り消しても LLM の呼び出しの ctx が終わらない")
	}
	if _, err := jobs.Get(ctx, "board-1", job.ID); !errors.Is(err, usecase.ErrJobNotFound) {
		t.Errorf("取り消した後の Get() = %v, want ErrJobNotFound", err)
	}

	// 枠が戻って、また始められる。
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := jobs.Start(ctx, "board-1", "annot-1", nil)
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

func TestJobs_DiscardFinishedDropsResult(t *testing.T) {
	t.Parallel()

	jobs, _ := newJobs(t, &fakeLLM{responses: []string{validLLMOutput}})
	ctx := asUser(t.Context(), "u1")

	job, _ := jobs.Start(ctx, "board-1", "annot-1", nil)
	waitJob(t, jobs, ctx, job.ID)

	if err := jobs.Discard(ctx, "board-1", job.ID); err != nil {
		t.Fatalf("Discard() = %v", err)
	}
	if err := jobs.Discard(ctx, "board-1", job.ID); !errors.Is(err, usecase.ErrJobNotFound) {
		t.Errorf("2 回目の Discard() = %v, want ErrJobNotFound", err)
	}
}

func TestJobs_TimeoutEndsStuckCall(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validLLMOutput)
	jobs, _ := newJobs(t, llm, usecase.WithJobTimeout(30*time.Millisecond))
	ctx := asUser(t.Context(), "u1")

	job, _ := jobs.Start(ctx, "board-1", "annot-1", nil)
	done := waitJob(t, jobs, ctx, job.ID)
	if done.Status != usecase.JobFailed {
		t.Errorf("Status = %q, want failed（応えない LLM に枠を握らせ続けない）", done.Status)
	}
}

func TestJobs_FreesSlotWhenFinished(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	jobs, _ := newJobs(t, llm)
	ctx := asUser(t.Context(), "u1")

	first, _ := jobs.Start(ctx, "board-1", "annot-1", nil)
	waitJob(t, jobs, ctx, first.ID)

	// 終わった直後に別のジョブを始められる（枠の解放は結果の書き込みより後なので
	// 少しだけ待つ）。
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := jobs.Start(ctx, "board-1", "annot-1", nil)
		if err == nil {
			return
		}
		if !errors.Is(err, usecase.ErrConcurrencyLimited) || time.Now().After(deadline) {
			t.Fatalf("Start() = %v, want 枠が戻る", err)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

type panicLLM struct{}

func (panicLLM) Complete(context.Context, port.VisionRequest) (port.VisionResponse, error) {
	panic("adapter bug")
}

func TestJobs_PanicInLLMFailsTheJobAndFreesSlot(t *testing.T) {
	t.Parallel()

	jobs, _ := newJobs(t, panicLLM{})
	ctx := asUser(t.Context(), "u1")

	job, err := jobs.Start(ctx, "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	done := waitJob(t, jobs, ctx, job.ID)
	if done.Status != usecase.JobFailed {
		t.Errorf("Status = %q, want failed（プロセスを落とさず、このジョブの失敗にする）", done.Status)
	}
}

// 走っている最中に保存で解釈の入力が変わったら、そのジョブを返さない（ADR 0085）。
//
// **前のジョブを捨てずに始め直す。** 捨ててからだと、鍵が注釈 ID だけでも新しい
// ジョブになって通る。枠は 1 つなので、返さなければ 429 に当たり、返すと err は
// nil になる。
func TestJobs_StartWhileRunningComparesSavedInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		from, to string
		sameJob  bool
	}{
		{name: "子のテキストを変えた", from: "返金導線が無い", to: "返金導線を足す"},
		// 名前はハッシュから外れたが（ADR 0079、#303）、解釈の入力には残っている。
		{name: "囲みの名前を変えた", from: `"name":"決済まわり"`, to: `"name":"決済の整理"`},
		{name: "粒度を変えた", from: `"granularity":""`, to: `"granularity":"epic"`},
		// 線や図形はテキストに出ない。画像は鍵に入れないので、同じ入力として扱う。
		{
			name:    "線だけ足した",
			from:    `{"id":"t9"`,
			to:      `{"id":"a1","type":"arrow","frameId":"annot-1"},{"id":"t9"`,
			sameJob: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			llm := newGatedLLM(validLLMOutput)
			jobs, boards := newJobs(t, llm)
			ctx := asUser(t.Context(), "u1")

			first, err := jobs.Start(ctx, "board-1", "annot-1", nil)
			if err != nil {
				t.Fatalf("Start() = %v", err)
			}

			changed := strings.Replace(interpretScene, tt.from, tt.to, 1)
			if changed == interpretScene {
				t.Fatalf("シーンが変わっていない（%q が無い）", tt.from)
			}
			boards.board.Scene = changed

			second, err := jobs.Start(ctx, "board-1", "annot-1", nil)
			if tt.sameJob {
				if err != nil || second.ID != first.ID {
					t.Errorf("Start() = (%q, %v), want 同じジョブ %q", second.ID, err, first.ID)
				}
			} else if !errors.Is(err, usecase.ErrConcurrencyLimited) {
				t.Errorf("Start() = (%q, %v), want ErrConcurrencyLimited（古い入力のジョブを返さない）",
					second.ID, err)
			}

			close(llm.release)
			waitJob(t, jobs, ctx, first.ID)
		})
	}
}
