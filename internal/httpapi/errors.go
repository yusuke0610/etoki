package httpapi

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// errorMapping は sentinel error 1 つを、契約の code と HTTP ステータスに写す。
type errorMapping struct {
	err    error
	status int
	code   apitypes.ErrorCode
}

// errorMappings は写し替えの正本。**ここ以外に分岐を置かない。**
//
// 以前はエンドポイントごとに 5 つの switch があった。同じ sentinel が別の
// ステータスに写る箇所は無かったので、1 表に畳める。エンドポイントごとに違うのは
// **表に無かったときの既定だけ**で、そこは fail / failCatalog が持つ。
//
// **ステータスだけでは打ち手が決まらない。** 409 には「開き直す」「諦める」
// 「解釈からやり直す」「入力を変える」が同居しており、403 は etoki のロール不足と
// GitHub 側の拒否で直す場所が違う（ADR 0017）。画面が打ち手を分けられるよう、
// ステータスと一緒に code を返す。
//
// 先に一致したものを採る。sentinel は互いに入れ子にならない（ユースケース層が
// port のエラーを自分のものに写し替えて返す）ので、並び順に依存する組は無い。
var errorMappings = []errorMapping{
	{usecase.ErrInvalidInput, http.StatusBadRequest, apitypes.ErrorCodeInvalidInput},

	// メンバーでないボードもここに来る。403 と区別すると、ID を総当たりして
	// 他人のボードの存在を確かめられる（ADR 0016 / 0017）。
	{usecase.ErrBoardNotFound, http.StatusNotFound, apitypes.ErrorCodeNotFound},
	{usecase.ErrAnnotationNotFound, http.StatusNotFound, apitypes.ErrorCodeNotFound},
	{port.ErrNotFound, http.StatusNotFound, apitypes.ErrorCodeNotFound},

	// 403 は 2 層ある。**畳まない。** etoki のロール不足は owner に頼めば済み、
	// GitHub の拒否はリポジトリ側の権限を直すしかない。直す場所が違う（ADR 0017）。
	{usecase.ErrForbidden, http.StatusForbidden, apitypes.ErrorCodeForbiddenRole},
	{port.ErrForbidden, http.StatusForbidden, apitypes.ErrorCodeForbiddenProject},

	// セッションが失効した、あるいはトークンを更新できなかった。画面が
	// 「再ログインが要る」と判断できるようにする（ADR 0015）。
	{port.ErrNotAuthenticated, http.StatusUnauthorized, apitypes.ErrorCodeLoginRequired},

	// 409 は「いまの状態では通せない」。やり直せば通るとは限らないので 400 に
	// しない。何をすればよいかは code ごとに違う。
	{usecase.ErrSceneConflict, http.StatusConflict, apitypes.ErrorCodeSceneConflict},
	{usecase.ErrTargetLocked, http.StatusConflict, apitypes.ErrorCodeTargetLocked},
	// 固定とは別。作成先は動かせないが、送られた projectId が古いだけなら
	// 開き直せば解ける（ADR 0037）。
	{usecase.ErrTargetMismatch, http.StatusConflict, apitypes.ErrorCodeTargetMismatch},
	{usecase.ErrContentHashMismatch, http.StatusConflict, apitypes.ErrorCodeContentHashMismatch},
	{usecase.ErrPreviousItemUnknown, http.StatusConflict, apitypes.ErrorCodePreviousItemUnknown},
	{usecase.ErrAlreadyMember, http.StatusConflict, apitypes.ErrorCodeAlreadyMember},
	{usecase.ErrLastOwner, http.StatusConflict, apitypes.ErrorCodeLastOwner},

	// 大きすぎるシーン。400 に畳まない。中身の誤りではなく大きさなので、
	// 打ち手が「送った内容を直す」ではなく「貼った画像を減らす」になる。
	{usecase.ErrSceneTooLarge, http.StatusRequestEntityTooLarge, apitypes.ErrorCodeSceneTooLarge},
	// 積み上がりすぎた会話。**送られた内容は正しい。** 打ち手が「送った内容を
	// 直す」ではなく「会話をやり直す」になるので、400 に畳まない（ADR 0041）。
	{usecase.ErrDiagramChatTooLong, http.StatusRequestEntityTooLarge,
		apitypes.ErrorCodeDiagramChatTooLong},

	// 設定不足であって、リクエストの誤りではない。
	{usecase.ErrTargetNotSelected, http.StatusUnprocessableEntity, apitypes.ErrorCodeTargetNotSelected},
	{usecase.ErrProjectFieldMissing, http.StatusUnprocessableEntity, apitypes.ErrorCodeProjectFieldMissing},

	// 上流の失敗。500 に丸めると、開発者は自分の設定を疑えない。
	{usecase.ErrLLMUnavailable, http.StatusBadGateway, apitypes.ErrorCodeLlmUnavailable},
	{usecase.ErrInterpretationFailed, http.StatusBadGateway, apitypes.ErrorCodeInterpretationFailed},
	{usecase.ErrDiagramFailed, http.StatusBadGateway, apitypes.ErrorCodeDiagramFailed},
	{usecase.ErrCreationIncomplete, http.StatusBadGateway, apitypes.ErrorCodeCreationIncomplete},
}

