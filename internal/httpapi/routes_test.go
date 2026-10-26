package httpapi

import (
	"bufio"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// route は 1 本の口。パスの引数は契約の書き方（`{id}`）に揃える。
type route struct{ method, path string }

func (r route) String() string { return r.method + " " + r.path }

// outsideContract は契約に載せない口のメソッドと、載せない理由。
//
// **メソッドまで書く。** パスだけで外すと、同じパスに別の用途の口（たとえば
// `POST /oauth/authorize`）を足したとき、それも黙って照合から外れる。
type outsideContract struct {
	methods []string
	reason  string
}

// anyMethods は `r.Any` が生やすメソッド（gin の routergroup.go の anyMethods）。
// gin が増やしたら、増えた分が契約との照合に回って落ちるので気づける。
var anyMethods = []string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
	http.MethodHead, http.MethodOptions, http.MethodDelete, http.MethodConnect,
	http.MethodTrace,
}

// routesOutsideContract は、ルーターにあって契約（api/openapi.yaml）に載せない口。
// **ここに無い口は、両方に無ければならない。**
//
// 足すときは理由を書く。理由が書けない口は、契約に載せる側の口。
var routesOutsideContract = map[string]outsideContract{
	mcpPath: {anyMethods, "MCP の入口（ADR 0071）。SDK が JSON-RPC で返し、画面の契約ではない"},
	wellKnownResourcePath: {[]string{http.MethodGet}, "RFC 9728 の形で返す" +
		"（.claude/rules/http-handlers.md の「OAuth の口は RFC の形で返す」）"},
	wellKnownResourcePath + mcpPath: {[]string{http.MethodGet}, "同上（`/mcp` に向けた resource metadata）"},
	wellKnownAuthorization:          {[]string{http.MethodGet}, "RFC 8414 の形で返す（同上）"},
	oauthAuthorizePath:              {[]string{http.MethodGet}, "RFC 6749 の形で返す（同上）。同意の画面へ転送するだけ"},
	oauthTokenPath:                  {[]string{http.MethodPost}, "RFC 6749 の形で返す（同上）"},
	oauthRegisterPath:               {[]string{http.MethodPost}, "RFC 7591 の形で返す（同上）"},
}

// ルーターの口と契約のパス・メソッドが一致していること（#273 の H1、#354）。
//
// **ルーティングは生成しない**（ADR 0011）。この判断は変えない。困るのはずれても
// 何も落ちないことで、列挙の語彙（TestVocabulary_MatchesContract、#275）と同じく、
// 両方の側から数え直して突き合わせる。手で並べた一覧と比べると、両方に同じ
// 書き忘れをしたときに緑のまま通る（test-effectiveness.md）。
//
// 見るのはパスとメソッドまで。何を返すかは生成型（apitypes）が型で縛っている。
func TestRoutes_MatchContract(t *testing.T) {
	t.Parallel()

	contract := contractRoutes(t)

	// 依存が nil でも口は同じだけ生える（未設定は 503 を返すだけ、ADR 0008）。
	var routed []route
	outside := map[route]bool{}
	for _, ri := range NewRouter(Deps{}).Routes() {
		if o, ok := routesOutsideContract[ri.Path]; ok && slices.Contains(o.methods, ri.Method) {
			outside[route{ri.Method, ri.Path}] = true
			continue
		}
		routed = append(routed, route{ri.Method, ginPathToContract(ri.Path)})
	}

	for _, r := range routed {
		if !slices.Contains(contract, r) {
			t.Errorf("%s はルーターにあるが契約に無い", r)
		}
	}
	for _, r := range contract {
		if !slices.Contains(routed, r) {
			t.Errorf("%s は契約にあるがルーターに無い", r)
		}
	}

	// 除外の表が古くなっていないこと。消した口が表に残ると、同じパスで契約に
	// 載せるべき口を足したときに黙って素通りする。
	for path, o := range routesOutsideContract {
		for _, m := range o.methods {
			if r := (route{m, path}); !outside[r] {
				t.Errorf("routesOutsideContract の %s はルーターに無い", r)
			}
		}
	}
}

// ginParam はルーターのパスの引数（`:id`）。
var ginParam = regexp.MustCompile(`:(\w+)`)

// ginPathToContract はルーターのパスを契約の書き方に揃える（`:id` → `{id}`）。
func ginPathToContract(p string) string {
	return ginParam.ReplaceAllString(p, "{$1}")
}

// 契約のパスとメソッドの行。api/openapi.yaml は prettier が整形するので、パスは
// paths の下に 2 つ、メソッドはその下に 4 つ字下げされている。
//
// **行末のコメントは許す**（`post: # note`）。許さないと、その口だけが契約から
// 黙って抜け、ルーターにも無ければ両方向の照合をすり抜ける。
//
// 鍵の形だけを見る `*Key` は、読めなかった行を落とすためにある。パスの字下げに
// あるのはパスの鍵だけで、メソッドの名前を鍵に持つ行はメソッドの行だけ。そこに
// 当たって読めない行は、契約から口が抜けている。
var (
	contractPath      = regexp.MustCompile(`^  (/\S*):\s*(?:#.*)?$`)
	contractPathKey   = regexp.MustCompile(`^  [^\s#]`)
	contractMethod    = regexp.MustCompile(`^    (get|put|post|delete|patch|head|options|trace):\s*(?:#.*)?$`)
	contractMethodKey = regexp.MustCompile(`^    (get|put|post|delete|patch|head|options|trace):`)
)

// contractRoutes は api/openapi.yaml の paths からパスとメソッドを読む。
//
// **YAML の読み手を依存に足さない。** 動かすものの依存だけを go.mod に置く
// （ADR 0011 が oapi-codegen を go.mod に入れなかったのと同じ）。代わりに、整形が
// 変わって読めなくなったら 0 本になるので、そこで落とす。
func contractRoutes(t *testing.T) []route {
	t.Helper()

	f, err := os.Open(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("open openapi.yaml: %v", err)
	}
	defer func() { _ = f.Close() }()

	var (
		out     []route
		inPaths bool
		path    string
		perPath = map[string]int{}
	)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "paths:":
			inPaths = true
		case !inPaths:
		case strings.HasPrefix(strings.TrimLeft(line, " "), "#"):
			// コメントだけの行。字下げなしでも paths を閉じない。
		case line != "" && !strings.HasPrefix(line, " "):
			// 次の最上位のキー（components など）。
			inPaths = false
		case contractPath.MatchString(line):
			path = contractPath.FindStringSubmatch(line)[1]
			perPath[path] = 0
		case contractPathKey.MatchString(line):
			t.Errorf("openapi.yaml のパスの行を読めなかった: %q", line)
			// 下に続くメソッドを、1 つ前のパスに付けない。
			path = ""
		case contractMethod.MatchString(line):
			if path == "" {
				break // 読めなかったパスの下。パスの行で落としてある。
			}
			method := strings.ToUpper(contractMethod.FindStringSubmatch(line)[1])
			out = append(out, route{method, path})
			perPath[path]++
		case contractMethodKey.MatchString(line):
			t.Errorf("openapi.yaml のメソッドの行を読めなかった: %q", line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}

	if len(out) == 0 {
		t.Fatal("openapi.yaml から口を 1 つも読めなかった（整形が変わった？）")
	}
	for p, n := range perPath {
		if n == 0 {
			t.Errorf("openapi.yaml の %s からメソッドを読めなかった（整形が変わった？）", p)
		}
	}
	return out
}
