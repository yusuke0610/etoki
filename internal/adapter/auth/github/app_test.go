package github_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	githubauth "github.com/yusuke0610/etoki/internal/adapter/auth/github"
	"github.com/yusuke0610/etoki/port"
)

const (
	testClientID     = "Iv1.testclientid"
	testClientSecret = "secret_must_not_leak"
)

// capturedRequest は 1 回分のリクエスト。
type capturedRequest struct {
	Path   string
	Header http.Header
	Form   url.Values
}

// newProvider は決められた応答を返すサーバーに向いた Provider を返す。
//
// path ごとに応答を差し替える。トークン交換と利用者取得でホストが違うので、
// 1 つのサーバーで両方を受ける。
func newProvider(t *testing.T, responses map[string]string) (*githubauth.Provider, *[]capturedRequest) {
	t.Helper()

	var got []capturedRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := capturedRequest{Path: r.URL.Path, Header: r.Header.Clone()}
		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			req.Form, _ = url.ParseQuery(string(raw))
		}
		got = append(got, req)

		body, ok := responses[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	p, err := githubauth.New(githubauth.Config{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		AuthBaseURL:  srv.URL,
		APIBaseURL:   srv.URL,
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	return p, &got
}

func TestNew_RequiresClientCredentials(t *testing.T) {
	t.Parallel()

	for name, cfg := range map[string]githubauth.Config{
		"どちらも無い":       {},
		"secret が無い":   {ClientID: testClientID},
		"clientID が無い": {ClientSecret: testClientSecret},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := githubauth.New(cfg); !errors.Is(err, githubauth.ErrClientCredentials) {
				t.Fatalf("New() = %v, want ErrClientCredentials", err)
			}
		})
	}
}

// 片方だけ設定した状態を「使うつもり」とみなす。使わないつもりなら両方空になる。
func TestConfig_Configured(t *testing.T) {
	t.Parallel()

	if (githubauth.Config{}).Configured() {
		t.Error("空の Config が Configured")
	}
	if !(githubauth.Config{ClientID: "x"}).Configured() {
		t.Error("片方だけの Config が Configured でない")
	}
}

