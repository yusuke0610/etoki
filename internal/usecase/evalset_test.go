package usecase_test

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// 解釈の評価セット（ADR 0098）の道具。ケースを読み、本番と同じ口で絵解き、
// 採点用の報告を書く。
//
// **本物のモデルに向けて回すのは evalset_real_test.go**（ビルドタグ eval、
// `make eval`）。このファイルは道具と、道具が壊れていないことを偽の LLM で
// 確かめるテストだけを持ち、`make test` に入る。道具までタグの内側に置くと、
// 誰も回さないあいだに壊れ、課金して回した日に初めて気づく。

// evalRoot は評価セットの置き場所。1 つのディレクトリが 1 ケース。
const evalRoot = "testdata/eval"

// evalExpectedHeadings は expected.md に必ず置く見出し（ADR 0098）。
//
// 期待はタイトルの文言ではなく、この 3 つの箇条書きで書く。文言はモデルごとに
// 揺れるので、一致では測れない。**機械では読まない。** 報告に載せて、人が
// 出力と見比べる。
var evalExpectedHeadings = []string{
	"## 作ってほしいもの",
	"## 作ってほしくないもの",
	"## 本文に入っていてほしいこと",
}

// evalCase は評価セットの 1 ケース。
type evalCase struct {
	name string
	spec evalCaseSpec
	// scene は保存済みシーン（boards.scene と同じ形）。
	scene string
	// image は画面が書き出した囲みの画像（ADR 0018）。作り直すのは
	// web/e2e/evalImages.spec.ts。
	image []byte
	// expected は人が書いた期待。
	expected string
	// annotation は絵解く囲み。名前・粒度・種別を報告に出すために引いておく。
	annotation domain.Annotation
}

// evalCaseSpec は case.json の形。シーンにも画像にも載らない入力だけを持つ。
type evalCaseSpec struct {
	// AnnotationID は絵解く囲み（scene.json の frame）の ID。
	AnnotationID string `json:"annotationId"`
	// Premise はボードの背景（ADR 0093）。空なら添えない。
	Premise string `json:"premise,omitempty"`
	// Previous は前回この囲みから作ったもの（ADR 0026）。並べた順に p1, p2… の
	// ref が振られる（本番の toPreviousItems と同じ）。
	Previous []evalPreviousItem `json:"previous,omitempty"`
}

// evalPreviousItem は前回作ったもの 1 件。作成時に控えた形（ADR 0023）。
type evalPreviousItem struct {
	Kind  domain.ItemKind `json:"kind"`
	Title string          `json:"title"`
	Body  string          `json:"body"`
}

// loadEvalCases は root の下のディレクトリを 1 つずつケースとして読む。
//
// **読めないケースがあれば、ほかを読まずに止める。** 飛ばすと、報告から 1 件
// 消えたことに気づけない。
func loadEvalCases(root string) ([]evalCase, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var cases []evalCase
	for _, e := range entries {
		// README.md のように、ディレクトリでないものはケースではない。
		if !e.IsDir() {
			continue
		}
		c, err := loadEvalCase(filepath.Join(root, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		cases = append(cases, c)
	}
	return cases, nil
}

// loadEvalCase は 1 ケースを読み、絵解く前に分かる誤りをここで弾く。
func loadEvalCase(dir string) (evalCase, error) {
	c := evalCase{name: filepath.Base(dir)}
	read := func(name string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, name)) }

	raw, err := read("case.json")
	if err != nil {
		return c, err
	}
	// 綴りを間違えた項目（premise を premis と書く、など）を黙って無視すると、
	// 背景を添えたつもりのケースが背景なしで測られる。
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c.spec); err != nil {
		return c, fmt.Errorf("case.json: %w", err)
	}
	for i, p := range c.spec.Previous {
		if !p.Kind.Valid() || strings.TrimSpace(p.Title) == "" {
			return c, fmt.Errorf("case.json: previous[%d] には kind（epic / issue）と title が要る", i)
		}
	}

	scene, err := read("scene.json")
	if err != nil {
		return c, err
	}
	c.scene = string(scene)
	parsed, err := domain.ParseScene(scene)
	if err != nil {
		return c, fmt.Errorf("scene.json: %w", err)
	}
	i := slices.IndexFunc(parsed.Annotations(), func(a domain.Annotation) bool {
		return a.ID == c.spec.AnnotationID
	})
	if i < 0 {
		return c, fmt.Errorf("scene.json に注釈 %q が無い", c.spec.AnnotationID)
	}
	c.annotation = parsed.Annotations()[i]
	if err := c.annotation.CheckMeta(); err != nil {
		return c, fmt.Errorf("scene.json: %w", err)
	}

	if c.image, err = read("image.png"); err != nil {
		return c, err
	}
	// 本番は形式と大きさしか見ない（validateImages）。壊れた PNG はそのまま
	// LLM に渡り、画像なしで読んだのと区別の付かない結果になる。
	if _, err := png.DecodeConfig(bytes.NewReader(c.image)); err != nil {
		return c, fmt.Errorf("image.png: %w", err)
	}

	expected, err := read("expected.md")
	if err != nil {
		return c, err
	}
	lines := strings.Split(string(expected), "\n")
	for _, h := range evalExpectedHeadings {
		if !slices.Contains(lines, h) {
			return c, fmt.Errorf("expected.md に見出し %q が無い", h)
		}
	}
	c.expected = string(expected)

	return c, nil
}

