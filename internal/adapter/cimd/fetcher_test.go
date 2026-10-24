package cimd

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const doc = `{"client_id":"%s","client_name":"Example","redirect_uris":["http://127.0.0.1/cb"]}`

// newTestServer は文書を返す TLS のサーバーと、そこへ繋げる Fetcher を作る。
//
// httptest のサーバーはループバックで待つので、行き先の判定を外した Fetcher で
// 繋ぐ。**判定そのものは TestFetch_RefusesLoopback と TestIsBlocked が見る。**
func newTestServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *Fetcher, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	f := newFetcher(func(netip.Addr) bool { return false },
		&http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}})
	return srv, f, &hits
}

func serveDoc(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(strings.Replace(doc, "%s", "https://"+r.Host+r.URL.Path, 1)))
}

func TestFetch_ReadsDocument(t *testing.T) {
	t.Parallel()
	srv, f, _ := newTestServer(t, serveDoc)

	id := srv.URL + "/oauth/meta.json"
	got, err := f.Fetch(t.Context(), id)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.ClientID != id || got.ClientName != "Example" ||
		len(got.RedirectURIs) != 1 || got.RedirectURIs[0] != "http://127.0.0.1/cb" {
		t.Errorf("Fetch = %+v", got)
	}
}

// 既定の判定では、ループバックへは繋がない。**相手に 1 度も届かない**ことまで見る。
func TestFetch_RefusesLoopback(t *testing.T) {
	t.Parallel()
	srv, testFetcher, hits := newTestServer(t, serveDoc)

	f := New(slog.New(slog.DiscardHandler))
	// TLS の検証で落ちたのではなく、繋ぐ前に止めたことを確かめるため、
	// テスト用の Fetcher と同じ信頼の設定を渡す。
	f.client.Transport.(*http.Transport).TLSClientConfig =
		testFetcher.client.Transport.(*http.Transport).TLSClientConfig

	for _, id := range []string{
		srv.URL + "/meta.json",
		strings.Replace(srv.URL, "127.0.0.1", "localhost", 1) + "/meta.json",
	} {
		_, err := f.Fetch(t.Context(), id)
		if !errors.Is(err, errBlockedAddress) {
			t.Errorf("%s: err = %v, want errBlockedAddress", id, err)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("サーバーに %d 回届いた", n)
	}
}

// 繋げなかった理由は、エラーの文に載せずログにだけ残す（#339）。この文は同意の
// 画面の応答に載る。**弾いた宛先と繋がらなかった宛先を同じ文にする**ことまで見る。
// 分けると、名前が内部のアドレスに解決されるかが応答から読める。
func TestFetch_UnreachableHidesConnectionDetail(t *testing.T) {
	t.Parallel()

	srv, testFetcher, _ := newTestServer(t, serveDoc)
	// 名前で名乗らせる。解決したアドレス（127.0.0.1 / ::1）が文に出ないことを見たいので、
	// client_id そのものにアドレスを含めない。
	id := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1) + "/meta.json"

	var blockedLog bytes.Buffer
	blocked := New(slog.New(slog.NewTextHandler(&blockedLog, nil)))
	blocked.client.Transport.(*http.Transport).TLSClientConfig =
		testFetcher.client.Transport.(*http.Transport).TLSClientConfig

	// 判定は外したまま、相手を止めて繋がらなくする。
	var refusedLog bytes.Buffer
	refused := testFetcher
	refused.logger = slog.New(slog.NewTextHandler(&refusedLog, nil))
	srv.Close()

	for name, tc := range map[string]struct {
		f   *Fetcher
		log *bytes.Buffer
	}{
		"弾いた宛先":     {blocked, &blockedLog},
		"繋がらなかった宛先": {refused, &refusedLog},
	} {
		_, err := tc.f.Fetch(t.Context(), id)
		if err == nil {
			t.Fatalf("%s: err = nil", name)
		}
		if got, want := err.Error(), "cimd: could not reach "+id; got != want {
			t.Errorf("%s: err = %q, want %q", name, got, want)
		}
		if !strings.Contains(tc.log.String(), "client_id="+id) {
			t.Errorf("%s: ログに client_id が無い: %s", name, tc.log)
		}
		// 詳細はログに残す。運用者が原因を追えなくならないように。
		if !strings.Contains(tc.log.String(), "dial tcp") {
			t.Errorf("%s: ログに接続の詳細が無い: %s", name, tc.log)
		}
	}
	// 弾いたことは、文を変えても辿れる。
	if _, err := blocked.Fetch(t.Context(), id); !errors.Is(err, errBlockedAddress) {
		t.Errorf("err = %v, want errBlockedAddress", err)
	}
}

// 本文を読んでいる途中で切れたときも、理由はログにだけ残す（#339）。読み込みの
// 失敗の文は、etoki 側のアドレス（`read tcp <こちら>-><相手>`）を含みうる。
func TestFetch_ReadFailureHidesDetail(t *testing.T) {
	t.Parallel()

	srv, f, _ := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		// 名乗った長さより短く書いて切る。
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte(`{"client_id":`))
	})
	var log bytes.Buffer
	f.logger = slog.New(slog.NewTextHandler(&log, nil))

	id := srv.URL + "/meta.json"
	_, err := f.Fetch(t.Context(), id)
	if err == nil {
		t.Fatal("err = nil")
	}
	if got, want := err.Error(), "cimd: could not read "+id; got != want {
		t.Errorf("err = %q, want %q", got, want)
	}
	if !strings.Contains(log.String(), "unexpected EOF") {
		t.Errorf("ログに読み込みの失敗が無い: %s", log.String())
	}
}

