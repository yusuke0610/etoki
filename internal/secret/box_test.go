package secret_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/secret"
)

func newBox(t *testing.T, seed byte) secret.Box {
	t.Helper()

	key := make([]byte, secret.KeySize)
	for i := range key {
		key[i] = seed + byte(i)
	}

	b, err := secret.New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return b
}

func TestSealOpen_RoundTrip(t *testing.T) {
	t.Parallel()

	b := newBox(t, 1)

	const token = "ghu_abcdefghijklmnopqrstuvwxyz0123456789"
	sealed, err := b.Seal(token)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if strings.Contains(sealed, token) {
		t.Fatal("封をした値に平文が残っている")
	}

	got, err := b.Open(sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != token {
		t.Errorf("Open = %q, want %q", got, token)
	}
}

// nonce は毎回変える。同じ値が同じ暗号文になると、保存先を見ただけで
// 「2 人が同じトークンを持っている」が分かってしまう。
func TestSeal_IsNotDeterministic(t *testing.T) {
	t.Parallel()

	b := newBox(t, 1)

	first, err := b.Seal("same")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	second, err := b.Seal("same")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if first == second {
		t.Error("同じ値が同じ暗号文になっている")
	}
}

func TestOpen_RejectsOtherKey(t *testing.T) {
	t.Parallel()

	sealed, err := newBox(t, 1).Seal("secret")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if _, err := newBox(t, 99).Open(sealed); !errors.Is(err, secret.ErrMalformed) {
		t.Fatalf("Open = %v, want ErrMalformed", err)
	}
}

// GCM は改竄を検出する。1 バイト変えたら開かない。
func TestOpen_RejectsTamperedValue(t *testing.T) {
	t.Parallel()

	b := newBox(t, 1)

	sealed, err := b.Seal("secret")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw[len(raw)-1] ^= 0xff

	if _, err := b.Open(base64.RawURLEncoding.EncodeToString(raw)); !errors.Is(err, secret.ErrMalformed) {
		t.Fatalf("Open = %v, want ErrMalformed", err)
	}
}

func TestOpen_RejectsGarbage(t *testing.T) {
	t.Parallel()

	b := newBox(t, 1)

	for _, in := range []string{"not base64 !!!", "c2hvcnQ"} {
		if _, err := b.Open(in); !errors.Is(err, secret.ErrMalformed) {
			t.Errorf("Open(%q) = %v, want ErrMalformed", in, err)
		}
	}
}

// 失効しない構成では refresh token が空になる。空に封をしても意味が無い。
func TestSealOpen_PassesEmptyThrough(t *testing.T) {
	t.Parallel()

	b := newBox(t, 1)

	sealed, err := b.Seal("")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if sealed != "" {
		t.Errorf("Seal(\"\") = %q, want 空文字", sealed)
	}

	got, err := b.Open("")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != "" {
		t.Errorf("Open(\"\") = %q, want 空文字", got)
	}
}

func TestNew_RejectsWrongKeySize(t *testing.T) {
	t.Parallel()

	for _, n := range []int{0, 16, 31, 33} {
		if _, err := secret.New(make([]byte, n)); !errors.Is(err, secret.ErrKeySize) {
			t.Errorf("New(%d バイト) = %v, want ErrKeySize", n, err)
		}
	}
}

// 環境変数から読む都合で、利用者がどの流儀で base64 にするか読めない。
func TestDecodeKey_AcceptsEveryBase64Flavour(t *testing.T) {
	t.Parallel()

	key := make([]byte, secret.KeySize)
	for i := range key {
		key[i] = byte(i) | 0xf0 // + と / が出るように寄せる
	}

	for name, enc := range map[string]*base64.Encoding{
		"標準":         base64.StdEncoding,
		"標準パディング無し":  base64.RawStdEncoding,
		"URL":        base64.URLEncoding,
		"URLパディング無し": base64.RawURLEncoding,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := secret.DecodeKey(enc.EncodeToString(key))
			if err != nil {
				t.Fatalf("DecodeKey: %v", err)
			}
			if string(got) != string(key) {
				t.Error("鍵が復元できていない")
			}
		})
	}
}

func TestDecodeKey_RejectsWrongLength(t *testing.T) {
	t.Parallel()

	short := base64.StdEncoding.EncodeToString(make([]byte, 16))
	if _, err := secret.DecodeKey(short); !errors.Is(err, secret.ErrKeySize) {
		t.Fatalf("DecodeKey = %v, want ErrKeySize", err)
	}
}