// previousItemID は前回ぶんの i 件目に振る item ID。GitHub の ID の代わり。
func previousItemID(i int) string { return fmt.Sprintf("PVTI_eval_%d", i+1) }

// previousRuns は前回ぶんを、本番の畳み込み（ListItemsByBoard）が返す形で置く。
func (c evalCase) previousRuns() []port.SyncRun {
	if len(c.spec.Previous) == 0 {
		return nil
	}
	items := make([]port.SyncItem, len(c.spec.Previous))
	for i, p := range c.spec.Previous {
		items[i] = port.SyncItem{
			ItemID: previousItemID(i),
			Kind:   port.ItemKind(p.Kind),
			Title:  p.Title,
			Body:   p.Body,
			// 畳み込みに入るのは届いたと分かっているものだけ（ADR 0056）。
			Confirmed: true,
		}
	}
	return []port.SyncRun{{
		BoardID:      "board-1",
		AnnotationID: c.spec.AnnotationID,
		Outcome:      port.OutcomeComplete,
		Items:        items,
	}}
}

// previousItems は前回ぶんを、本番が LLM に見せる形（ref つき）で返す。
// 出力を検査し直すときと、報告で ref を引くときに使う。
func (c evalCase) previousItems() []domain.PreviousItem {
	out := make([]domain.PreviousItem, len(c.spec.Previous))
	for i, p := range c.spec.Previous {
		out[i] = domain.PreviousItem{
			Ref:    fmt.Sprintf("p%d", i+1),
			ItemID: previousItemID(i),
			Kind:   p.Kind,
			Title:  p.Title,
			Body:   p.Body,
		}
	}
	return out
}

// evalRun は 1 ケースを 1 回絵解いた記録。
type evalRun struct {
	c     evalCase
	calls []evalCall
	// result は通ったときだけ入る。
	result *usecase.InterpretationResult
	// err は始められなかったか、走って失敗した理由。通ったら nil。
	err     error
	elapsed time.Duration
}

// evalCall は LLM の呼び出し 1 回ぶん。再送を含めて、呼んだ順に並ぶ。
type evalCall struct {
	elapsed time.Duration
	usage   port.Usage
	output  string
	err     error
	// rejected は出力が検査に通らなかった理由。通ったときと、呼び出しそのものが
	// 失敗したときは空。
	rejected string
}

