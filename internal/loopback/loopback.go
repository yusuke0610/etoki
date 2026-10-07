// Package loopback はホスト名がループバックを指すかを判定する。
//
// 平文の http を手元にだけ許す判定に使う。外へ出る呼び出し（GitHub と LLM の
// アダプタ）と、受けたリクエストの Host の検証（httpapi）の両方が読む。
// **判定を書き写さないためにここに置く。** 写すと片方だけ緩む。アダプタが
// httpapi に依存する向きも作らずに済む。
package loopback

import (
	"net"
	"strings"
)

// Hostname はホスト名がループバックを指すかを返す。
//
// **受けるのはポートも角括弧も付かないホスト名だけ**（`url.URL.Hostname()` が
// 返す形）。"127.0.0.1:8080" や "[::1]" は false になる。剥がすのは呼び出し側。
//
// 名前で通すのは "localhost" だけ。他の名前は解決しない。解決すると、判定の
// 結果が DNS の答えに依ることになる。
func Hostname(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
