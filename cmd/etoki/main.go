// Command etoki は etoki サーバーの起動とマイグレーション適用を行う。
//
// クラウド固有のアダプタが必要な場合、利用者はこの main を写して
// 独自の実装を差し込む。そのためここは配線だけに留め、ロジックを置かない。
package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki"
	githubauth "github.com/yusuke0610/etoki/internal/adapter/auth/github"
	"github.com/yusuke0610/etoki/internal/adapter/github"
	"github.com/yusuke0610/etoki/internal/adapter/llm"
	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/internal/secret"
	"github.com/yusuke0610/etoki/port"
)

// envEncryptionKey は保存するトークンを暗号化する鍵。
const envEncryptionKey = "ETOKI_TOKEN_ENCRYPTION_KEY"

// envPublicURL は認可から戻ってくる先の組み立てに使う。
const envPublicURL = "ETOKI_PUBLIC_URL"

// envWebDir はビルド済みフロントエンドの置き場所。未設定なら画面を配らない。
const envWebDir = "ETOKI_WEB_DIR"

// defaultDBPath は ETOKI_DB_PATH が未設定のときに使う SQLite ファイル。
const defaultDBPath = "etoki.db"

// usage は const ではなく var。実行の上限の既定値（ADR 0044）は数値と
// time.Duration なので、文字列にするのが定数式にならない。
var usage = `usage:
  etoki                 サーバーを起動する
  etoki migrate         マイグレーションを適用する
  etoki claim [--yes] <login>
                        所有者の無いボードを引き受ける。引き当てた相手を
                        見せて確かめる（--yes で省く）
  etoki grants [--login <login>]
                        MCP のクライアントに許した接続を一覧する
  etoki revoke [--yes] <grant-id>
  etoki revoke [--yes] --login <login>
                        接続を取り消す。--login はその利用者の接続をすべて。
                        取り消す前に対象を見せて確かめる（--yes で省く）

environment:
  ETOKI_ADDR            リッスンアドレス（既定: ` + etoki.DefaultAddr + `）
                        ループバックの外へ広げるなら先に認証を設定する
  ETOKI_ALLOWED_ORIGINS 追加で許すオリジン（カンマ区切り）。ループバックは常に許す
  ETOKI_DB_PATH         SQLite ファイルのパス（既定: ` + defaultDBPath + `）
  ETOKI_WEB_DIR         ビルド済みフロントエンドの置き場所（例: web/dist）
                        未設定なら画面を配らない（make dev では Vite が配る）
  ETOKI_LLM_BASE_URL    LLM のエンドポイント（既定: ` + llm.DefaultBaseURL + `）
                        鍵を設定しているなら、http はループバックだけ
  ETOKI_LLM_API_KEY     LLM の API キー（認証不要なら未設定でよい）
  ETOKI_LLM_MODEL       モデル ID（既定: ` + llm.DefaultModel + `）
  ETOKI_LLM_MAX_CONCURRENT     1 人が同時に走らせられる解釈・図の生成の数
                               （既定: ` + strconv.Itoa(etoki.DefaultLLMMaxConcurrent) + `）
  ETOKI_LLM_RATE_LIMIT         ETOKI_LLM_RATE_WINDOW のあいだに始められる回数
                               未設定なら無制限。単独で設定してよく、
                               そのとき窓は既定になる
  ETOKI_LLM_RATE_WINDOW        回数を数える窓（既定: ` + etoki.DefaultLLMRateWindow.String() + `）
                               単独では設定できない（回数の上限が要る）
  ETOKI_GITHUB_TOKEN    GitHub のトークン（repo の read と Projects の read/write）
                        認証を設定した場合は使わない
  ETOKI_GITHUB_BASE_URL GitHub API のルート（既定: ` + github.DefaultBaseURL + `）
                        /graphql を持つ先。http はループバックだけ。
                        GHES では確かめていない
  ETOKI_GITHUB_APP_CLIENT_ID      GitHub App の client ID（設定するとログインを要求する）
  ETOKI_GITHUB_APP_CLIENT_SECRET  同 client secret
  ETOKI_TOKEN_ENCRYPTION_KEY      トークンを暗号化する鍵（base64 の 32 バイト）
  ETOKI_PUBLIC_URL                認可から戻ってくる先。空ならリクエストの Host から組む
  ETOKI_GITHUB_KIND_FIELD    種別のカスタムフィールド名（既定: ` + etoki.DefaultKindFieldName + `）
  ETOKI_GITHUB_PARENT_FIELD  親のカスタムフィールド名（既定: ` + etoki.DefaultParentFieldName + `）

LLM や GitHub を未設定のままでも起動する。その場合、解釈や作成のエンドポイント
だけが「設定されていない」と返し、ボードの編集と状態表示は使える。

draft issue の作成先はボードごとに画面で選ぶ。環境変数では指定しない。

GitHub App を設定するとログインを要求し、GitHub は利用者ごとのトークンで叩く。
そのとき ETOKI_GITHUB_TOKEN は使わない。認証を設定しなければ従来どおり PAT で
動く。
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "etoki: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// **最初のシグナルで既定の扱いに戻す。** 止めるまでの猶予は作成の後始末を
	// 覆うので最長 75 秒あり（ADR 0056）、そのあいだ `Server.Run` は
	// 「press Ctrl-C again to stop now」と案内する。**`defer stop()` だけだと
	// その案内が嘘になる。** `run` が返るまで stop が呼ばれず、2 回目の
	// Ctrl-C もここが捕まえてしまうため（捕まえた先の ctx はもう
	// キャンセル済みなので、何も起きずに待たされる）。
	//
	// stop は ctx をキャンセルしないので、シグナルが来ないまま終わる経路の
	// ために done で抜ける。
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-done:
		}
	}()

	// サブコマンドは 1 つだけなので flag パッケージは使わない。増えたら見直す。
	args := os.Args[1:]
	switch {
	case len(args) == 0:
		return serve(ctx)
	case args[0] == "migrate":
		return migrate(ctx)
	case args[0] == "claim":
		login, yes, err := parseClaimArgs(args[1:])
		if err != nil {
			fmt.Fprint(os.Stderr, usage)
			return err
		}
		return claim(ctx, login, yes)
	case args[0] == "grants":
		login, err := parseGrantsArgs(args[1:])
		if err != nil {
			fmt.Fprint(os.Stderr, usage)
			return err
		}
		return listGrants(ctx, login)
	case args[0] == "revoke":
		target, err := parseRevokeArgs(args[1:])
		if err != nil {
			fmt.Fprint(os.Stderr, usage)
			return err
		}
		return revoke(ctx, target)
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func serve(ctx context.Context) error {
	// gin 既定のリクエストログは使わず slog に寄せるので、debug 出力も止める。
	gin.SetMode(gin.ReleaseMode)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	path := dbPath()

	db, err := sqlite.Open(ctx, path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// SQLite は接続時にファイルを作るため、未初期化でも起動できてしまう。
	// そのまま動かすと全 API が 500 を返し続けて原因が分かりにくいので、
	// ここで落とす。適用は明示的な操作にしておきたいので自動では行わない。
	if err := sqlite.EnsureMigrated(ctx, db); err != nil {
		if errors.Is(err, sqlite.ErrNotMigrated) {
			return fmt.Errorf("%w\n  先に `make migrate` を実行してください (%s)", err, path)
		}
		return err
	}

	llmClient, err := newLLMClient()
	if err != nil {
		return err
	}

	boards := sqlite.NewBoardRepository(db)

	auth, err := newAuthenticator(db)
	if err != nil {
		return err
	}

	githubClient, err := newGitHubClient(auth)
	if err != nil {
		return err
	}

	webDir := os.Getenv(envWebDir)

	limits, err := llmLimits()
	if err != nil {
		return err
	}

	srv, err := etoki.New(etoki.Options{
		Addr:     os.Getenv("ETOKI_ADDR"),
		Boards:   boards,
		Mappings: sqlite.NewMappingRepository(db),
		LLM:      llmClient,
		GitHub:   githubClient,
		Auth:     auth,
		// 認証ありの構成で `/mcp` を OAuth で開く（ADR 0076）。Auth が nil なら
		// etoki.New が使わない。
		OAuthGrants:                  sqlite.NewOAuthGrantRepository(db),
		OAuthClientMetadataDocuments: true,
		WebDir:                       webDir,
		PublicURL:                    os.Getenv(envPublicURL),
		KindFieldName:                os.Getenv("ETOKI_GITHUB_KIND_FIELD"),
		ParentFieldName:              os.Getenv("ETOKI_GITHUB_PARENT_FIELD"),
		Logger:                       logger,
		LLMLimits:                    limits,
		AllowedOrigins:               splitList(os.Getenv("ETOKI_ALLOWED_ORIGINS")),
	})
	if err != nil {
		return err
	}

	logger.InfoContext(ctx, "listening",
		slog.String("addr", srv.Addr()),
		slog.String("db", path),
		slog.Bool("llm", llmClient != nil),
		slog.Bool("github", githubClient != nil),
		slog.Bool("auth", auth != nil),
		// 画面を配っているか。配っていないのに :8080 をブラウザで開くと
		// 404 しか返らないので、起動時に読めるようにしておく。
		slog.Bool("web", webDir != ""),
	)

	// 認証を有効にすると、それ以前のボードは所有者が無いので画面から消える
	// （ADR 0016）。黙って消さず、引き受け方を添えて知らせる。
	if auth != nil {
		n, err := boards.CountUnowned(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			logger.WarnContext(ctx, "boards without an owner are hidden",
				slog.Int("count", n),
				slog.String("hint", "run `etoki claim <login>` to take them over"),
			)
		}
	}

	return srv.Run(ctx)
}

// newLLMClient は環境変数から LLM クライアントを組み立てる。
//
// エンドポイントも鍵も未設定なら nil を返す。「設定していない」と「設定を
// 間違えた」を区別したいため。nil のとき解釈は 503 で「設定されていない」と
// 返り、鍵だけ誤っている場合は呼び出し時に 401 として返る。
//
// 鍵の有無では判断できない。認証不要のローカルエンドポイントに向けるとき、
// 鍵は空のままで正しい（ADR 0008）。
func newLLMClient() (port.LLMClient, error) {
	cfg := llm.ConfigFromEnv()
	if cfg.BaseURL == "" && cfg.APIKey == "" {
		return nil, nil
	}

	// BaseURL の綴り間違いはここで落ちる。実行時まで持ち越さない。
	c, err := llm.New(cfg)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// newAuthenticator は環境変数から認証を組み立てる。
//
// GitHub App が未設定なら nil を返す。認証しない構成になり、これまでどおり
// PAT で動く（ADR 0015）。
func newAuthenticator(db *sql.DB) (*etoki.Authenticator, error) {
	cfg := githubauth.ConfigFromEnv()
	if !cfg.Configured() {
		return nil, nil
	}

	// 認証を使うなら鍵は必須。無いまま起動して平文で保存する、を起こさない。
	box, err := newSecretBox()
	if err != nil {
		return nil, err
	}

	// client_id と client_secret の片方だけ、はここで落ちる。
	provider, err := githubauth.New(cfg)
	if err != nil {
		return nil, err
	}

	return etoki.NewAuthenticator(provider, sqlite.NewSessionRepository(db, box))
}

// newGitHubClient は環境変数から GitHub クライアントを組み立てる。
//
// auth があればそれをトークン源にする。無ければ PAT。どちらも無ければ nil を
// 返し、作成のエンドポイントが「設定されていない」と返す。LLM と同じ扱い。
//
// Mode を決めるのはここだけ。「使えるリポジトリ」の定義が GitHub App と PAT で
// 違うが、それを知っているのは認証をどう設定したかを見ているこの層だけ
// （ADR 0015）。
func newGitHubClient(auth *etoki.Authenticator) (port.GitHubClient, error) {
	cfg := github.ConfigFromEnv()

	if auth != nil {
		// 認証を設定したら PAT は使わない。作成の主体がリクエストごとに
		// 変わると、誰が作ったのかを追えなくなる。
		cfg.Token = ""
		cfg.TokenSource = auth
		cfg.Mode = github.ModeApp
	} else if cfg.Token == "" {
		return nil, nil
	}

	c, err := github.New(cfg)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// claim は所有者の無いボードを引き受ける。
//
// 初回ログインした利用者に自動で寄せない。共有サーバーで先に入った人が全部を
// 持っていく決まり方は説明できないため、明示的な操作にしてある（ADR 0016）。
//
// **引き受ける前に、login が当たった相手を見せて確かめる**（ADR 0053）。etoki が
// 知っているのは最後にその login でログインした人までで、改名で空いた login を
// 取った別人かどうかは GitHub にしか分からない。
func claim(ctx context.Context, login string, yes bool) error {
	// 確かめられない入力で黙って進めない。パイプから呼ぶなら --yes を明示させる。
	if !yes && !isTerminal(os.Stdin) {
		return errors.New("claim asks for confirmation; pass --yes when stdin is not a terminal")
	}

	db, sessions, err := openAuthDB(ctx, "claim")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// 引き受ける相手は一度ログインしている必要がある。users に行ができるのは
	// ログインしたときだけなので、そこで初めて指せるようになる。
	user, err := sessions.FindUserByLogin(ctx, githubauth.ProviderName, login)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("unknown user %q: sign in once before claiming boards", login)
	}

	boards := sqlite.NewBoardRepository(db)
	unowned, err := boards.CountUnowned(ctx)
	if err != nil {
		return err
	}

	if !yes && !confirmClaim(os.Stdin, os.Stderr, *user, unowned) {
		return errors.New("claim canceled")
	}

	n, err := boards.ClaimUnowned(ctx, user.ID)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "etoki: claimed %d board(s) for %s (%s)\n", n, user.Login, user.ID)

	return nil
}

// parseClaimArgs は claim の引数を読む。
//
// フラグは --yes の 1 つだけなので flag パッケージは使わない（サブコマンドと同じ
// 判断）。位置は login の前後どちらでもよい。
func parseClaimArgs(args []string) (login string, yes bool, err error) {
	for _, a := range args {
		switch {
		case a == "--yes":
			yes = true
		case strings.HasPrefix(a, "-"):
			return "", false, fmt.Errorf("claim: unknown flag %q", a)
		case login != "":
			return "", false, errors.New("claim takes exactly one login")
		default:
			login = a
		}
	}
	if login == "" {
		return "", false, errors.New("claim requires a login")
	}
	return login, yes, nil
}

// confirmClaim は引き当てた相手を見せ、y / yes のときだけ true を返す。
//
// **既定は止める。** 何も打たずに Enter を押しただけで、別人に全ボードが
// 渡らないようにする。
func confirmClaim(in io.Reader, out io.Writer, u port.User, unowned int) bool {
	// 書けなくても確かめる手段が無いだけなので、読む側（既定は止める）に任せる。
	_, _ = fmt.Fprintf(out, `所有者の無いボード %d 枚を、次の利用者に引き受けさせます。
  表示名        %s
  login         @%s
  ID            %s
  最終ログイン  %s