// runEvalCase は 1 ケースを、本番と同じ口（InterpretationJobs.Start）で絵解く。
//
// **組み立て・検査・再送・ジョブの上限は本番のものを通す。** 評価のために
// 別の経路で LLM を呼ぶと、測っているのが本番の絵解きではなくなる（ADR 0098）。
// ここが用意するのは、ボードと前回ぶんの置き場（フェイク）と、呼び出しを
// 控える包みだけ。
func runEvalCase(ctx context.Context, c evalCase, client port.LLMClient, logger *slog.Logger) evalRun {
	rec := &recordingLLM{next: client}
	board := newBoard(c.scene)
	board.Premise = c.spec.Premise
	svc := usecase.NewInterpretationService(
		&fakeBoards{board: board}, &fakeMappings{runs: c.previousRuns()}, rec,
		usecase.NewLLMLimiter(usecase.LLMLimits{}), usecase.WithLogger(logger))
	jobs := usecase.NewInterpretationJobs(svc)

	run := evalRun{c: c}
	started := time.Now()
	job, err := jobs.Start(ctx, board.ID, c.spec.AnnotationID,
		[]port.Image{{MediaType: "image/png", Data: c.image}})
	if err == nil {
		job, err = awaitEvalJob(ctx, jobs, board.ID, job.ID)
	}
	run.elapsed = time.Since(started)

	switch {
	case err != nil:
		run.err = err
	case job.Status == usecase.JobFailed:
		run.err = job.Err
	default:
		run.result = job.Result
	}

	// 検査に落ちた出力に理由を付ける。本番は理由を再送の指示にだけ使い、
	// 返さない。同じ検査（parseInterpretation）を通し直して取る。
	run.calls = rec.taken()
	previous := c.previousItems()
	for i := range run.calls {
		call := &run.calls[i]
		if call.err != nil {
			continue
		}
		if _, err := usecase.ParseInterpretation(call.output, c.annotation.Granularity, previous); err != nil {
			call.rejected = err.Error()
		}
	}

	return run
}

// awaitEvalJob はジョブが走り終わるまで待つ。
//
// **Get を通して待つ。** 結果はジョブの goroutine が書くので、鍵を握って読む
// Get を挟む（waitDone と同じ理由）。上限は置かない。待つ長さはジョブの上限
// （DefaultJobTimeout）が本番と同じ値で決める。
func awaitEvalJob(
	ctx context.Context, jobs *usecase.InterpretationJobs, boardID, id string,
) (usecase.InterpretationJob, error) {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		job, err := jobs.Get(ctx, boardID, id)
		if err != nil || job.Status != usecase.JobRunning {
			return job, err
		}
		select {
		case <-ctx.Done():
			return job, ctx.Err()
		case <-tick.C:
		}
	}
}

// recordingLLM は呼び出しを 1 回ずつ控えて、next に渡す。
//
// 解釈はジョブの goroutine で走るので、控えは鍵を握って読み書きする。
type recordingLLM struct {
	next  port.LLMClient
	mu    sync.Mutex
	calls []evalCall
}

func (r *recordingLLM) Complete(ctx context.Context, req port.VisionRequest) (port.VisionResponse, error) {
	started := time.Now()
	resp, err := r.next.Complete(ctx, req)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, evalCall{
		elapsed: time.Since(started), usage: resp.Usage, output: resp.Text, err: err,
	})
	return resp, err
}

func (r *recordingLLM) taken() []evalCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

// failureLabel は失敗を、打ち手の違う 3 つに分けて言う。細かい原因は報告の
// 本文に元のエラーごと載せる。
//
// **原因の一覧をここに写さない。** 呼び出しそのものの失敗かは
// usecase.IsLLMCallFailure が決める。写すと、原因を足した日にここだけ「その他」に
// 落ちる。
func failureLabel(err error) string {
	switch {
	case errors.Is(err, usecase.ErrInterpretationFailed):
		return "出力が検査に通らなかった"
	case usecase.IsLLMCallFailure(err):
		return "呼び出しが失敗した"
	default:
		return "その他"
	}
}

// evalReport は 1 回の評価の報告。
//
// **採点の欄は空けて書く。** 付けるのは人（ADR 0098）。機械で取れるもの
// （通ったか、呼んだ回数、トークン数、時間）だけを埋める。
type evalReport struct {
	startedAt time.Time
	// model と endpoint は何に向けて回したか。**鍵は載せない。** 報告は PR に
	// 貼るもの。
	model    string
	endpoint string
	// commit は回したときのコミット。make eval が渡す。
	commit string
	runs   []evalRun
}

// endpointOf は向け先の URL から、報告に載せてよい部分（スキームとホスト）だけを
// 取り出す。ユーザー情報やパスを落とす。
func endpointOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "（読めない URL）"
	}
	return u.Scheme + "://" + u.Host
}

