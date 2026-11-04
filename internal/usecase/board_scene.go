package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/port"
)

// ErrSceneConflict は保存の基準にした版が古いことを表す。
//
// ボードは共有できるので（ADR 0017）、2 人が同時に描くのは例外ではない。保存は
// シーン全体を書くため、後勝ちで通すと消えるのは「相手が触った要素」ではなく
// 相手の作業すべてになる。黙って一方を採るのではなく、食い違っていることを
// 呼び出し側に返す（ADR 0020）。
var ErrSceneConflict = errors.New("etoki: board scene was updated by someone else")

// ErrSceneTooLarge はシーンが保存できる大きさを超えていることを表す。
//
// **ErrInvalidInput に畳まない。** 中身の誤りではなく大きさなので、打ち手が
// 「送った内容を直す」ではなく「貼った画像を減らす」になる。畳むと画面は
// どちらとも言えない文言しか出せない（ADR 0034）。
var ErrSceneTooLarge = errors.New("etoki: board scene is too large")

// SavedScene は保存の結果。
type SavedScene struct {
	// UpdatedAt は保存後の版。次の保存の基準になる（ADR 0020）。
	UpdatedAt time.Time
	// FileIDs は保存後にボードが持っている画像の ID。次の保存で送らなくて
	// よい画像の一覧になる（ADR 0074）。
	FileIDs []string
}

// SaveScene はボードのシーンと貼った画像を更新し、保存後の版を返す。
//
// base は編集の基準にした更新時刻。いまの版と違えば何も書かずに
// ErrSceneConflict を返す（ADR 0020）。返した時刻が次の保存の基準になる。
//
// files は画像の ID → Excalidraw の画像データ（JSON）で、**ボードがまだ
// 持っていない画像だけ**を受け取る（ADR 0074）。シーンから参照されなくなった
// 画像は保存で消える。
func (s *BoardService) SaveScene(
	ctx context.Context, id, scene string, files map[string]string, base time.Time,
) (SavedScene, error) {
	parsed, err := validateScene(scene)
	if err != nil {
		return SavedScene{}, err
	}
	referenced := parsed.FileIDs()
	added, err := validateFiles(files, referenced)
	if err != nil {
		return SavedScene{}, err
	}
	// 未指定を「照合しない」に倒さない。倒すと API を直接叩く経路で照合を
	// 素通りでき、防ぎたい後勝ちがそのまま残る（ADR 0010 と同じ理由）。
	if base.IsZero() {
		return SavedScene{}, fmt.Errorf("%w: baseUpdatedAt is required", ErrInvalidInput)
	}
	if _, err := s.access(ctx, id, port.RoleEditor); err != nil {
		return SavedScene{}, err
	}

	// 画像の合計は**保存したあとに残るもの**で数える。外した画像は保存で
	// 消えるので、数えると「1 枚消して 1 枚足す」が通らなくなる。
	//
	// 大きさは書く前に別に読むが、**読んだ時点と書く時点の食い違いは問題に
	// ならない。** 画像が変わるのは版を進める保存だけなので、その間に別の
	// 保存が入っていれば、この保存は照合に負けて何も書かない（ADR 0020）。
	sizes, err := s.boards.FileSizes(ctx, id)
	if err != nil {
		return SavedScene{}, err
	}
	if total := heldFileBytes(referenced, added, sizes); FilesExceedLimit(total) {
		return SavedScene{}, fmt.Errorf("%w: pasted images total %d bytes, limit is %d",
			ErrSceneTooLarge, total, MaxBoardFileBytes)
	}

	// 引き当てた Board の UpdatedAt とはここで比べない。比べてから書くまでの
	// 隙間に入った保存を上書きするので、照合は UPDATE と同じ 1 文に任せる。
	//
	// **時計が進まなくても版は必ず進める。** 同じ時刻を書くと、次に来た古い
	// 基準がそのまま一致してしまい、照合を置いた意味が無くなる（ADR 0020）。
	// 書けた保存は base = そのときの版なので、base より後にすれば必ず進む。
	// 時計が巻き戻ったときも同じ理由でここを通る。
	now := s.now()
	if !now.After(base) {
		now = base.Add(time.Nanosecond)
	}

	held, err := s.boards.UpdateScene(ctx, actorOf(ctx), id, port.SceneWrite{
		Scene:      scene,
		Added:      added,
		Referenced: referenced,
	}, base, now)
	if err != nil {
		if errors.Is(err, port.ErrConflict) {
			// 「保存に失敗した」ではなく「他の人が保存している」という状態。
			// 呼び出し側が上書きせずに見せられるよう、専用のエラーに写す。
			return SavedScene{}, fmt.Errorf("%w: %s", ErrSceneConflict, id)
		}
		return SavedScene{}, err
	}

	return SavedScene{UpdatedAt: now, FileIDs: held}, nil
}

