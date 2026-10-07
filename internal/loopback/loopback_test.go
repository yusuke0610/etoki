package loopback_test

import (
	"testing"

	"github.com/yusuke0610/etoki/internal/loopback"
)

func TestHostname(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"localhost":   true,
		"LOCALHOST":   true,
		"127.0.0.1":   true,
		"127.1.2.3":   true,
		"::1":         true,
		"example.com": false,
		"10.0.0.1":    false,
		"192.168.1.1": false,
		// 前方一致で読まない。localhost で始まる別のホストは外。
		"localhost.example.com": false,
		"127.0.0.1.example.com": false,
		// ポートや角括弧つきは受けない。剥がすのは呼び出し側（httpapi.IsLoopbackHost）。
		"127.0.0.1:8080": false,
		"[::1]":          false,
		"":               false,
	}

	for host, want := range tests {
		t.Run(host, func(t *testing.T) {
			t.Parallel()

			if got := loopback.Hostname(host); got != want {
				t.Errorf("Hostname(%q) = %v, want %v", host, got, want)
			}
		})
	}
}
