package etoki

import (
	"net/http"
	"time"
)

// NewServerForTest は任意のハンドラと停止の猶予で Server を組み立てる。
//
// 停止の途中でリクエストの ctx が切れることを確かめるには、ctx を待って
// 止まるハンドラと、秒単位ではない猶予が要る。どちらも Options からは
// 差し込めない。
func NewServerForTest(addr string, h http.Handler, shutdown, cancelAfter time.Duration) *Server {
	return &Server{
		addr:                addr,
		handler:             h,
		shutdownTimeout:     shutdown,
		cancelRequestsAfter: cancelAfter,
	}
}
