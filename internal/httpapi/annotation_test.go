package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/port"
)

// 履歴は畳まずに 1 回ずつ、新しい順で返す（ADR 0007 / 0026）。
func TestListAnnotationRuns(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	id := createBoard(t, r, "ボード")
	saveAnnotatedScene(t, r, id)

	for _, title := range []string{"1 回目", "2 回目"} {
		if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
			BoardID:      id,
			AnnotationID: "annot-1",
			ContentHash:  currentHash(t, r, id),
			// 時刻は同じ。並びが時刻に依存していれば、ここで崩れる。
			CreatedAt: fixedTime,
			Outcome:   port.OutcomeComplete,
			Items: []port.SyncItem{{
				ItemID: "PVTI_" + title, Kind: port.KindEpic, Title: title,
				LocalID: "e1", Action: port.ActionCreated, Confirmed: true, CreatedAt: fixedTime,
			}},
		}); err != nil {
			t.Fatalf("SaveRun: %v", err)
		}
	}

	rec := do(t, r, http.MethodGet,
		"/api/boards/"+id+"/annotations/annot-1/runs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}

	got := decode[[]apitypes.SyncRun](t, rec)
	if len(got) != 2 {
		t.Fatalf("件数 = %d, want 2 (%+v)", len(got), got)
	}
	if len(got[0].Items) != 1 || got[0].Items[0].Title != "2 回目" {
		t.Errorf("先頭 = %+v, want 2 回目（新しい順）", got[0].Items)
	}
	if got[1].Items[0].Title != "1 回目" {
		t.Errorf("2 番目 = %+v, want 1 回目", got[1].Items)
	}
	if got[0].ID <= got[1].ID {
		t.Errorf("id = %d, %d, want 降順", got[0].ID, got[1].ID)
	}
}

// 途中で失敗した run は、契約にもそれと分かる形で出る（ADR 0043）。ここが
// 切れると、画面は「作れた件数が少ない run」しか受け取れない（#110）。
func TestListAnnotationRuns_ShowsIncomplete(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	id := createBoard(t, r, "ボード")
	saveAnnotatedScene(t, r, id)

	hash := currentHash(t, r, id)
	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID: id, AnnotationID: "annot-1", ContentHash: hash, CreatedAt: fixedTime,
		Outcome: port.OutcomeIncomplete, Error: "github graphql: rate limited",
		Items: []port.SyncItem{{
			ItemID: "PVTI_e1", Kind: port.KindEpic, Title: "作れたほう",
			LocalID: "e1", Action: port.ActionCreated, Confirmed: true, CreatedAt: fixedTime,
		}},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	runs := decodeOK[[]apitypes.SyncRun](t, do(t, r, http.MethodGet,
		"/api/boards/"+id+"/annotations/annot-1/runs", nil))
	if len(runs) != 1 {
		t.Fatalf("件数 = %d, want 1", len(runs))
	}
	if runs[0].Outcome == nil || *runs[0].Outcome != apitypes.RunOutcomeIncomplete {
		t.Errorf("outcome = %v, want incomplete", runs[0].Outcome)
	}
	if runs[0].Error != "github graphql: rate limited" {
		t.Errorf("error = %q, want GitHub の本文", runs[0].Error)
	}

	// 一覧にも出す。履歴を開かないと気づけないのでは、見せたことにならない。
	// **状態は created のまま**（作れたぶんは記録されている、ADR 0009）。
	states := decodeOK[apitypes.BoardAnnotations](t, do(t, r, http.MethodGet,
		"/api/boards/"+id+"/annotations", nil)).Annotations
	if len(states) != 1 {
		t.Fatalf("注釈 = %d 件, want 1", len(states))
	}
	if states[0].State != apitypes.SyncStateCreated {
		t.Errorf("state = %v, want created", states[0].State)
	}
	if states[0].LastRunOutcome == nil ||
		*states[0].LastRunOutcome != apitypes.RunOutcomeIncomplete {
		t.Errorf("lastRunOutcome = %v, want incomplete", states[0].LastRunOutcome)
	}
}

