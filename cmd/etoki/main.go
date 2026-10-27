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
	"strconv"
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

	// サブコマンドとその引数は手で読む。flag パッケージは位置引数より後ろの
	// フラグを読まない（`claim <login> --yes` が通らない）うえ、空の `--login` を
	// 断る判定（parseGrantsArgs）は結局ここで書くことになる。サブコマンドが
	// 4 つに増えた時点で見直し、この判断のままにした。
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

	db, path, err := openMigratedDB(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

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
		AllowedOrigins:               splitList(os.Getenv(envAllowedOrigins)),
		SetupHints:                   setupHints(),
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

// openMigratedDB は DB を開き、マイグレーション済みであることを確かめる。開いた
// ファイルのパスも返す（起動時のログと案内に載せる）。
//
// SQLite は接続時にファイルを作るため、未初期化でも開けてしまう。そのまま動かすと
// 全 API が 500 を返し続け、管理用のコマンドは原因の分からない SQL エラーになる
// ので、ここで落とす。適用は明示的な操作にしておきたいので自動では行わない。
//
// **serve と管理用のコマンド（openAuthDB）はどちらもここを通す。** 案内を 2 箇所に
// 書くと、片方だけ古い手順を言う。
func openMigratedDB(ctx context.Context) (*sql.DB, string, error) {
	path := dbPath()

	db, err := sqlite.Open(ctx, path)
	if err != nil {
		return nil, path, err
	}

	if err := sqlite.EnsureMigrated(ctx, db); err != nil {
		_ = db.Close()
		if errors.Is(err, sqlite.ErrNotMigrated) {
			return nil, path, fmt.Errorf("%w\n  先に `make migrate` を実行してください (%s)", err, path)
		}
		return nil, path, err
	}
	return db, path, nil
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
