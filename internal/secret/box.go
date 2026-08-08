// Package secret は保存する資格情報の封をする。
//
// GitHub のトークンを DB に平文で置かないためだけの、最小の道具（ADR 0015）。
// 鍵の配布・ローテーションは扱わない。
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// KeySize は鍵の長さ。AES-256 を使う。
const KeySize = 32

// ErrKeySize は鍵の長さが違うことを表す。
var ErrKeySize = fmt.Errorf("secret: key must be %d bytes", KeySize)

// ErrNotInitialized はゼロ値の Box を使おうとしたことを表す。
//
// panic ではなくエラーにする。配線を間違えたときに、暗号化まわりの nil 参照
// ではなく「鍵が無い」と読める形で落ちてほしい。
var ErrNotInitialized = errors.New("secret: box is not initialized")

// ErrMalformed は封を開けられないことを表す。
//
// 鍵違い・改竄・そもそも封でない、を区別しない。区別できる情報を返すと、
// 総当たりの手掛かりになる。
var ErrMalformed = errors.New("secret: cannot open sealed value")

// Box は値に封をする。ゼロ値は使えない。New で作る。
type Box struct {
	aead cipher.AEAD
}

// New は鍵から Box を作る。鍵は KeySize バイトちょうど。
func New(key []byte) (Box, error) {
	if len(key) != KeySize {
		return Box{}, fmt.Errorf("%w (got %d)", ErrKeySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return Box{}, fmt.Errorf("secret: new cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Box{}, fmt.Errorf("secret: new gcm: %w", err)
	}

	return Box{aead: aead}, nil
}

// DecodeKey は base64（標準・URL いずれも、パディングの有無も問わない）の
// 鍵を解く。
//
// 環境変数から読む都合で、利用者がどの流儀で base64 にするか読めない。
// ここで受け付ける幅を広げておかないと、「鍵の長さが違う」という分かりにくい
// 失敗として現れる。
func DecodeKey(raw string) ([]byte, error) {
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if key, err := enc.DecodeString(raw); err == nil && len(key) == KeySize {
			return key, nil
		}
	}
	return nil, fmt.Errorf("%w: expected base64 of %d bytes", ErrKeySize, KeySize)
}

// Seal は値に封をする。空文字は空文字のまま返す。
//
// 空を素通しするのは、失効しない構成で refresh token が空になるため。
// 空に封をしても意味が無く、開ける側で空判定が要らなくなる。
func (b Box) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if b.aead == nil {
		return "", ErrNotInitialized
	}

	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secret: read nonce: %w", err)
	}

	// nonce を前に置く。開けるときに同じ鍵と nonce が要るが、nonce は秘密では
	// ない。1 つの値にまとめておくと、保存先が 1 カラムで済む。
	sealed := b.aead.Seal(nonce, nonce, []byte(plain), nil)

	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open は封を開ける。空文字は空文字のまま返す。
func (b Box) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	if b.aead == nil {
		return "", ErrNotInitialized
	}

	raw, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		return "", ErrMalformed
	}

	n := b.aead.NonceSize()
	if len(raw) < n {
		return "", ErrMalformed
	}

	plain, err := b.aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", ErrMalformed
	}

	return string(plain), nil
}
