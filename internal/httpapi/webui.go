package httpapi

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// indexFile はブラウザが最初に受け取るページ。
const indexFile = "index.html"

// CheckWebDir は配れる中身があるかを確かめる。
//
// 起動時に落とすために公開している。指定したのに配られない、を黙って通すと、
// 画面が開けない原因がパスの打ち間違いなのか、bun run build を忘れたのか、
// フロントエンド側の不具合なのかを、404 の並びから切り分けることになる。
func CheckWebDir(dir string) error {
	index := filepath.Join(dir, indexFile)
	if _, err := os.Stat(index); err != nil {
		return fmt.Errorf("web dir %q has no %s: %w", dir, indexFile, err)
	}
	return nil
}

// newWebUI はビルド済みフロントエンド（web/dist）を配るハンドラを返す。
// dir が空なら何も配らず、すべて 404 になる。
//
// **未知のパスに index.html を返す SPA の fallback は持たない**（ADR 0032）。
// フロントエンドに client-side routing が無く、ログイン後の戻り先も "/" なので、
// 配る必要があるのは "/" と実在するファイルだけ。先回りして fallback を置くと、
// 打ち間違えた URL が 404 ではなく 200 + HTML で返る。ルーティングを入れる
// ときに一緒に足す。
func newWebUI(dir string) gin.HandlerFunc {
	root := http.Dir(dir)

	return func(c *gin.Context) {
		// /api と /healthz は配信の対象にしない。ここを通すと、API の打ち
		// 間違いが 404 ではなく 200 + HTML で返り、フロント側では「JSON の
		// はずが HTML」という読みにくい失敗になる。
		//
		// dir が空でも同じ応答にする。配っているかどうかで 404 の形が変わると、
		// make dev と配布構成とで画面側の失敗の見え方が変わる。
		if dir == "" || isReservedPath(c.Request.URL.Path) {
			notFound(c)
			return
		}

		// 書き込みのつもりで外したパスに、ページを返さない。
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			notFound(c)
			return
		}

		name := c.Request.URL.Path
		if name == "/" {
			name = "/" + indexFile
		}

		// ".." を含むパスは http.Dir が dir の外に出る前に落とす。自前で
		// 組み立てると、その判断をここでもう一度書くことになる。
		f, err := root.Open(name)
		if err != nil {
			notFound(c)
			return
		}
		defer func() { _ = f.Close() }()

		info, err := f.Stat()
		// ディレクトリは配らない。中身の一覧を出す理由が無い。
		if err != nil || info.IsDir() {
			notFound(c)
			return
		}

		// index.html だけはキャッシュさせない。中身が指す asset の名前は
		// ビルドごとに変わるので、古い index.html が残ると新しい asset を
		// 指さないまま動く。asset 側はファイル名にハッシュが入るため、
		// 止めるのはここだけでよい。
		if name == "/"+indexFile {
			c.Header("Cache-Control", "no-cache")
		}

		http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), f)
	}
}

// notFound は配れなかったときの応答。API の 404 と同じ形に揃える。
func notFound(c *gin.Context) {
	errorJSON(c, http.StatusNotFound, "not found")
}

// isReservedPath は etoki 自身が持つパスかを返す。
func isReservedPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/") || p == "/healthz"
}