// 記録していなかった頃の run は「不明」として出す。**complete に倒さない。**
// 埋めると、列を足す前の途中失敗が成功として残る（ADR 0043）。
func TestListAnnotationRuns_OmitsOutcomeWhenNotRecorded(t *testing.T) {
	t.Parallel()

	r, mappings, db := newRouterWithDB(t)
	id := createBoard(t, r, "ボード")
	saveAnnotatedScene(t, r, id)

	// 移行前の run を再現する。port 経由では書けないので DB に直接入れる。
	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID: id, AnnotationID: "annot-1", ContentHash: currentHash(t, r, id),
		CreatedAt: fixedTime, Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{{
			ItemID: "PVTI_e1", Kind: port.KindEpic, Title: "古い run",
			LocalID: "e1", Action: port.ActionCreated, Confirmed: true, CreatedAt: fixedTime,
		}},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if _, err := db.ExecContext(t.Context(),
		`UPDATE sync_runs SET outcome = NULL`); err != nil {
		t.Fatalf("clear outcome: %v", err)
	}

	runs := decodeOK[[]apitypes.SyncRun](t, do(t, r, http.MethodGet,
		"/api/boards/"+id+"/annotations/annot-1/runs", nil))
	if len(runs) != 1 {
		t.Fatalf("件数 = %d, want 1", len(runs))
	}
	if runs[0].Outcome != nil {
		t.Errorf("outcome = %v, want 省略（記録していない）", *runs[0].Outcome)
	}

	states := decodeOK[apitypes.BoardAnnotations](t, do(t, r, http.MethodGet,
		"/api/boards/"+id+"/annotations", nil)).Annotations
	if len(states) != 1 {
		t.Fatalf("注釈 = %d 件, want 1", len(states))
	}
	if states[0].LastRunOutcome != nil {
		t.Errorf("lastRunOutcome = %v, want 省略", *states[0].LastRunOutcome)
	}
}

func TestListAnnotationRuns_Empty(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodGet,
		"/api/boards/"+id+"/annotations/annot-1/runs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	// null ではなく空配列。一覧は常に配列で返す。
	if body := rec.Body.String(); body != "[]" {
		t.Errorf("body = %q, want %q", body, "[]")
	}
}

func TestListAnnotationRuns_BoardNotFound(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodGet,
		"/api/boards/no-such-id/annotations/annot-1/runs", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// 注釈のないボードでは空配列を返す。
//
// **どちらも null にしない。** 文字列で見るのは、片方を作り忘れても件数の
// 検査は 0 で通ってしまうため。
func TestListAnnotations_Empty(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	rec := do(t, r, http.MethodGet, "/api/boards/"+id+"/annotations", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != `{"annotations":[],"detached":[]}` {
		t.Errorf("body = %q", body)
	}
}

// frame を消して保存しても、GitHub に作ったものへは辿れる（#111）。
//
// **名前も 3 状態も返さない。** シーンに frame が無いので取りようが無く、
// 既定で埋めると「名前の無い注釈」（ADR 0022）と見分けが付かなくなる。
func TestListAnnotations_Detached(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	id := createBoard(t, r, "ボード")
	base := saveAnnotatedScene(t, r, id)

	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID:      id,
		AnnotationID: "annot-1",
		ContentHash:  currentHash(t, r, id),
		CreatedAt:    fixedTime,
		Outcome:      port.OutcomeComplete,
		Items: []port.SyncItem{{
			ItemID: "PVTI_e1", Kind: port.KindEpic, Title: "決済API",
			LocalID: "e1", Action: port.ActionCreated, Confirmed: true, CreatedAt: fixedTime,
		}},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	// 注釈の frame ごと消して保存する。GitHub 側の draft issue は残っている。
	if rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		saveSceneBody(`{"type":"excalidraw","elements":[]}`, base)); rec.Code != http.StatusOK {
		t.Fatalf("save scene: %d %s", rec.Code, rec.Body)
	}

	got := listAnnotations(t, r, id)
	if len(got.Annotations) != 0 {
		t.Errorf("annotations = %v, want 空（frame は消えている）", got.Annotations)
	}
	if len(got.Detached) != 1 {
		t.Fatalf("detached = %d 件, want 1", len(got.Detached))
	}

	d := got.Detached[0]
	if d["id"] != "annot-1" {
		t.Errorf("id = %v, want annot-1", d["id"])
	}
	if _, ok := d["name"]; ok {
		t.Error("name を返している。シーンから消えているので取りようが無い")
	}
	if _, ok := d["state"]; ok {
		t.Error("state を返している。比べる相手のテキストがシーンに無い")
	}
	if _, ok := d["lastSyncedAt"]; !ok {
		t.Error("lastSyncedAt が無い。いつ実行したのかも見分ける材料になる")
	}
	items, _ := d["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %d 件, want 1", len(items))
	}
	// 辿る先はここにしか無い。名前が取れない以上、何の囲みだったかは
	// 作ったものから読むしかない。
	if it, _ := items[0].(map[string]any); it["title"] != "決済API" {
		t.Errorf("items[0].title = %v", it["title"])
	}
}

