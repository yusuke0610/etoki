package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/yusuke0610/etoki/internal/httpapi"
	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
)

// 使える機能は Deps の nil から決まる。**押す前に見せるための値なので、
// エンドポイントの 503 と食い違ってはならない。** 判定材料が同じであることを、
// 組み合わせごとに固定する。
func TestGetCapabilities(t *testing.T) {
	t.Parallel()

	boards, mappings := newRepos(t)
	gh := &stubGitHub{}

	// 全部そろった構成。ここから 1 つずつ落とす。
	full := func() httpapi.Deps {
		return httpapi.Deps{
			Boards:      usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks()),
			Annotations: usecase.NewAnnotationService(boards, mappings),
			Interpretations: usecase.NewInterpretationService(
				boards, mappings, &stubLLM{text: validInterpretation}),
			Creations: usecase.NewCreationService(
				boards, mappings, gh, usecase.NewBoardLocks()),
			Catalog: usecase.NewGitHubCatalogService(gh),
			Members: usecase.NewBoardMemberService(boards, nil, usecase.NewBoardLocks()),
			Access:  usecase.NewBoardAccessService(boards, gh, nil),
		}
	}

	tests := []struct {
		name string
		// drop は full から依存を 1 つ落とす。
		drop func(*httpapi.Deps)
		want apitypes.Capabilities
	}{
		{
			name: "全部そろっている",
			drop: func(*httpapi.Deps) {},
			want: apitypes.Capabilities{Interpretation: true, Creation: true, Sharing: true},
		},
		{
			// README が言う「LLM を設定しなくても起動する」構成。ブレストと
			// 保存は使えて、解釈だけができない（ADR 0008）。
			name: "LLM が未設定",
			drop: func(d *httpapi.Deps) { d.Interpretations = nil },
			want: apitypes.Capabilities{Interpretation: false, Creation: true, Sharing: true},
		},
		{
			name: "GitHub が未設定",
			drop: func(d *httpapi.Deps) { d.Creations, d.Catalog = nil, nil },
			want: apitypes.Capabilities{Interpretation: true, Creation: false, Sharing: true},
		},
		{
			// 作成先の候補だけ引けない配線は cmd/etoki には無いが、Deps は
			// 別々に受ける。**片方でも欠けたら作れない**として返す。
			name: "作成先の候補だけ引けない",
			drop: func(d *httpapi.Deps) { d.Catalog = nil },
			want: apitypes.Capabilities{Interpretation: true, Creation: false, Sharing: true},
		},
		{
			// 認証を設定していない構成。共有する相手がいない（ADR 0016 / 0017）。
			name: "共有が組み立てられていない",
			drop: func(d *httpapi.Deps) { d.Members = nil },
			want: apitypes.Capabilities{Interpretation: true, Creation: true, Sharing: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			deps := full()
			tt.drop(&deps)

			rec := do(t, httpapi.NewRouter(deps), http.MethodGet, "/api/capabilities", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
			}

			if got := decode[apitypes.Capabilities](t, rec); got != tt.want {
				t.Errorf("capabilities = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// false を返した機能は、実際に叩くと 503 になる。**片方だけ直すと、押す前の
// 案内と押した後の応答が食い違う。**
func TestGetCapabilities_MatchesUnavailableEndpoints(t *testing.T) {
	t.Parallel()

	// LLM も GitHub も無い、いちばん素の構成。
	r, _ := newRouter(t)

	rec := do(t, r, http.MethodGet, "/api/capabilities", nil)
	caps := decode[apitypes.Capabilities](t, rec)
	if caps.Interpretation || caps.Creation {
		t.Fatalf("素の構成なのに使えることになっている: %+v", caps)
	}

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	for _, tt := range []struct {
		method string
		path   string
		code   apitypes.ErrorCode
	}{
		{http.MethodPost, interpretPath(id, "annot-1"), apitypes.ErrorCodeLlmNotConfigured},
		{http.MethodGet, "/api/github/repositories", apitypes.ErrorCodeGithubNotConfigured},
	} {
		t.Run(tt.path, func(t *testing.T) {
			rec := do(t, r, tt.method, tt.path, nil)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
			}
			if code := decode[map[string]string](t, rec)["code"]; code != string(tt.code) {
				t.Errorf("code = %q, want %q", code, tt.code)
			}
		})
	}
}