// markdown は報告を Markdown にする。先頭の表が PR の本文に貼る要約。
func (r evalReport) markdown() string {
	var b strings.Builder

	fmt.Fprintf(&b, "# 絵解きの評価（%s）\n\n", r.startedAt.UTC().Format("2006-01-02 15:04 UTC"))
	fmt.Fprintf(&b, "- モデル: `%s`\n", r.model)
	fmt.Fprintf(&b, "- 向け先: `%s`\n", r.endpoint)
	fmt.Fprintf(&b, "- コミット: `%s`\n\n", cmp.Or(r.commit, "不明"))
	b.WriteString("採点の 5 列（漏れ・余計なもの・タイトル・本文・構造）は人が ○ / △ / × で付ける。\n")
	b.WriteString("見方は `internal/usecase/testdata/eval/README.md`。\n\n")

	b.WriteString("| ケース | 種別 | 粒度 | 結果 | 呼んだ回数 | 入力トークン | 出力トークン | 時間 " +
		"| 漏れ | 余計なもの | タイトル | 本文 | 構造 |\n")
	b.WriteString("| --- | --- | --- | --- | --: | --: | --: | --: | :-: | :-: | :-: | :-: | :-: |\n")
	for _, run := range r.runs {
		in, out := run.tokens()
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %d | %s | %s | %s |  |  |  |  |  |\n",
			run.c.name, cmp.Or(string(run.c.annotation.Kind), "指定なし"),
			cmp.Or(string(run.c.annotation.Granularity), "指定なし"), run.outcome(),
			len(run.calls), tokenCount(in), tokenCount(out), seconds(run.elapsed))
	}

	for _, run := range r.runs {
		run.writeSection(&b)
	}
	return b.String()
}

// outcome は表の「結果」の欄。
func (run evalRun) outcome() string {
	if run.err != nil {
		return "失敗（" + failureLabel(run.err) + "）"
	}
	return "通った"
}

// tokens は呼び出しを通したトークン数の合計。報告しない LLMClient では 0 で、
// 0 は「報告が無かった」と読む（port.Usage）。
func (run evalRun) tokens() (in, out int) {
	for _, call := range run.calls {
		in += call.usage.InputTokens
		out += call.usage.OutputTokens
	}
	return in, out
}

// writeSection はケース 1 つぶんの節を書く。期待・出力・呼び出しの順。
func (run evalRun) writeSection(b *strings.Builder) {
	c := run.c
	fmt.Fprintf(b, "\n## %s（%s）\n\n", c.name, cmp.Or(c.annotation.Name, "名前なし"))
	if c.spec.Premise != "" {
		b.WriteString("ボードの背景を添えた。\n\n")
	}
	if n := len(c.spec.Previous); n > 0 {
		fmt.Fprintf(b, "前回この囲みから作ったものが %d 件ある。\n\n", n)
	}

	b.WriteString("### 期待\n\n")
	b.WriteString(demoteHeadings(withoutTitle(c.expected), 2))
	b.WriteString("\n\n### 出力\n\n")
	var out strings.Builder
	if run.err != nil {
		fmt.Fprintf(&out, "失敗（%s）: %s", failureLabel(run.err), oneLineText(run.err.Error()))
	} else {
		run.writeItems(&out)
	}
	b.WriteString(strings.TrimRight(out.String(), "\n"))

	b.WriteString("\n\n### 呼び出し\n\n")
	if len(run.calls) == 0 {
		b.WriteString("LLM を呼んでいない。\n")
		return
	}
	b.WriteString("| 回 | 時間 | 入力トークン | 出力トークン | 結果 |\n| --: | --: | --: | --: | --- |\n")
	for i, call := range run.calls {
		fmt.Fprintf(b, "| %d | %s | %s | %s | %s |\n", i+1, seconds(call.elapsed),
			tokenCount(call.usage.InputTokens), tokenCount(call.usage.OutputTokens), call.result())
	}
	// 通らなかった出力は、そのまま見られるように残す。通った出力は上の
	// 「出力」と同じものなので重ねない。
	for i, call := range run.calls {
		if call.rejected == "" {
			continue
		}
		fmt.Fprintf(b, "\n<details><summary>%d 回目の出力</summary>\n\n%s\n\n</details>\n", i+1, fenced(call.output))
	}
}