// heldFileBytes は保存したあとにボードが持つ画像の合計バイト数を返す。
//
// 参照している画像ごとに、今回送られてきたものがあればその大きさ、無ければ
// いま持っているものの大きさを足す。どちらにも無い画像（参照先の実体が
// 欠けている）は 0 として数える。
func heldFileBytes(
	referenced []string, added []port.BoardFile, stored map[string]int64,
) int64 {
	sent := make(map[string]int64, len(added))
	for _, f := range added {
		sent[f.ID] = int64(len(f.Data))
	}

	var total int64
	for _, id := range referenced {
		if n, ok := sent[id]; ok {
			total += n
			continue
		}
		total += stored[id]
	}
	return total
}

// validateScene は保存前にシーンが読めることと、大きさが上限に収まることを
// 確かめ、読んだシーンを返す。
//
// 壊れた JSON を保存すると、次に読み込んだときにボードごと開けなくなる。
// 入口で弾いておく。
//
// **大きさは読む前に見る。** 上限を超えたシーンは弾くと決めてあるので
// （MaxSceneBytes）、通らないと分かっているものを解析する理由が無い。
//
// **超えたぶんを削って保存しない。** 保存はシーン全体を書くので、削れるのは
// 開発者が描いたものそのものになる（ADR 0038）。
//
// **画像の実体を抱えたシーンは弾く**（ADR 0074）。画像は別に送らせる。受け
// 付けると画像の入口が 2 つになり、シーンに乗った画像は画像の上限
// （MaxBoardFileBytes）も、参照されなくなったら消す約束も素通りする。
func validateScene(scene string) (domain.Scene, error) {
	if SceneExceedsLimit(scene) {
		return domain.Scene{}, fmt.Errorf("%w: scene is %d bytes, limit is %d",
			ErrSceneTooLarge, len(scene), MaxSceneBytes)
	}
	parsed, err := domain.ParseScene([]byte(scene))
	if err != nil {
		return domain.Scene{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	for _, a := range parsed.Annotations() {
		if err := a.CheckMeta(); err != nil {
			return domain.Scene{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
		}
	}
	if parsed.HasFiles() {
		return domain.Scene{}, fmt.Errorf(
			"%w: scene must not carry pasted images; send them as files", ErrInvalidInput)
	}
	return parsed, nil
}

// validateFiles は保存で足す画像を確かめ、ID の順に並べて返す。
//
// **シーンから参照されていない画像は弾く。** 受け取って捨てると、送った
// 画像が残っていないことに呼び出し側が気づけない（validation-boundaries の
// 「黙って空に落とさない」）。
//
// 中身は Excalidraw の画像データ（JSON のオブジェクト）で、**見るのは id が
// キーと同じかどうかだけ。** Excalidraw は画像を id で引くので、食い違うと
// 開き直したときに画像の要素が指す先が見つからない。それ以外の中身は解釈
// しない（シーンと同じ）。
func validateFiles(files map[string]string, referenced []string) ([]port.BoardFile, error) {
	if len(files) == 0 {
		return nil, nil
	}

	refs := make(map[string]struct{}, len(referenced))
	for _, id := range referenced {
		refs[id] = struct{}{}
	}

	ids := slices.Sorted(maps.Keys(files))
	added := make([]port.BoardFile, 0, len(ids))
	for _, id := range ids {
		if _, ok := refs[id]; !ok {
			return nil, fmt.Errorf("%w: file %q is not referenced by the scene", ErrInvalidInput, id)
		}

		data := files[id]
		var head struct {
			ID *string `json:"id"`
		}
		if err := json.Unmarshal([]byte(data), &head); err != nil {
			return nil, fmt.Errorf("%w: file %q is not a JSON object: %w", ErrInvalidInput, id, err)
		}
		if head.ID == nil || *head.ID != id {
			return nil, fmt.Errorf("%w: file %q does not carry its own id", ErrInvalidInput, id)
		}

		added = append(added, port.BoardFile{ID: id, Data: data})
	}
	return added, nil
}