// lookupError は sentinel を表から引く。載っていなければ false。
func lookupError(err error) (errorMapping, bool) {
	for _, m := range errorMappings {
		if errors.Is(err, m.err) {
			return m, true
		}
	}
	return errorMapping{}, false
}

// respondMapped は表に載っているエラーだけを応答にする。載っていなければ false を
// 返し、既定に落とすかどうかは呼び出し側が決める。
func respondMapped(c *gin.Context, err error) bool {
	m, ok := lookupError(err)
	if !ok {
		return false
	}

	errorJSON(c, m.status, m.code, hintFor(m, err))
	return true
}

// hintFor は本文に載せる手掛かりを返す。
//
// **404 だけは err の文言を載せない。** 非メンバーのボードもここに来るので、
// 本文の違いから存在を読み取れるようにしない（ADR 0016 / 0017）。
func hintFor(m errorMapping, err error) string {
	if m.code == apitypes.ErrorCodeNotFound {
		return "not found"
	}
	return err.Error()
}

// errorJSON はエラー本文を 1 つの型に揃える。gin.H を直に書くと、契約に
// 無いキーが混ざっても気づけない。
//
// **code は引数で受ける。** 任意にすると、足し忘れた経路だけが画面の対応表から
// 漏れて、静かに既定の文言に落ちる。引数なら忘れるとコンパイルエラーになる。
func errorJSON(c *gin.Context, status int, code apitypes.ErrorCode, hint string) {
	c.JSON(status, apitypes.ErrorResponse{Code: code, Error: hint})
}

// 設定不足（503）の応答。**1 つの code に畳まない。** 設定するものが違うので、
// 畳むと画面が「何を設定すればよいか」を言えなくなる。
//
// 404 ではなく 503 なのは、URL の誤りではなく設定の不足だと伝えるため。ルート
// 自体は生やしてある（router.go の Deps を参照）。文言はここにまとめてあり、
// 画面に出るのは code から引いた日本語のほう。

func llmNotConfigured(c *gin.Context) {
	errorJSON(c, http.StatusServiceUnavailable, apitypes.ErrorCodeLlmNotConfigured,
		"llm is not configured: set ETOKI_LLM_API_KEY or ETOKI_LLM_BASE_URL")
}

// githubNotConfigured は GitHub 未設定のときの案内。
//
// 作成先はボードごとに選ぶので、必要なのはトークンだけ（ADR 0014）。
func githubNotConfigured(c *gin.Context) {
	errorJSON(c, http.StatusServiceUnavailable, apitypes.ErrorCodeGithubNotConfigured,
		"github is not configured: set ETOKI_GITHUB_TOKEN")
}

func authNotConfigured(c *gin.Context) {
	errorJSON(c, http.StatusServiceUnavailable, apitypes.ErrorCodeAuthNotConfigured,
		"authentication is not configured: "+
			"set ETOKI_GITHUB_APP_CLIENT_ID and ETOKI_GITHUB_APP_CLIENT_SECRET")
}

// sharingNotConfigured は共有が組み立てられていないときの案内。
//
// 招待は「誰であるか」が決まって初めて意味を持つ。認証を設定していない構成は
// 利用者 1 人なので、共有する相手がいない（ADR 0016 / 0017）。
func sharingNotConfigured(c *gin.Context) {
	errorJSON(c, http.StatusServiceUnavailable, apitypes.ErrorCodeSharingNotConfigured,
		"sharing requires authentication: "+
			"set ETOKI_GITHUB_APP_CLIENT_ID and ETOKI_GITHUB_APP_CLIENT_SECRET")
}