// result は呼び出しの表の「結果」の欄。
func (call evalCall) result() string {
	switch {
	case call.err != nil:
		return "呼び出しの失敗: " + cell(call.err.Error())
	case call.rejected != "":
		return "検査に通らなかった: " + cell(call.rejected)
	default:
		return "通った"
	}
}

// writeItems は通った解釈を、項目ごとに書く。本文は手を加えずに囲む。
func (run evalRun) writeItems(b *strings.Builder) {
	in := run.result.Interpretation
	fmt.Fprintf(b, "summary: %s\n\n", oneLineText(in.Summary))

	refs := map[string]domain.PreviousItem{}
	for _, p := range run.c.previousItems() {
		refs[p.ItemID] = p
	}
	for _, it := range in.Items {
		fmt.Fprintf(b, "#### %s（%s）%s\n\n", it.LocalID, it.Kind, oneLineText(it.Title))
		var notes []string
		if it.ParentLocalID != nil {
			notes = append(notes, "親: "+*it.ParentLocalID)
		}
		if it.PreviousItemID != nil {
			p := refs[*it.PreviousItemID]
			notes = append(notes, fmt.Sprintf("前回の %s（%s）を書き換える", p.Ref, oneLineText(p.Title)))
		}
		if len(notes) > 0 {
			b.WriteString(strings.Join(notes, " ／ ") + "\n\n")
		}
		if strings.TrimSpace(it.Body) == "" {
			b.WriteString("本文なし\n\n")
			continue
		}
		b.WriteString(fenced(it.Body) + "\n\n")
	}
}

// withoutTitle は expected.md の先頭の表題（`# …`）を外す。Markdown の検査が
// 先頭の見出しを求めるので置いてあるが、報告ではケースの節の見出しと重なる。
func withoutTitle(md string) string {
	md = strings.TrimSpace(md)
	if strings.HasPrefix(md, "# ") {
		_, rest, _ := strings.Cut(md, "\n")
		md = strings.TrimSpace(rest)
	}
	return md
}

// demoteHeadings は見出しを n 段下げる。expected.md の見出しをケースの節の下に
// 収めるため。
func demoteHeadings(md string, n int) string {
	lines := strings.Split(md, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "#") {
			lines[i] = strings.Repeat("#", n) + l
		}
	}
	return strings.Join(lines, "\n")
}

// backticks はバッククォートの連なり。
var backticks = regexp.MustCompile("`+")

// fenced は s を、中の ``` で閉じないフェンスで囲む。LLM の出力は何を含むか
// 分からない。
func fenced(s string) string {
	longest := 0
	for _, m := range backticks.FindAllString(s, -1) {
		longest = max(longest, len(m))
	}
	fence := strings.Repeat("`", max(3, longest+1))
	return fence + "text\n" + strings.TrimRight(s, "\n") + "\n" + fence
}

// oneLineText は改行と連続する空白を 1 つの空白に畳む。
func oneLineText(s string) string { return strings.Join(strings.Fields(s), " ") }

// cell は表の 1 マスに入れられる形にする。区切りの | を逃がす。
func cell(s string) string { return strings.ReplaceAll(oneLineText(s), "|", `\|`) }

func tokenCount(n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprint(n)
}

func seconds(d time.Duration) string { return fmt.Sprintf("%.1f 秒", d.Seconds()) }

// writeEvalReport は報告を dir の下に書き、書いたパスを返す。
func writeEvalReport(dir string, r evalReport) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "report.md")
	return path, os.WriteFile(path, []byte(r.markdown()), 0o644)
}

// evalFakeOutput は偽の LLM に返させる、c の粒度で検査を通る出力。前回ぶんが
// あるケースでは、1 件目を前回の p1 に対応づける。
func evalFakeOutput(c evalCase) string {
	ref := "null"
	if len(c.spec.Previous) > 0 {
		ref = `"p1"`
	}
	if c.annotation.Granularity == domain.GranularityIssue {
		return `{"summary":"偽の LLM の出力","items":[` +
			`{"localId":"i1","kind":"issue","title":"偽の項目","body":"","parentLocalId":null,"previousRef":` + ref + `}]}`
	}
	return `{"summary":"偽の LLM の出力","items":[` +
		`{"localId":"e1","kind":"epic","title":"偽のまとまり","body":"","parentLocalId":null,"previousRef":null},` +
		`{"localId":"i1","kind":"issue","title":"偽の項目","body":"","parentLocalId":"e1","previousRef":` + ref + `}]}`
}

