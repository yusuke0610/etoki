// Package version は、動いている etoki の版を持つ（ADR 0099）。
//
// 不具合の報告で「どの版の話か」を言えるようにするため、`etoki version` と
// `/healthz` がここを読む。
package version

import "runtime/debug"

// value は make がビルド時に埋める（`-ldflags "-X …/internal/version.value=…"`）。
// 中身は `git describe --tags --always --dirty`。
var value string

// String は版を返す。
//
// make が埋めていなければ、Go がビルド時に VCS から埋めた値を返す。**こちらは
// 控えにだけ使う。** 作業ツリー（`.claude/worktrees/`）の中でビルドすると、Go は
// `.git` がファイルの作業ツリーを VCS の根と見なさず、外側のチェックアウトの版を
// 埋める。どちらも無ければ `(devel)`（`go test` や `go run` など）。
func String() string {
	if value != "" {
		return value
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}
