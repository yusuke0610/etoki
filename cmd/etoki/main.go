// Command etoki は etoki サーバーの起動とマイグレーション適用を行う。
//
// クラウド固有のアダプタが必要な場合、利用者はこの main を写して
// 独自の実装を差し込む。そのためここは配線だけに留め、ロジックを置かない。
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

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

// defaultDBPath は ETOKI_DB_PATH が未設定のときに使う SQLite ファイル。
const defaultDBPath = "etoki.db"

const usage = `usage:
  etoki                 サーバーを起動する
  etoki migrate         マイグレーションを適用する
  etoki claim <login>   所有者の無いボードを引き受ける

environment:
  ETOKI_ADDR            リッスンアドレス（既定: ` + etoki.DefaultAddr + `）
  ETOKI_ALLOWED_ORIGINS 追加で許すオリジン（カンマ区切り）。ループバックは常に許す
  ETOKI_DB_PATH         SQLite ファイルのパス（既定: ` + defaultDBPath + `）
  ETOKI_LLM_BASE_URL    LLM のエンドポイント（既定: ` + llm.DefaultBaseURL + `）
  ETOKI_LLM_API_KEY     LLM の API キー（認証不要なら未設定でよい）
  ETOKI_LLM_MODEL       モデル ID（既定: ` + llm.DefaultModel + `）
  ETOKI_GITHUB_TOKEN    GitHub のトークン（repo の read と Projects の read/write）
                        認証を設定した場合は使わない
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

	// サブコマンドは 1 つだけなので flag パッケージは使わない。増えたら見直す。
	args := os.Args[1:]
	switch {
	case len(args) == 0:
		return serve(ctx)
	case args[0] == "migrate":
		return migrate(ctx)
	case args[0] == "claim":
		if len(args) != 2 {
			fmt.Fprint(os.Stderr, usage)
			return errors.New("claim requires a login")
		}
		return claim(ctx, args[1])
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

	srv, err := etoki.New(etoki.Options{
		Addr:            os.Getenv("ETOKI_ADDR"),
		Boards:          boards,
		Mappings:        sqlite.NewMappingRepository(db),
		LLM:             llmClient,
		GitHub:          githubClient,
		Auth:            auth,
		PublicURL:       os.Getenv(envPublicURL),
		KindFieldName:   os.Getenv("ETOKI_GITHUB_KIND_FIELD"),
		ParentFieldName: os.Getenv("ETOKI_GITHUB_PARENT_FIELD"),
		Logger:          logger,
		AllowedOrigins:  splitList(os.Getenv("ETOKI_ALLOWED_ORIGINS")),
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
func claim(ctx context.Context, login string) error {
	path := dbPath()

	db, err := sqlite.Open(ctx, path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// serve と同じ確認を通す。未初期化の DB に対して claim すると、原因の
	// 分からない SQL エラーになる。
	if err := sqlite.EnsureMigrated(ctx, db); err != nil {
		if errors.Is(err, sqlite.ErrNotMigrated) {
			return fmt.Errorf("%w\n  先に `make migrate` を実行してください (%s)", err, path)
		}
		return err
	}

	if !githubauth.ConfigFromEnv().Configured() {
		return errors.New("claim requires authentication to be configured")
	}

	// 引き受ける相手は一度ログインしている必要がある。users に行ができるのは
	// ログインしたときだけなので、そこで初めて指せるようになる。
	box, err := newSecretBox()
	if err != nil {
		return err
	}

	user, err := sqlite.NewSessionRepository(db, box).
		FindUserByLogin(ctx, githubauth.ProviderName, login)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("unknown user %q: sign in once before claiming boards", login)
	}

	n, err := sqlite.NewBoardRepository(db).ClaimUnowned(ctx, user.ID)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "etoki: claimed %d board(s) for %s\n", n, login)

	return nil
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