func TestListAnnotations_BoardNotFound(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodGet, "/api/boards/no-such-id/annotations", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// 一度も issue 化していない注釈は uncreated になる。
func TestListAnnotations_Uncreated(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")
	saveAnnotatedScene(t, r, id)

	got := listAnnotations(t, r, id).Annotations

	if len(got) != 1 {
		t.Fatalf("注釈の件数 = %d, want 1 (%+v)", len(got), got)
	}
	if got[0]["id"] != "annot-1" {
		t.Errorf("id = %v", got[0]["id"])
	}
	if got[0]["state"] != "uncreated" {
		t.Errorf("state = %v, want uncreated", got[0]["state"])
	}
	if got[0]["granularity"] != "epic" {
		t.Errorf("granularity = %v, want epic", got[0]["granularity"])
	}
	if _, ok := got[0]["lastSyncedAt"]; ok {
		t.Error("未実行なのに lastSyncedAt が入っている")
	}
	// テンプレートから始めていない注釈には種別が無い。**空文字を載せない。**
	// 載せると DiagramKind の enum に無い値が契約の外から出る。
	if _, ok := got[0]["kind"]; ok {
		t.Errorf("種別を選んでいないのに kind が入っている: %v", got[0]["kind"])
	}
}

// テンプレートから始めた注釈は、選んだ種別が一覧にも出る。
//
// **出ないと画面が種別を見せられない。** 種別はハッシュの入力でもあるので、
// 何の図として解釈されるかを開発者が確かめられる必要がある（中核思想 3）。
func TestListAnnotations_ReturnsKind(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	scene := `{"type":"excalidraw","elements":[
		{"id":"annot-1","type":"frame","name":"決済まわり",
		 "customData":{"etoki":{"granularity":"","kind":"sequence"}}},
		{"id":"t1","type":"text","text":"注文する","frameId":"annot-1"}]}`
	if rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		saveSceneBody(scene, fixedTime)); rec.Code != http.StatusOK {
		t.Fatalf("save scene: %d %s", rec.Code, rec.Body)
	}

	got := listAnnotations(t, r, id).Annotations

	if len(got) != 1 {
		t.Fatalf("注釈の件数 = %d, want 1 (%+v)", len(got), got)
	}
	if got[0]["kind"] != "sequence" {
		t.Errorf("kind = %v, want sequence", got[0]["kind"])
	}
}

// 届いたか分からない書き込みは、在るものとは別のキーで返る（ADR 0056、#170）。
//
// **畳み込みには入らない**ので、`items` に混ざると「いま GitHub に在る N 件」が
// 嘘になる。ここが繋がっていないと、画面は記録があることに気づけない。
func TestListAnnotations_SeparatesUnconfirmedItems(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	id := createBoard(t, r, "ボード")
	saveAnnotatedScene(t, r, id)

	// 1 件目は書けて、2 件目は応答を失った run。
	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID:      id,
		AnnotationID: "annot-1",
		ContentHash:  currentHash(t, r, id),
		CreatedAt:    fixedTime,
		Outcome:      port.OutcomeIncomplete,
		Error:        "Post ...: EOF",
		Items: []port.SyncItem{
			{ItemID: "PVTI_e1", Kind: port.KindEpic, Title: "決済API", LocalID: "e1", Action: port.ActionCreated, Confirmed: true, CreatedAt: fixedTime},
			{ItemID: "", Kind: port.KindIssue, Title: "SDK更新", LocalID: "i1", Action: port.ActionCreated, Confirmed: false, CreatedAt: fixedTime},
		},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	got := listAnnotations(t, r, id).Annotations

	items, _ := got[0]["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %d 件, want 1（未確定が混ざっている）", len(items))
	}
	if it, _ := items[0].(map[string]any); it["confirmed"] != true {
		t.Errorf("items[0].confirmed = %v, want true", it["confirmed"])
	}

	unconfirmed, _ := got[0]["unconfirmedItems"].([]any)
	if len(unconfirmed) != 1 {
		t.Fatalf("unconfirmedItems = %d 件, want 1", len(unconfirmed))
	}
	it, _ := unconfirmed[0].(map[string]any)
	if it["title"] != "SDK更新" {
		t.Errorf("unconfirmedItems[0].title = %v, want SDK更新", it["title"])
	}
	// **item ID は埋めない。** 埋めると、在りもしない item を更新先として送れる。
	if it["itemId"] != "" {
		t.Errorf("unconfirmedItems[0].itemId = %v, want 空文字", it["itemId"])
	}
	if it["confirmed"] != false {
		t.Errorf("unconfirmedItems[0].confirmed = %v, want false", it["confirmed"])
	}

	// **状態は created のまま。** uncreated に戻すと、作り直しで重複する
	// （ADR 0009 / 0056）。
	if got[0]["state"] != "created" {
		t.Errorf("state = %v, want created", got[0]["state"])
	}
}