// GitHub App は scope を要求しない。権限は App の設定で決まる。
func TestAuthorizeURL(t *testing.T) {
	t.Parallel()

	p, _ := newProvider(t, nil)

	raw := p.AuthorizeURL("state-1", "http://127.0.0.1:5173/api/auth/callback")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	q := u.Query()
	if q.Get("client_id") != testClientID {
		t.Errorf("client_id = %q", q.Get("client_id"))
	}
	if q.Get("state") != "state-1" {
		t.Errorf("state = %q", q.Get("state"))
	}
	if q.Get("redirect_uri") != "http://127.0.0.1:5173/api/auth/callback" {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Has("scope") {
		t.Error("scope を要求している。GitHub App の権限は App の設定で決まる")
	}
	if !strings.HasSuffix(u.Path, "/login/oauth/authorize") {
		t.Errorf("path = %q", u.Path)
	}
}

func TestExchange(t *testing.T) {
	t.Parallel()

	p, got := newProvider(t, map[string]string{
		"/login/oauth/access_token": `{"access_token":"ghu_1","refresh_token":"ghr_1",
			"expires_in":28800,"refresh_token_expires_in":15811200,"token_type":"bearer"}`,
		"/user": `{"id":583231,"login":"octocat","name":"The Octocat"}`,
	})

	id, creds, err := p.Exchange(t.Context(), "code-1", "http://127.0.0.1/api/auth/callback")
	if err != nil {
		t.Fatalf("Exchange() = %v", err)
	}

	// login は改名で変わる。同定に使えるのは id だけ。
	if id.Subject != "583231" {
		t.Errorf("Subject = %q, want 583231", id.Subject)
	}
	if id.Login != "octocat" || id.DisplayName != "The Octocat" {
		t.Errorf("Identity = %+v", id)
	}
	if id.Provider != githubauth.ProviderName {
		t.Errorf("Provider = %q, want %q", id.Provider, githubauth.ProviderName)
	}

	if creds.AccessToken != "ghu_1" || creds.RefreshToken != "ghr_1" {
		t.Errorf("Credentials = %+v", creds)
	}
	if !creds.Expiring() {
		t.Error("失効する資格情報なのに Expiring() が false")
	}
	// 8 時間ちょうどを期待すると時計に依存する。幅で見る。
	if d := time.Until(creds.ExpiresAt); d < 7*time.Hour || d > 9*time.Hour {
		t.Errorf("ExpiresAt までの残り = %v, want 約 8 時間", d)
	}

	form := (*got)[0].Form
	if form.Get("code") != "code-1" || form.Get("client_secret") != testClientSecret {
		t.Errorf("トークン交換のフォーム = %v", form)
	}
	if auth := (*got)[1].Header.Get("authorization"); auth != "Bearer ghu_1" {
		t.Errorf("利用者取得の authorization = %q", auth)
	}
}

// 名前を設定していない利用者は login を表示名にする。空欄にしない。
func TestExchange_FallsBackToLogin(t *testing.T) {
	t.Parallel()

	p, _ := newProvider(t, map[string]string{
		"/login/oauth/access_token": `{"access_token":"ghu_1"}`,
		"/user":                     `{"id":1,"login":"octocat","name":""}`,
	})

	id, _, err := p.Exchange(t.Context(), "code-1", "")
	if err != nil {
		t.Fatalf("Exchange() = %v", err)
	}
	if id.DisplayName != "octocat" {
		t.Errorf("DisplayName = %q, want octocat", id.DisplayName)
	}
}

// App 側で「Expire user authorization tokens」を切ると expires_in が返らない。
func TestExchange_HandlesNonExpiringTokens(t *testing.T) {
	t.Parallel()

	p, _ := newProvider(t, map[string]string{
		"/login/oauth/access_token": `{"access_token":"ghu_forever","token_type":"bearer"}`,
		"/user":                     `{"id":1,"login":"octocat"}`,
	})

	_, creds, err := p.Exchange(t.Context(), "code-1", "")
	if err != nil {
		t.Fatalf("Exchange() = %v", err)
	}
	if creds.Expiring() {
		t.Error("失効しない資格情報が Expiring() を返している")
	}
	if !creds.ExpiresAt.IsZero() || creds.RefreshToken != "" {
		t.Errorf("Credentials = %+v, want 期限も refresh も無し", creds)
	}
}

// 失敗も HTTP 200 で返る。ステータスコードだけを見ると、空のトークンで進む。
func TestExchange_RejectsErrorBodyWith200(t *testing.T) {
	t.Parallel()

	p, _ := newProvider(t, map[string]string{
		"/login/oauth/access_token": `{"error":"incorrect_client_credentials",
			"error_description":"The client_id and/or client_secret passed are incorrect."}`,
	})

	_, _, err := p.Exchange(t.Context(), "code-1", "")
	if err == nil {
		t.Fatal("Exchange() = nil, want error")
	}
	if !strings.Contains(err.Error(), "incorrect_client_credentials") {
		t.Errorf("エラーに理由が含まれない: %v", err)
	}
}

// 使い切った code / refresh token は再ログインで直る。sentinel に寄せて
// UI が案内できるようにする。
func TestExchange_MapsRecoverableErrorsToSentinel(t *testing.T) {
	t.Parallel()

	p, _ := newProvider(t, map[string]string{
		"/login/oauth/access_token": `{"error":"bad_verification_code"}`,
	})

	if _, _, err := p.Exchange(t.Context(), "code-1", ""); !errors.Is(err, port.ErrNotAuthenticated) {
		t.Fatalf("Exchange() = %v, want ErrNotAuthenticated", err)
	}
}

func TestExchange_RequiresCode(t *testing.T) {
	t.Parallel()

	p, got := newProvider(t, nil)

	if _, _, err := p.Exchange(t.Context(), "", ""); err == nil {
		t.Fatal("Exchange() = nil, want error")
	}
	if len(*got) != 0 {
		t.Errorf("code が空なのにリクエストを送っている: %d 回", len(*got))
	}
}

func TestRefresh(t *testing.T) {
	t.Parallel()

	p, got := newProvider(t, map[string]string{
		"/login/oauth/access_token": `{"access_token":"ghu_2","refresh_token":"ghr_2",
			"expires_in":28800,"refresh_token_expires_in":15811200}`,
	})

	creds, err := p.Refresh(t.Context(), "ghr_1")
	if err != nil {
		t.Fatalf("Refresh() = %v", err)
	}
	if creds.AccessToken != "ghu_2" || creds.RefreshToken != "ghr_2" {
		t.Errorf("Credentials = %+v", creds)
	}

	form := (*got)[0].Form
	if form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "ghr_1" {
		t.Errorf("更新のフォーム = %v", form)
	}
}

func TestRefresh_RejectsExpiredRefreshToken(t *testing.T) {
	t.Parallel()

	p, _ := newProvider(t, map[string]string{
		"/login/oauth/access_token": `{"error":"bad_refresh_token"}`,
	})

	if _, err := p.Refresh(t.Context(), "ghr_dead"); !errors.Is(err, port.ErrNotAuthenticated) {
		t.Fatalf("Refresh() = %v, want ErrNotAuthenticated", err)
	}
}

func TestRefresh_RequiresToken(t *testing.T) {
	t.Parallel()

	p, got := newProvider(t, nil)

	if _, err := p.Refresh(t.Context(), ""); !errors.Is(err, port.ErrNotAuthenticated) {
		t.Fatalf("Refresh() = %v, want ErrNotAuthenticated", err)
	}
	if len(*got) != 0 {
		t.Errorf("refresh token が空なのにリクエストを送っている: %d 回", len(*got))
	}
}

// client_secret がエラーに混ざると、ログや画面に出た時点で漏れる。
func TestDoesNotLeakClientSecret(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		// 相手が送り返してくる可能性まで含めて確かめる。
		_, _ = io.WriteString(w, `{"message":"boom `+testClientSecret+`"}`)
	}))
	t.Cleanup(srv.Close)

	p, err := githubauth.New(githubauth.Config{
		ClientID:     testClientID,
		ClientSecret: testClientSecret,
		AuthBaseURL:  srv.URL,
		APIBaseURL:   srv.URL,
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	_, _, exchangeErr := p.Exchange(t.Context(), "code-1", "")
	if exchangeErr == nil {
		t.Fatal("Exchange() = nil, want error")
	}
	if strings.Contains(exchangeErr.Error(), testClientSecret) {
		t.Errorf("client secret が漏れている: %v", exchangeErr)
	}
}