// 評価セットのケースがどれも読めて、本番と同じ口で絵解けることを、偽の LLM で
// 確かめる。
//
// **本物のモデルは呼ばない**（make test と CI に入る）。落ちるのは、ケースの形が
// 崩れたとき（シーン・画像・期待）と、始める時点で弾かれるとき（画像の大きさ、
// 囲みの ID）。make eval を回してから気づくと、課金だけが残る。
//
// **本番の絵解きと同じ材料が LLM に届いたかも見る。** 道具が画像や背景を渡し
// そこねると、評価は本番と違うものを測ったまま、それらしい報告を出す。
func TestEvalSet_EveryCaseRunsThroughTheService(t *testing.T) {
	t.Parallel()

	cases, err := loadEvalCases(evalRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatalf("%s にケースが 1 つも無い", evalRoot)
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeLLM{responses: []string{evalFakeOutput(c)}}
			run := runEvalCase(t.Context(), c, fake, slog.New(slog.DiscardHandler))
			if run.err != nil {
				t.Fatalf("絵解けない: %v", run.err)
			}
			if len(run.calls) != 1 || run.calls[0].rejected != "" {
				t.Fatalf("calls = %+v, want 検査に通った 1 回", run.calls)
			}

			req := fake.requests[0]
			if len(req.Images) != 1 || !bytes.Equal(req.Images[0].Data, c.image) {
				t.Errorf("画像が image.png のまま届いていない（%d 枚）", len(req.Images))
			}
			// 背景は行ごとに印を付けて渡る（ADR 0093）ので、行ごとに見る。
			for line := range strings.SplitSeq(c.spec.Premise, "\n") {
				if line = strings.TrimSpace(line); line != "" && !strings.Contains(req.Text, line) {
					t.Errorf("背景の行 %q が届いていない", line)
				}
			}
			// 前回ぶんは ref で届く。p1 に対応づけた出力が、置いた 1 件目の ID に
			// 解決されれば、前回ぶんが本番の形で渡っている。
			if len(c.spec.Previous) > 0 {
				items := run.result.Interpretation.Items
				last := items[len(items)-1]
				if last.PreviousItemID == nil || *last.PreviousItemID != previousItemID(0) {
					t.Errorf("previousItemId = %v, want %s", last.PreviousItemID, previousItemID(0))
				}
			}
		})
	}
}

// 報告に、期待と出力と呼び出しの記録が並び、採点の欄が空いていることを確かめる。
// 失敗したケースも、何回呼んで何が通らなかったかが読めること。
func TestEvalSet_ReportShowsExpectationOutputAndCalls(t *testing.T) {
	t.Parallel()

	cases, err := loadEvalCases(evalRoot)
	if err != nil {
		t.Fatal(err)
	}
	c := cases[0]
	logger := slog.New(slog.DiscardHandler)

	passed := runEvalCase(t.Context(), c, &fakeLLM{
		responses: []string{evalFakeOutput(c)},
		usages:    []port.Usage{{InputTokens: 1234, OutputTokens: 56}},
	}, logger)
	failed := runEvalCase(t.Context(), c, &fakeLLM{responses: []string{"JSON ではない"}}, logger)
	if failed.err == nil || len(failed.calls) != 3 {
		t.Fatalf("failed = %v / %d 回, want 3 回呼んで失敗", failed.err, len(failed.calls))
	}

	md := evalReport{
		startedAt: time.Date(2026, 11, 15, 13, 0, 0, 0, time.UTC),
		model:     "test-model",
		endpoint:  "http://127.0.0.1:8090",
		commit:    "abc1234",
		runs:      []evalRun{passed, failed},
	}.markdown()

	for _, want := range []string{
		"- モデル: `test-model`",
		"- 向け先: `http://127.0.0.1:8090`",
		"- コミット: `abc1234`",
		// 要約の行。採点の 5 マスは空いている。
		fmt.Sprintf("| %s | %s | %s | 通った | 1 | 1234 | 56 |", c.name,
			cmp.Or(string(c.annotation.Kind), "指定なし"), cmp.Or(string(c.annotation.Granularity), "指定なし")),
		"|  |  |  |  |  |\n",
		fmt.Sprintf("| %s | %s | %s | 失敗（出力が検査に通らなかった） | 3 | - | - |", c.name,
			cmp.Or(string(c.annotation.Kind), "指定なし"), cmp.Or(string(c.annotation.Granularity), "指定なし")),
		// 期待は見出しを下げてそのまま載る。
		"#### 作ってほしいもの",
		"#### 作ってほしくないもの",
		"#### 本文に入っていてほしいこと",
		// 出力。
		"summary: 偽の LLM の出力",
		"（issue）偽の項目",
		// 通らなかった出力と、その理由。
		"| 3 | ",
		"検査に通らなかった: parse interpretation:",
		"<details><summary>3 回目の出力</summary>",
		"JSON ではない",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("報告に %q が無い\n---\n%s", want, md)
		}
	}
	// 期待の本文（見出しの下の箇条書き）も落とさない。
	for line := range strings.SplitSeq(c.expected, "\n") {
		if strings.HasPrefix(line, "- ") && !strings.Contains(md, line) {
			t.Errorf("期待の行 %q が報告に無い", line)
		}
	}
}

