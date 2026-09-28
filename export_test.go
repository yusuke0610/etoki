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

// ShutdownBudgetForTest は停止の猶予と、リクエストを切るまでの長さを返す。
//
// 実際に 75 秒待つテストは書けないので、**猶予が作成の後始末を覆っていること**
// だけを定数どうしの関係として固定する（ADR 0056）。
func ShutdownBudgetForTest() (shutdown, cancelAfter time.Duration) {
	return shutdownTimeout, cancelRequestsAfter
}
