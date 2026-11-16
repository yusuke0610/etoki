package version

import (
	"runtime/debug"
	"testing"
)

// make が埋めた値を、Go が VCS から埋めた値より先に返す。逆にすると、作業ツリーで
// 作った配布物が外側のチェックアウトの版を名乗る。
func TestString_PrefersTheValueMakeEmbedded(t *testing.T) {
	saved := value
	t.Cleanup(func() { value = saved })

	value = "v1.2.3-4-gabcdef0-dirty"
	if got := String(); got != "v1.2.3-4-gabcdef0-dirty" {
		t.Errorf("String() = %q, want the embedded value", got)
	}
}

// 埋めていなければ、Go のビルド情報の版に落ちる。それも無ければ (devel)。
// 空文字は返さない。報告に空欄が載ると、版を確かめたのかが読めない。
func TestString_FallsBackToBuildInfo(t *testing.T) {
	saved := value
	t.Cleanup(func() { value = saved })

	value = ""
	want := "(devel)"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		want = info.Main.Version
	}
	if got := String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}