// 実行記録があってハッシュが一致すれば created、ボードを変えれば changed。
func TestListAnnotations_CreatedThenChanged(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	id := createBoard(t, r, "ボード")
	base := saveAnnotatedScene(t, r, id)

	// 現在のハッシュを API 経由では取れないので、いったん状態を引いてから
	// 同じ内容で run を記録する。ハッシュ自体は domain 側のテストで担保済み。
	states := listAnnotations(t, r, id).Annotations
	if states[0]["state"] != "uncreated" {
		t.Fatalf("前提が崩れている: %v", states[0])
	}

	hash := currentHash(t, r, id)
	parent := "e1"
	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID:      id,
		AnnotationID: "annot-1",
		ContentHash:  hash,
		CreatedAt:    fixedTime,
		Outcome:      port.OutcomeComplete,
		Items: []port.SyncItem{
			{ItemID: "PVTI_e1", Kind: port.KindEpic, Title: "決済API", Body: "決済まわりの入口", LocalID: "e1", Action: port.ActionCreated, Confirmed: true, CreatedAt: fixedTime},
			{ItemID: "PVTI_i1", Kind: port.KindIssue, Title: "SDK更新", Body: "SDK の更新内容", LocalID: "i1", ParentLocalID: &parent, Action: port.ActionCreated, Confirmed: true, CreatedAt: fixedTime},
		},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	got := listAnnotations(t, r, id).Annotations
	if got[0]["state"] != "created" {
		t.Fatalf("state = %v, want created", got[0]["state"])
	}
	if _, ok := got[0]["lastSyncedAt"]; !ok {
		t.Error("lastSyncedAt が返っていない")
	}
	items, _ := got[0]["items"].([]any)
	if len(items) != 2 {
		// 以降で添字を引くので、ここで止める。
		t.Fatalf("items = %d 件, want 2", len(items))
	}
	// 何を作ったのかを追えるのは etoki の記録だけ。逆方向同期は実装しないので
	// GitHub からは取り直せない（ADR 0023）。epic と issue の両方を見る。
	for i, want := range []string{"決済まわりの入口", "SDK の更新内容"} {
		it, _ := items[i].(map[string]any)
		if it["body"] != want {
			t.Errorf("items[%d].body = %v, want %v", i, it["body"], want)
		}
	}

	// 付箋の文言を変えると changed になる。基準は 1 回目の保存が返した版。
	// 時計は止めてあるが版は進んでいるので、fixedTime を送り直すと 409 になる。
	changed := `{"type":"excalidraw","elements":[
		{"id":"annot-1","type":"frame","name":"決済まわり","customData":{"etoki":{"granularity":"epic"}}},
		{"id":"t1","type":"text","text":"Stripe の SDK が古い（急ぎ）","frameId":"annot-1"}]}`
	if rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		saveSceneBody(changed, base)); rec.Code != http.StatusOK {
		t.Fatalf("save scene: %d %s", rec.Code, rec.Body)
	}

	got = listAnnotations(t, r, id).Annotations
	if got[0]["state"] != "changed" {
		t.Errorf("state = %v, want changed", got[0]["state"])
	}
	// 「変更あり」でも前回の作成物は見せ続ける。開発者が判断するのに要る。
	if items, _ := got[0]["items"].([]any); len(items) != 2 {
		t.Errorf("changed でも前回の items を返すべき: %d 件", len(items))
	}
}
