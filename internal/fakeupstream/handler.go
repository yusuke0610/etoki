// Package fakeupstream は、etoki を手元で通しで動かすための偽の LLM と偽の
// GitHub を 1 つの HTTP サーバーとして提供する（ADR 0050）。
//
// 本番のコードからは使わない。追うのは本物の API ではなく etoki のアダプタが
// 送るリクエストで、ずれは往復テストで検知する。
package fakeupstream

import (
	"encoding/json"
	"net/http"
)

// Server は偽の上流 1 台ぶん。状態はメモリにだけ持ち、再起動で消える。
type Server struct {
	github github
	mux    *http.ServeMux
}

// New は空の状態で Server を作る。
func New() *Server {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /v1/messages", handleMessages)
	s.mux.HandleFunc("POST /graphql", s.github.handleGraphQL)
	s.mux.HandleFunc("GET /_fake/items", s.handleItems)
	return s
}

// ServeHTTP は http.Handler を満たす。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Items は偽の GitHub に積まれた draft issue を作った順に返す。
func (s *Server) Items() []Item {
	return s.github.snapshot()
}

// handleItems は積まれた draft issue を返す。作れたか・更新されたかを画面の
// 外で確かめるための口。
func (s *Server) handleItems(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Items())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
