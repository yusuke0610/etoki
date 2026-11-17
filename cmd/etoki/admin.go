package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	githubauth "github.com/yusuke0610/etoki/internal/adapter/auth/github"
	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// このファイルはサーバーを動かす人のための管理用コマンド（migrate / claim /
// grants / revoke / delete-user）を置く。**写して独自の main を書く利用者が要るのは main.go の
// 配線で、ここではない。** 分けてあるのは、どこまでが配線かを読み分けさせないため。

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
func parseClaimArgs(args []string) (login string, yes bool, err error) {
	return parseLoginArgs("claim", args)
}

// parseDeleteUserArgs は delete-user の引数を読む。形は claim と同じ。
func parseDeleteUserArgs(args []string) (login string, yes bool, err error) {
	return parseLoginArgs("delete-user", args)
}

// parseLoginArgs は「login 1 つと --yes」を取るサブコマンドの引数を読む。
//
// フラグは --yes の 1 つだけなので flag パッケージは使わない（サブコマンドと同じ
// 判断）。位置は login の前後どちらでもよい。
func parseLoginArgs(command string, args []string) (login string, yes bool, err error) {
	for _, a := range args {
		switch {
		case a == "--yes":
			yes = true
		case strings.HasPrefix(a, "-"):
			return "", false, fmt.Errorf("%s: unknown flag %q", command, a)
		case login != "":
			return "", false, fmt.Errorf("%s takes exactly one login", command)
		default:
			login = a
		}
	}
	if login == "" {
		return "", false, fmt.Errorf("%s requires a login", command)
	}
	return login, yes, nil
}

// deleteUser は利用者を、その人だけが owner のボードとメンバーの行ごと消す
// （ADR 0101）。画面に入れなくなった利用者から、消してほしいと頼まれたときの道具。
//
// **消し方は画面の口と同じ（usecase.AccountService）。** ここで書き直すと、どれを
// 消してどれを残すかの判断が 2 箇所になる。
//
// **消す前に、引き当てた相手と消えるものを見せて確かめる**（claim / revoke と
// 同じ、ADR 0053）。
//
// **サーバーとは排他を共有しない。** ボードのロックはプロセスの中だけにある
// （ADR 0014）。その利用者のボードで作成が走っているあいだに消すと、作った draft
// issue の記録が残らない。サーバーを止めるか、作成が走っていないときに使う。
func deleteUser(ctx context.Context, login string, yes bool) error {
	// 確かめられない入力で黙って進めない。パイプから呼ぶなら --yes を明示させる。
	if !yes && !isTerminal(os.Stdin) {
		return errors.New(
			"delete-user asks for confirmation; pass --yes when stdin is not a terminal")
	}

	db, sessions, err := openAuthDB(ctx, "delete-user")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	user, err := sessions.FindUserByLogin(ctx, githubauth.ProviderName, login)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("unknown user %q", login)
	}

	boards := sqlite.NewBoardRepository(db)
	accounts := usecase.NewAccountService(boards,
		usecase.NewBoardService(boards, sqlite.NewMappingRepository(db), usecase.NewBoardLocks()),
		sqlite.NewAccountRepository(db))
	// 消す相手として振る舞う。どれを消すかは、その人から見たボードで決まる。
	ctx = port.ContextWithUserID(ctx, user.ID)

	losing, err := accounts.Deletion(ctx)
	if err != nil {
		return err
	}
	if !yes && !confirmDeleteUser(os.Stdin, os.Stderr, *user, losing) {
		return errors.New("delete-user canceled")
	}

	// 消すものは Delete が引き直して決める。見せた一覧を照合しない（ADR 0042）
	// ので、ここで消えた枚数を言わない。見せた数と違いうる。
	if err := accounts.Delete(ctx); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "etoki: deleted %s (%s)\n", user.Login, user.ID)
	return nil
}

// confirmDeleteUser は消す相手と消えるものを見せ、y / yes のときだけ true を返す。
// **既定は止める**（confirm）。
func confirmDeleteUser(
	in io.Reader, out io.Writer, u port.User, losing usecase.AccountDeletion,
) bool {
	// 書けなくても確かめる手段が無いだけなので、読む側（既定は止める）に任せる。
	_, _ = fmt.Fprintln(out, "次の利用者を消します。取り消せません。")
	writeUser(out, u)
	writeAccountDeletion(out, losing)
	return confirm(in, out, "消しますか？")
}

