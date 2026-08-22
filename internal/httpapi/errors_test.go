package httpapi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// wantMapping は sentinel 1 つに期待する写し先。
type wantMapping struct {
	err    error
	status int
	code   apitypes.ErrorCode
}

// wantMappings は「この sentinel はこのステータスとこの code になる」を固定する。
//
// **鍵は Go 上の名前。** 下の TestErrorMappings_CoverAllSentinels が、
// usecase と port のソースから sentinel を数え直してこの鍵と突き合わせる。
// sentinel を足して写し替えを書き忘れると、そこで落ちる。黙って 500 に落ちる
// 経路を残さないための仕掛けなので、面倒でも名前で持つ。
var wantMappings = map[string]wantMapping{
	"usecase.ErrInvalidInput": {
		usecase.ErrInvalidInput, http.StatusBadRequest, apitypes.ErrorCodeInvalidInput},

	"usecase.ErrBoardNotFound": {
		usecase.ErrBoardNotFound, http.StatusNotFound, apitypes.ErrorCodeNotFound},
	"usecase.ErrAnnotationNotFound": {
		usecase.ErrAnnotationNotFound, http.StatusNotFound, apitypes.ErrorCodeNotFound},
	"port.ErrNotFound": {
		port.ErrNotFound, http.StatusNotFound, apitypes.ErrorCodeNotFound},

	// 403 の 2 層。畳むと直す場所の違いが画面から消える（ADR 0017）。
	"usecase.ErrForbidden": {
		usecase.ErrForbidden, http.StatusForbidden, apitypes.ErrorCodeForbiddenRole},
	"port.ErrForbidden": {
		port.ErrForbidden, http.StatusForbidden, apitypes.ErrorCodeForbiddenProject},

	"port.ErrNotAuthenticated": {
		port.ErrNotAuthenticated, http.StatusUnauthorized, apitypes.ErrorCodeLoginRequired},

	// 409 は 6 つある。ステータスだけでは打ち手が決まらない代表。
	"usecase.ErrSceneConflict": {
		usecase.ErrSceneConflict, http.StatusConflict, apitypes.ErrorCodeSceneConflict},
	"usecase.ErrTargetLocked": {
		usecase.ErrTargetLocked, http.StatusConflict, apitypes.ErrorCodeTargetLocked},
	"usecase.ErrContentHashMismatch": {
		usecase.ErrContentHashMismatch, http.StatusConflict, apitypes.ErrorCodeContentHashMismatch},
	"usecase.ErrPreviousItemUnknown": {
		usecase.ErrPreviousItemUnknown, http.StatusConflict, apitypes.ErrorCodePreviousItemUnknown},
	"usecase.ErrAlreadyMember": {
		usecase.ErrAlreadyMember, http.StatusConflict, apitypes.ErrorCodeAlreadyMember},
	"usecase.ErrLastOwner": {
		usecase.ErrLastOwner, http.StatusConflict, apitypes.ErrorCodeLastOwner},

	"usecase.ErrTargetNotSelected": {
		usecase.ErrTargetNotSelected, http.StatusUnprocessableEntity, apitypes.ErrorCodeTargetNotSelected},
	"usecase.ErrProjectFieldMissing": {
		usecase.ErrProjectFieldMissing, http.StatusUnprocessableEntity, apitypes.ErrorCodeProjectFieldMissing},

	"usecase.ErrLLMUnavailable": {
		usecase.ErrLLMUnavailable, http.StatusBadGateway, apitypes.ErrorCodeLlmUnavailable},
	"usecase.ErrInterpretationFailed": {
		usecase.ErrInterpretationFailed, http.StatusBadGateway, apitypes.ErrorCodeInterpretationFailed},
	"usecase.ErrCreationIncomplete": {
		usecase.ErrCreationIncomplete, http.StatusBadGateway, apitypes.ErrorCodeCreationIncomplete},
}

// notMapped は境界に出てこないので表に載せない sentinel。
//
// **どちらもユースケース層が自分のエラーに写し替えて返す**ので、ハンドラまで
// 届かない。届くようになったら表に足す判断が要る。
var notMapped = map[string]string{
	"port.ErrConflict":      "usecase.ErrSceneConflict に写して返す（ADR 0020）",
	"port.ErrAlreadyExists": "usecase.ErrAlreadyMember に写して返す",
}

func TestErrorMappings(t *testing.T) {
	t.Parallel()

	for name, want := range wantMappings {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// 実際に届くのは包まれたエラー。errors.Is で引けることまで見る。
			err := fmt.Errorf("%w: board-1", want.err)

			got, ok := lookupError(err)
			if !ok {
				t.Fatalf("%s が表に無い。表に無い sentinel は黙って 500 に落ちる", name)
			}
			if got.status != want.status {
				t.Errorf("status = %d, want %d", got.status, want.status)
			}
			if got.code != want.code {
				t.Errorf("code = %q, want %q", got.code, want.code)
			}
		})
	}
}

// 404 だけは本文に err を載せない。非メンバーのボードもここに来るので、本文の
// 違いから存在を読み取れるようにしない（ADR 0016 / 0017）。
func TestHintFor_HidesNotFoundDetail(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: board-1", usecase.ErrBoardNotFound)
	m, ok := lookupError(err)
	if !ok {
		t.Fatal("ErrBoardNotFound が表に無い")
	}

	if hint := hintFor(m, err); hint != "not found" {
		t.Errorf("hint = %q, want %q", hint, "not found")
	}
}

// 表に無いエラーは既定に落ちる。fail が 500 を返す前提を固定する。
func TestLookupError_UnknownError(t *testing.T) {
	t.Parallel()

	if _, ok := lookupError(fmt.Errorf("db: boom")); ok {
		t.Error("見知らぬエラーが表に引っかかった")
	}
}

// 表が sentinel を取りこぼしていないことを、ソースから数え直して確かめる。
//
// **これが無いと、sentinel を足したときに写し替えを書き忘れても緑のまま通る。**
// 忘れた経路だけが 500 に落ち、画面は「原因不明」としか言えなくなる。
// 意図して載せないものは notMapped に理由つきで書く。
func TestErrorMappings_CoverAllSentinels(t *testing.T) {
	t.Parallel()

	for _, dir := range []struct{ pkg, path string }{
		{"usecase", filepath.Join("..", "usecase")},
		{"port", filepath.Join("..", "..", "port")},
	} {
		for _, name := range sentinelNames(t, dir.pkg, dir.path) {
			if _, ok := wantMappings[name]; ok {
				continue
			}
			if reason, ok := notMapped[name]; ok {
				t.Logf("%s は表に載せない: %s", name, reason)
				continue
			}
			t.Errorf("%s が写し替えの表にない。"+
				"errors.go に足すか、載せない理由を notMapped に書く", name)
		}
	}
}

// sentinelNames はパッケージのソースから、公開された Err* 変数の名前を集める。
func sentinelNames(t *testing.T, pkg, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") ||
			strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		file, err := parser.ParseFile(
			token.NewFileSet(), filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}

		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, ident := range value.Names {
					if strings.HasPrefix(ident.Name, "Err") {
						names = append(names, pkg+"."+ident.Name)
					}
				}
			}
		}
	}

	return names
}
