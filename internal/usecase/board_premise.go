package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/yusuke0610/etoki/port"
)

// ErrPremiseConflict はボードの背景を、編集を始めた時点の本文から書き換え
// ようとしたのに、そのあいだに別の人が背景を書き換えていたことを表す
// （ADR 0093）。
//
// ErrSceneConflict と分ける。食い違ったのはシーンではなく背景で、打ち手は
// 「いまの背景を読み直して書き直す」になる。
var ErrPremiseConflict = errors.New("etoki: board premise was updated by someone else")

// ErrPremiseTooLarge はボードの背景が上限（MaxPremiseRunes）を超えていることを
// 表す（ADR 0093）。
//
// **ErrInvalidInput に畳まない。** 中身の誤りではなく長さなので、打ち手が
// 「短くする」になる（ErrSceneTooLarge と同じ理由）。
var ErrPremiseTooLarge = errors.New("etoki: board premise is too large")

// SetPremise はボードの背景を書き換える（ADR 0093）。
//
// base は編集を始めた時点の背景。いまの背景と食い違えば何も書かずに
// ErrPremiseConflict を返す。**照合は永続化層の書き込みと同じ 1 文に任せる。**
// ここで読んで比べると、比べてから書くまでの隙間に入った編集を上書きする。
//
// **editor 以上。** 改名と同じで、作成先のように取り消せない作成の行き先を
// 決めるものではない。
//
// 背景はハッシュに入らない（ADR 0093）ので、書き換えても注釈の 3 状態は
// 変わらない。版（updated_at）も進めない（ADR 0092）。
func (s *BoardService) SetPremise(ctx context.Context, id, base, premise string) error {
	premise, err := normalizePremise(premise)
	if err != nil {
		return err
	}

	if _, err := s.access(ctx, id, port.RoleEditor); err != nil {
		return err
	}

	if err := s.boards.UpdatePremise(ctx, actorOf(ctx), id, base, premise); err != nil {
		if errors.Is(err, port.ErrConflict) {
			return fmt.Errorf("%w: %s", ErrPremiseConflict, id)
		}
		return err
	}
	return nil
}

// normalizePremise は背景の改行を揃え、前後の空白を落としてから長さを見る。
//
// 改行を揃えるのは、編集した環境の違いだけで本文が変わらないようにするため。
// 空白だけの背景は空にする（「書いていない」と同じ）。**途中の空白や空行は
// 残す。** 書いた人が区切りとして置いたもので、LLM にもそのまま渡す。
func normalizePremise(premise string) (string, error) {
	premise = strings.ReplaceAll(premise, "\r\n", "\n")
	premise = strings.ReplaceAll(premise, "\r", "\n")
	premise = strings.TrimSpace(premise)

	if n := utf8.RuneCountInString(premise); n > MaxPremiseRunes {
		return "", fmt.Errorf("%w: premise has %d characters, limit is %d",
			ErrPremiseTooLarge, n, MaxPremiseRunes)
	}
	return premise, nil
}
