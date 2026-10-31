package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/yusuke0610/etoki/port"
)

// ErrTargetLocked は作成先を変えられないことを表す。
//
// そのボードで draft issue を 1 件でも作ったら固定する。sync_runs は GitHub 側に
// 残っている item の追跡表であり、作成先が変わると記録が指す先を見失う（ADR 0014）。
var ErrTargetLocked = errors.New("etoki: board target is locked")

// ErrTargetMismatch は表示名の取り直しなのに、送られた作成先が保存されている
// ものと違うことを表す。
//
// **ErrTargetLocked と分ける。** 直し方が違う。固定は解けないが、食い違いは
// 画面を開き直せば解ける（ADR 0037）。
var ErrTargetMismatch = errors.New("etoki: board target does not match")

// SetTarget は draft issue の作成先をボードに設定する。
//
// すでに draft issue を作っているボードでは ErrTargetLocked を返す。
func (s *BoardService) SetTarget(ctx context.Context, id string, t port.BoardTarget) error {
	if !t.Selected() {
		return fmt.Errorf("%w: repository and project are required", ErrInvalidInput)
	}
	if err := validateProjectURL(t.ProjectURL); err != nil {
		return err
	}

	// 作成中のボードは待つ。作成が終われば run が残るので、待った先で
	// ErrTargetLocked になる。「作成が始まっていたなら変えられない」が
	// 判定の取りこぼしではなく順序として出る。
	release, err := s.locks.Acquire(ctx, id)
	if err != nil {
		return err
	}
	defer release()

	// 作成先はそのボードで作られる draft issue の行き先そのものなので、
	// 変えられるのは owner だけ（ADR 0017）。
	if _, err := s.access(ctx, id, port.RoleOwner); err != nil {
		return err
	}

	locked, err := s.TargetLocked(ctx, id)
	if err != nil {
		return err
	}
	if locked {
		// 同じ値なら通してもよさそうだが、通さない。「変更できた」と
		// 「たまたま同じだった」を呼び出し側が区別できなくなる。
		return fmt.Errorf("%w: %s", ErrTargetLocked, id)
	}

	return s.boards.UpdateTarget(ctx, actorOf(ctx), id, t)
}

// RefreshTargetDisplay は作成先の表示用スナップショットだけを更新する。
//
// **固定済みでも通る。** 固定するのは作成先そのもの（owner / name / projectId）
// であって、表示用の値ではない（ADR 0037）。GitHub 側で Project を改名すると
// 古い名前が残るが、選び直しは固定後に通らないので直す手段が無かった。
//
// projectID は変更先ではなく照合材料。保存されているものと違えば
// ErrTargetMismatch を返し、何も書かない。
func (s *BoardService) RefreshTargetDisplay(
	ctx context.Context, id, projectID string, d port.BoardTargetDisplay,
) error {
	if projectID == "" {
		return fmt.Errorf("%w: projectId is required", ErrInvalidInput)
	}
	// URL の門番は SetTarget と同じ。こちらだけ素通しにすると、固定済みの
	// ボードに javascript: を保存する道が開く（ADR 0025）。
	if err := validateProjectURL(d.ProjectURL); err != nil {
		return err
	}

	// 作成先の変更と同じ鍵で直列化する。引き当ててから書くまでの隙間に
	// SetTarget が入ると、別の Project の表示名を書くことになる。
	release, err := s.locks.Acquire(ctx, id)
	if err != nil {
		return err
	}
	defer release()

	// 作成先にまつわる操作なので owner だけ。変更と揃える（ADR 0017）。
	a, err := s.access(ctx, id, port.RoleOwner)
	if err != nil {
		return err
	}
	if !a.Board.Target.Selected() {
		return fmt.Errorf("%w: %s", ErrTargetNotSelected, id)
	}
	if a.Board.Target.ProjectID != projectID {
		return fmt.Errorf("%w: %s", ErrTargetMismatch, id)
	}

	return s.boards.UpdateTargetDisplay(ctx, actorOf(ctx), id, d)
}

// TargetLocked はそのボードの作成先が固定済みかどうかを返す。
//
// 判定は「run が 1 件でもあるか」。フロントは sync_runs を数えられないので、
// 状態としてサーバーが返す必要がある。
//
// **所有者は見ない。呼ぶ前に Find で確かめること。** run は board_id でしか
// 引けないので、ここで二重に絞ると、絞り忘れたときにどちらが効いているのか
// 分からなくなる（ADR 0016）。
func (s *BoardService) TargetLocked(ctx context.Context, id string) (bool, error) {
	runs, err := s.mappings.ListLatestRunsByBoard(ctx, id)
	if err != nil {
		return false, err
	}
	return len(runs) > 0, nil
}

// projectURLHost は作成先 URL に許すホスト。
//
// GHES では変わるが、いまはホストを設定する導線が無い（`github.Config.BaseURL`
// は env から埋めていない）。導線を足すときはここも一緒に設定可能にする。
const projectURLHost = "github.com"

// validateProjectURL は作成先 URL が GitHub のものかを見る。
//
// **これが唯一の門番。** 保存された値はフロントがそのまま `href` に入れるので、
// ここを通り抜けたものは開発者のブラウザで開かれる。owner は招待した相手に
// 画面を見せられる（ADR 0017）ので、`javascript:` を保存できると招待した相手の
// ブラウザで実行できてしまう。外部ドメインを通すと、「GitHub でこの Project を
// 開く」という文言で別の場所へ送れる。
//
// **フロント側には同じ判定を置かない。** この列は新規なので、壊れた値が入る
// 経路はここしか無い。両側に置くと判定が 2 箇所になり、片方だけ変わる。
//
// **パスの形は見ない。** `/orgs/...` か `/users/...` かは owner が user か org
// かで変わり、etoki はどちらなのかを知らない（ADR 0025）。ここで形を決めると、
// 知らないと言った判断を裏で覆すことになる。
func validateProjectURL(raw string) error {
	// 空文字は「URL を知らない」。移行前のボードと、URL を送らずに設定した
	// 作成先が該当する。フロントがリポジトリの Projects へ落とす。
	if raw == "" {
		return nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: projectUrl is not a URL: %w", ErrInvalidInput, err)
	}
	// 認証情報つきの URL は弾く。ホストは合っていても、表示上どこへ向かうのかが
	// 読みにくくなる。
	if u.Scheme != "https" || !strings.EqualFold(u.Host, projectURLHost) || u.User != nil {
		return fmt.Errorf("%w: projectUrl must be an https://%s URL (got %q)",
			ErrInvalidInput, projectURLHost, raw)
	}

	return nil
}