// ケースの読み込みが、絵解く前に分かる誤りを弾くこと。通すと、背景を添えた
// つもりのケースが背景なしで測られる、といった食い違いが報告に現れないまま残る。
func TestLoadEvalCase_RejectsBrokenCases(t *testing.T) {
	t.Parallel()

	// 壊す前の写しは読める。読めないなら、下の失敗は写し方のせいかもしれない。
	base := filepath.Join(t.TempDir(), "todo")
	copyEvalCase(t, filepath.Join(evalRoot, "todo"), base)
	if _, err := loadEvalCase(base); err != nil {
		t.Fatalf("写しを読めない: %v", err)
	}

	for name, tt := range map[string]struct {
		file, content, want string
	}{
		"case.json の綴り違い": {
			"case.json", `{"annotationId":"frame-todo","premis":"背景"}`, `unknown field "premis"`,
		},
		"前回分に種別が無い": {
			"case.json", `{"annotationId":"frame-todo","previous":[{"title":"探す"}]}`, "previous[0]",
		},
		"シーンに無い囲み": {
			"case.json", `{"annotationId":"frame-none"}`, `注釈 "frame-none" が無い`,
		},
		"PNG でない画像": {
			"image.png", "PNG ではない", "image.png:",
		},
		"期待の見出しが欠けている": {
			"expected.md", "# todo\n\n## 作ってほしいもの\n\n- 何か\n", `見出し "## 作ってほしくないもの" が無い`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "todo")
			copyEvalCase(t, base, dir)
			if err := os.WriteFile(filepath.Join(dir, tt.file), []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := loadEvalCase(dir); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("loadEvalCase() = %v, want %q を含むエラー", err, tt.want)
			}
		})
	}
}

// copyEvalCase はケースの 4 つのファイルを写す。
func copyEvalCase(t *testing.T, from, to string) {
	t.Helper()

	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"case.json", "scene.json", "image.png", "expected.md"} {
		b, err := os.ReadFile(filepath.Join(from, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// LLM の本文に ``` が入っていても、報告のフェンスがそこで閉じないこと。閉じると、
// 本文の残りが報告の Markdown として描かれる。
func TestFenced_OutlastsFencesInside(t *testing.T) {
	t.Parallel()

	got := fenced("例:\n```go\nfmt.Println()\n```\n")
	want := "````text\n例:\n```go\nfmt.Println()\n```\n````"
	if got != want {
		t.Errorf("fenced() = %q, want %q", got, want)
	}
}

// 向け先は、スキームとホストだけを報告に載せる。報告は PR に貼るので、URL に
// 埋めた資格情報やパスを写さない。
func TestEndpointOf_KeepsOnlySchemeAndHost(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"https://api.anthropic.com":                "https://api.anthropic.com",
		"http://127.0.0.1:11434":                   "http://127.0.0.1:11434",
		"https://user:secret@gateway.example/v1?x": "https://gateway.example",
		"not a url": "（読めない URL）",
	} {
		if got := endpointOf(raw); got != want {
			t.Errorf("endpointOf(%q) = %q, want %q", raw, got, want)
		}
	}
}