login は最後にログインしたときのものです。改名で空いた login を別人が取って
いないか、表示名と最終ログインで確かめてください。
引き受けますか？ [y/N] `,
		unowned, u.DisplayName, u.Login, u.ID,
		u.UpdatedAt.UTC().Format("2006-01-02 15:04 MST"))

	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// openAuthDB は認証まわりを触るサブコマンド（claim / grants / revoke）の
// 共通の準備。DB を開き、マイグレーション済みで、認証が設定されていることを
// 確かめる。
//
// serve と同じ確認を通す。未初期化の DB に対して叩くと、原因の分からない SQL
// エラーになる。
func openAuthDB(ctx context.Context, command string) (*sql.DB, *sqlite.SessionRepository, error) {
	path := dbPath()

	db, err := sqlite.Open(ctx, path)
	if err != nil {
		return nil, nil, err
	}

	if err := sqlite.EnsureMigrated(ctx, db); err != nil {
		_ = db.Close()
		if errors.Is(err, sqlite.ErrNotMigrated) {
			return nil, nil, fmt.Errorf("%w\n  先に `make migrate` を実行してください (%s)", err, path)
		}
		return nil, nil, err
	}

	if !githubauth.ConfigFromEnv().Configured() {
		_ = db.Close()
		return nil, nil, fmt.Errorf("%s requires authentication to be configured", command)
	}

	box, err := newSecretBox()
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	return db, sqlite.NewSessionRepository(db, box), nil
}

// grantRow は一覧と確認に出す 1 行。許可に、許した利用者の login を添える。
type grantRow struct {
	grant port.OAuthGrant
	login string
}

// grantRows は許可に login を添える。利用者が消えていれば login は空。
func grantRows(
	ctx context.Context, sessions *sqlite.SessionRepository, grants []port.OAuthGrant,
) ([]grantRow, error) {
	ids := make([]string, 0, len(grants))
	for _, g := range grants {
		ids = append(ids, g.UserID)
	}
	users, err := sessions.FindUsers(ctx, ids)
	if err != nil {
		return nil, err
	}
	logins := make(map[string]string, len(users))
	for _, u := range users {
		logins[u.ID] = u.Login
	}

	rows := make([]grantRow, 0, len(grants))
	for _, g := range grants {
		rows = append(rows, grantRow{grant: g, login: logins[g.UserID]})
	}
	return rows, nil
}

// writeGrants は許可を表にして書く。
func writeGrants(out io.Writer, rows []grantRow) {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tLOGIN\tCLIENT\tCLIENT ID\tGRANTED\tLAST USED")
	for _, r := range rows {
		login := "@" + r.login
		if r.login == "" {
			login = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.grant.ID, login, orDash(r.grant.ClientName), r.grant.ClientID,
			r.grant.CreatedAt.UTC().Format("2006-01-02 15:04 MST"),
			r.grant.LastUsedAt.UTC().Format("2006-01-02 15:04 MST"))
	}
	_ = tw.Flush()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// listGrants は MCP のクライアントに許した接続を一覧する（ADR 0076）。
//
// login を渡せばその利用者のものだけ。サーバーを動かしている人が、画面に
// 入れなくなった利用者の接続を見つけて切るための道具。
func listGrants(ctx context.Context, login string) error {
	db, sessions, err := openAuthDB(ctx, "grants")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	repo := sqlite.NewOAuthGrantRepository(db)
	var grants []port.OAuthGrant
	if login == "" {
		grants, err = repo.ListAllGrants(ctx)
	} else {
		user, ferr := sessions.FindUserByLogin(ctx, githubauth.ProviderName, login)
		if ferr != nil {
			return ferr
		}
		if user == nil {
			return fmt.Errorf("unknown user %q", login)
		}
		grants, err = repo.ListGrants(ctx, user.ID)
	}
	if err != nil {
		return err
	}

	rows, err := grantRows(ctx, sessions, grants)
	if err != nil {
		return err
	}
	writeGrants(os.Stdout, rows)
	return nil
}

// revokeTarget は revoke が取り消す相手。どちらか一方だけが入る。
type revokeTarget struct {
	grantID string
	login   string
	yes     bool
}

// revoke は接続を取り消す（ADR 0076）。
//
// **取り消す前に対象を見せて確かめる**（claim と同じ。ADR 0053）。取り消すと
// 手元の MCP のクライアントは同意からやり直しになる。既定は止める。
func revoke(ctx context.Context, target revokeTarget) error {
	if !target.yes && !isTerminal(os.Stdin) {
		return errors.New("revoke asks for confirmation; pass --yes when stdin is not a terminal")
	}

	db, sessions, err := openAuthDB(ctx, "revoke")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	repo := sqlite.NewOAuthGrantRepository(db)
	var grants []port.OAuthGrant
	if target.grantID != "" {
		g, err := repo.FindGrant(ctx, target.grantID)
		if err != nil {
			return err
		}
		if g == nil {
			return fmt.Errorf("unknown grant %q", target.grantID)
		}
		grants = []port.OAuthGrant{*g}
	} else {
		user, err := sessions.FindUserByLogin(ctx, githubauth.ProviderName, target.login)
		if err != nil {
			return err
		}
		if user == nil {
			return fmt.Errorf("unknown user %q", target.login)
		}
		if grants, err = repo.ListGrants(ctx, user.ID); err != nil {
			return err
		}
	}
	if len(grants) == 0 {
		fmt.Fprintln(os.Stderr, "etoki: no grants to revoke")
		return nil
	}

	rows, err := grantRows(ctx, sessions, grants)
	if err != nil {
		return err
	}
	if !target.yes && !confirmRevoke(os.Stdin, os.Stderr, rows) {
		return errors.New("revoke canceled")
	}

	for _, g := range grants {
		if err := repo.DeleteGrant(ctx, g.ID); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "etoki: revoked %d grant(s)\n", len(grants))
	return nil
}

// confirmRevoke は取り消す対象を見せ、y / yes のときだけ true を返す。
// 既定は止める（confirmClaim と同じ）。
func confirmRevoke(in io.Reader, out io.Writer, rows []grantRow) bool {
	_, _ = fmt.Fprintf(out, "次の %d 件の接続を取り消します。"+
		"そのクライアントは同意からやり直しになります。\n", len(rows))
	writeGrants(out, rows)
	_, _ = fmt.Fprint(out, "取り消しますか？ [y/N] ")

	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// parseGrantsArgs は grants の引数を読む。
func parseGrantsArgs(args []string) (login string, err error) {
	for i := 0; i < len(args); i++ {
		if args[i] != "--login" {
			return "", fmt.Errorf("grants: unexpected argument %q", args[i])
		}
		// **空の値を断る。** 空の login は「絞らない」と同じに読まれるので、
		// `--login "$LOGIN"` の変数が空だと、1 人ぶんのつもりで全員の接続を出す。
		if i+1 >= len(args) || login != "" || args[i+1] == "" {
			return "", errors.New("grants: --login takes one non-empty login")
		}
		i++
		login = args[i]
	}
	return login, nil
}

// parseRevokeArgs は revoke の引数を読む。grant の ID か --login のどちらか
// 1 つを要る。両方を許すと、どちらが取り消されるのかが読み手に分からない。
func parseRevokeArgs(args []string) (revokeTarget, error) {
	var t revokeTarget
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--yes":
			t.yes = true
		case a == "--login":
			// 空の値は grants と同じく断る。
			if i+1 >= len(args) || t.login != "" || args[i+1] == "" {
				return revokeTarget{}, errors.New("revoke: --login takes one non-empty login")
			}
			i++
			t.login = args[i]
		case strings.HasPrefix(a, "-"):
			return revokeTarget{}, fmt.Errorf("revoke: unknown flag %q", a)
		case t.grantID != "":
			return revokeTarget{}, errors.New("revoke takes exactly one grant id")
		default:
			t.grantID = a
		}
	}
	switch {
	case t.grantID == "" && t.login == "":
		return revokeTarget{}, errors.New("revoke requires a grant id or --login")
	case t.grantID != "" && t.login != "":
		return revokeTarget{}, errors.New("revoke takes either a grant id or --login, not both")
	}
	return t, nil
}

// isTerminal は f が端末につながっているかを返す。
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// newSecretBox は保存する資格情報に封をする道具を作る。
//
// 認証を使う経路（serve と claim）から共通で呼ぶ。鍵の扱いを 2 箇所に書くと、
// 片方だけ緩められる余地ができる。
func newSecretBox() (secret.Box, error) {
	key, err := secret.DecodeKey(os.Getenv(envEncryptionKey))
	if err != nil {
		return secret.Box{}, fmt.Errorf("%w\n  %s に base64 の 32 バイトを設定してください",
			err, envEncryptionKey)
	}
	return secret.New(key)
}

func migrate(ctx context.Context) error {
	path := dbPath()

	db, err := sqlite.Open(ctx, path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := sqlite.Migrate(ctx, db); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "etoki: migrated %s\n", path)

	return nil
}

func dbPath() string {
	if p := os.Getenv("ETOKI_DB_PATH"); p != "" {
		return p
	}
	return defaultDBPath
}

// llmLimits は環境変数から実行の上限を組み立てる（ADR 0044）。
//
// **未設定と 0 を区別する。** 未設定は「既定のまま」、0 は設定の誤りとして
// 落とす。0 を無制限と読ませると、未設定（既定 1）と 0（無制限）で意味が
// 逆向きになる。矛盾の判定そのものは etoki.New が持つので、ここは読み取りだけ。
func llmLimits() (etoki.LLMLimits, error) {
	maxConcurrent, err := positiveInt("ETOKI_LLM_MAX_CONCURRENT")
	if err != nil {
		return etoki.LLMLimits{}, err
	}
	rateLimit, err := positiveInt("ETOKI_LLM_RATE_LIMIT")
	if err != nil {
		return etoki.LLMLimits{}, err
	}

	var window time.Duration
	if raw := os.Getenv("ETOKI_LLM_RATE_WINDOW"); raw != "" {
		window, err = time.ParseDuration(raw)
		if err != nil {
			return etoki.LLMLimits{}, fmt.Errorf("ETOKI_LLM_RATE_WINDOW: %w", err)
		}
		if window <= 0 {
			return etoki.LLMLimits{}, fmt.Errorf(
				"ETOKI_LLM_RATE_WINDOW must be positive, got %q", raw)
		}
	}

	return etoki.LLMLimits{
		MaxConcurrent: maxConcurrent,
		RateLimit:     rateLimit,
		RateWindow:    window,
	}, nil
}

// positiveInt は 1 以上の整数として環境変数を読む。未設定なら 0。
//
// **読めない値を既定に倒さない。** 倒すと、設定したつもりの上限が黙って外れる
// （中核思想 3）。
func positiveInt(name string) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	if n < 1 {
		return 0, fmt.Errorf("%s must be at least 1, got %q", name, raw)
	}
	return n, nil
}

// splitList はカンマ区切りの環境変数を要素に分ける。空要素は捨てる。
func splitList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}