// writeAccountDeletion は消えるものを書く。画面の確認と同じ材料を出す（ADR 0101）。
func writeAccountDeletion(out io.Writer, d usecase.AccountDeletion) {
	if len(d.Boards) == 0 {
		_, _ = fmt.Fprintln(out, "一緒に消えるボードはありません。")
	} else {
		_, _ = fmt.Fprintf(out,
			"一緒に消えるボード（%d 枚。共有していても、この利用者だけがオーナーなら消えます）:\n",
			len(d.Boards))
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "  NAME\tOTHER MEMBERS\tRECORDED DRAFT ISSUES\tUNCONFIRMED WRITES")
		for _, b := range d.Boards {
			_, _ = fmt.Fprintf(tw, "  %s\t%d\t%d\t%d\n", b.Name, b.OtherMemberCount,
				b.Losing.RecordedItemCount, b.Losing.UnconfirmedWriteCount)
		}
		_ = tw.Flush()
	}
	if d.LeftBoardCount > 0 {
		_, _ = fmt.Fprintf(out,
			"ほかのボード %d 枚からはメンバーとして外れます（ボードは残ります）。\n", d.LeftBoardCount)
	}
	_, _ = fmt.Fprintln(out, "セッション・GitHub のトークン・MCP の接続も消えます。"+
		"GitHub に作った draft issue と、GitHub 側のアプリの認可は消えません。")
}

// confirmClaim は引き当てた相手を見せ、y / yes のときだけ true を返す。
//
// **既定は止める。** 何も打たずに Enter を押しただけで、別人に全ボードが
// 渡らないようにする。
func confirmClaim(in io.Reader, out io.Writer, u port.User, unowned int) bool {
	// 書けなくても確かめる手段が無いだけなので、読む側（既定は止める）に任せる。
	_, _ = fmt.Fprintf(out, "所有者の無いボード %d 枚を、次の利用者に引き受けさせます。\n", unowned)
	writeUser(out, u)
	return confirm(in, out, "引き受けますか？")
}

// confirm は問いを出し、y / yes と答えたときだけ true を返す。
//
// **既定は止める。** 何も打たずに Enter を押しただけで、別人に全ボードが渡ったり
// 接続が切れたりしないようにする（ADR 0053）。claim と revoke はどちらもここを
// 通す。片方だけ緩めると、確かめる操作のあいだで既定が割れる。
func confirm(in io.Reader, out io.Writer, question string) bool {
	// 書けなくても確かめる手段が無いだけなので、読む側（既定は止める）に任せる。
	_, _ = fmt.Fprint(out, question+" [y/N] ")

	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// writeUser は login で引き当てた相手を、確かめられる形で書く（ADR 0053）。
// etoki が知っているのは「最後にその login でログインした人」までなので、
// 別人かどうかは ID・表示名・最終ログインを見て人が決める。
func writeUser(out io.Writer, u port.User) {
	_, _ = fmt.Fprintf(out, `  表示名        %s
  login         @%s
  ID            %s
  最終ログイン  %s
login は最後にログインしたときのものです。改名で空いた login を別人が取って
いないか、表示名と最終ログインで確かめてください。
`,
		u.DisplayName, u.Login, u.ID, u.UpdatedAt.UTC().Format("2006-01-02 15:04 MST"))
}

// openAuthDB は認証まわりを触るサブコマンド（claim / grants / revoke）の
// 共通の準備。DB を開き、マイグレーション済みで、認証が設定されていることを
// 確かめる。
//
// serve と同じ確認を通す。未初期化の DB に対して叩くと、原因の分からない SQL
// エラーになる。
func openAuthDB(ctx context.Context, command string) (*sql.DB, *sqlite.SessionRepository, error) {
	db, _, err := openMigratedDB(ctx)
	if err != nil {
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
	var (
		grants []port.OAuthGrant
		owner  *port.User
	)
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
		owner = user
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
	if !target.yes && !confirmRevoke(os.Stdin, os.Stderr, rows, owner) {
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
// 既定は止める（confirm）。
//
// **login で指したときは、引き当てた相手も見せる**（user が nil でない）。
// 引き当ては claim と同じく「最後にその login でログインした人」なので、改名で
// 空いた login を取った別人の接続を切らないよう、claim と同じ材料を出す。
func confirmRevoke(in io.Reader, out io.Writer, rows []grantRow, user *port.User) bool {
	if user != nil {
		_, _ = fmt.Fprintln(out, "次の利用者の接続を取り消します。")
		writeUser(out, *user)
	}
	_, _ = fmt.Fprintf(out, "次の %d 件の接続を取り消します。"+
		"そのクライアントは同意からやり直しになります。\n", len(rows))
	writeGrants(out, rows)
	return confirm(in, out, "取り消しますか？")
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
