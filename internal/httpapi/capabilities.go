package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
)

// getCapabilities はいま使える機能を返す。
//
// **押した後にしか分からない 503 を、押す前に見せるための口**（ADR 0008 で
// 「LLM を設定しなくても起動する」と決めた帰結）。判定材料は各エンドポイントと
// 同じ `handlers` の nil なので、ここが true なのに 503 が返ることはない。
//
// **利用者ごとの権限は返さない。** そちらはボード単位で
// `GET /api/boards/{id}/access` が返す（ADR 0017）。プロセスの設定と利用者の
// 権限を 1 つに畳むと、「GitHub は設定されているが自分は書けない」を表現できない。
func (h *handlers) getCapabilities(c *gin.Context) {
	c.JSON(http.StatusOK, apitypes.Capabilities{
		Interpretation: h.interpretations != nil,
		// **解釈と畳まない。** 未設定の理由は同じ LLM でも、答えている問いが
		// 違う。畳むと、画面はどちらのボタンを止めればよいのかをこの値から
		// 決められなくなる。材料は生成のエンドポイントが 503 になるものそのもの。
		DiagramDraft: h.diagrams != nil,
		// 作成先の候補（catalog）も同じ GitHub の設定で決まる。片方だけ nil に
		// なる配線は cmd/etoki には無いので、1 つにまとめて返す。
		Creation: h.creations != nil && h.catalog != nil,
		// 見るのは h.members だけ。**共有の 4 つのエンドポイントが 503 に
		// なる材料がそれだから。** h.access は別の口（/boards/{id}/access）の
		// 材料で、ここに混ぜると「共有は使えない」と案内したのに /members は
		// 成功する、という食い違いを作れる。
		Sharing: h.members != nil,
	})
}