func TestIsBlocked(t *testing.T) {
	t.Parallel()

	for addr, want := range map[string]bool{
		"127.0.0.1":        true,
		"::1":              true,
		"10.1.2.3":         true,
		"172.16.0.1":       true,
		"192.168.1.1":      true,
		"169.254.169.254":  true, // クラウドのメタデータ
		"fe80::1":          true,
		"fc00::1":          true,
		"100.64.0.1":       true,
		"0.0.0.0":          true,
		"::":               true,
		"224.0.0.1":        true,
		"::ffff:127.0.0.1": true,
		"::ffff:10.0.0.1":  true,
		"64:ff9b::a00:1":   true,
		"64:ff9b:1::a00:1": true, // ローカル用の NAT64（RFC 8215）
		"2002:a00:1::":     true, // 6to4 で 10.0.0.1 を指す
		"8.8.8.8":          false,
		"1.1.1.1":          false,
		"2001:4860::8888":  false,
	} {
		if got := isBlocked(netip.MustParseAddr(addr)); got != want {
			t.Errorf("isBlocked(%s) = %v, want %v", addr, got, want)
		}
	}
}

// リダイレクトは追わない。追うと、公開アドレスで受けて内側へ送り直せる。
func TestFetch_DoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var followed atomic.Bool
	srv, f, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			followed.Store(true)
			serveDoc(w, r)
			return
		}
		http.Redirect(w, r, "/target", http.StatusFound)
	})

	if _, err := f.Fetch(t.Context(), srv.URL+"/meta.json"); err == nil {
		t.Error("リダイレクトで err = nil")
	}
	if followed.Load() {
		t.Error("リダイレクトを追った")
	}
}

// 上限ちょうどは通し、1 バイト超えたら断る。
func TestFetch_RejectsLargeDocument(t *testing.T) {
	t.Parallel()

	// `{"client_name":"` と `"}` で 18 バイト。
	for size, wantOK := range map[int]bool{maxDocumentBytes: true, maxDocumentBytes + 1: false} {
		srv, f, _ := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"client_name":"` + strings.Repeat("a", size-18) + `"}`))
		})
		_, err := f.Fetch(t.Context(), srv.URL+"/meta.json")
		if (err == nil) != wantOK {
			t.Errorf("%d バイト: err = %v, want ok %v", size, err, wantOK)
		}
	}
}

func TestFetch_Timeout(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	srv, f, _ := newTestServer(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	f.client.Timeout = 100 * time.Millisecond

	start := time.Now()
	if _, err := f.Fetch(t.Context(), srv.URL+"/meta.json"); err == nil {
		t.Error("応答しない相手で err = nil")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("打ち切りまで %s かかった", d)
	}
}

func TestFetch_CachesByMaxAge(t *testing.T) {
	t.Parallel()

	cases := []struct {
		header   string
		wantHits int32
	}{
		{"max-age=60", 1},
		{"public, max-age=60", 1},
		{"no-store", 2},
		{"no-cache, max-age=60", 2},
		{"", 2},
		{"max-age=0", 2},
	}
	for _, tc := range cases {
		t.Run(tc.header, func(t *testing.T) {
			t.Parallel()
			srv, f, hits := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.header != "" {
					w.Header().Set("Cache-Control", tc.header)
				}
				serveDoc(w, r)
			})
			for range 2 {
				if _, err := f.Fetch(t.Context(), srv.URL+"/meta.json"); err != nil {
					t.Fatalf("Fetch: %v", err)
				}
			}
			if got := hits.Load(); got != tc.wantHits {
				t.Errorf("届いた回数 = %d, want %d", got, tc.wantHits)
			}
		})
	}
}

// 長い max-age を返されても、上限より長くは持たない。
func TestFetch_CacheAgeIsCapped(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	srv, f, hits := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=86400")
		serveDoc(w, r)
	})
	f.now = func() time.Time { return now }

	id := srv.URL + "/meta.json"
	if _, err := f.Fetch(t.Context(), id); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	now = now.Add(maxCacheAge)
	if _, err := f.Fetch(t.Context(), id); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("届いた回数 = %d, want 2（上限を過ぎたら取り直す）", got)
	}
}

func TestFetch_RejectsNonHTTPSAndErrors(t *testing.T) {
	t.Parallel()

	srv, f, hits := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	})
	if _, err := f.Fetch(t.Context(), strings.Replace(srv.URL, "https", "http", 1)+"/m"); err == nil {
		t.Error("http で err = nil")
	}
	if hits.Load() != 0 {
		t.Error("http の URL を取りに行った")
	}
	if _, err := f.Fetch(t.Context(), srv.URL+"/m"); err == nil {
		t.Error("404 で err = nil")
	}
}

// プロキシを通すと、繋ぐ先はプロキシになり、行き先の判定が相手のアドレスを
// 見られなくなる。環境変数にプロキシがあっても使わないことを固定する。
func TestNew_DoesNotUseProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.example:3128")

	tr, ok := New(nil).client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T", New(nil).client.Transport)
	}
	if tr.Proxy != nil {
		t.Error("プロキシを使う設定になっている")
	}
}
