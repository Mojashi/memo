// Package slides は Marp のスライドを PNG にする。
//
// 描画は、起動しっぱなしの Chrome を持つ「描画サーバー」が受け持つ。
//
//   - 描画サーバーは memo 自身を `memo --slide-server` で起動したもの。1台に1つだけ動き、
//     いくつ memo を開いても共有する (Unix ソケットでつなぐ)
//   - Chrome は入っているものを使い、無ければ描画専用の chrome-headless-shell を自動で取ってくる
//   - Markdown をスライドにするのは、埋め込んだ marp-core (marp.js) を Chrome のページで動かして行う。
//     Node や marp-cli は要らない
//   - スライドの PNG は中身から名前を決めて共有の置き場に書く。同じスライドは描き直さない
//   - HTML (や SVG) のファイルも、同じ Chrome で開いて静止画にする
//   - 誰もつながっていない状態がしばらく続くと、描画サーバーは Chrome ごと終わる
package slides

import (
	"fmt"
	"os"
	"path/filepath"
)

// protocolVersion は memo と描画サーバーのやりとりの版。変えると、古い memo が起動した描画サーバーとは別のものを使う。
const protocolVersion = "2"

// Paths は描画サーバーが使うファイルの置き場。
type Paths struct{ Dir string }

// DefaultPaths はユーザーのキャッシュフォルダの中 (macOS なら ~/Library/Caches/memo/slides)。
func DefaultPaths() (Paths, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, fmt.Errorf("キャッシュフォルダが分かりません: %w", err)
	}
	return Paths{filepath.Join(cache, "memo", "slides")}, nil
}

func (p Paths) Socket() string { return filepath.Join(p.Dir, "server-"+protocolVersion+".sock") }
func (p Paths) Lock() string   { return filepath.Join(p.Dir, "server-"+protocolVersion+".lock") }
func (p Paths) Log() string    { return filepath.Join(p.Dir, "server.log") }
func (p Paths) Images() string { return filepath.Join(p.Dir, "images") }
func (p Paths) Chrome() string { return filepath.Join(p.Dir, "chrome") }
func (p Paths) page() string   { return filepath.Join(p.Dir, "page.html") }

// request は memo から描画サーバーへの依頼 (1行の JSON)。Page があれば HTML のページ、なければ Marp のスライド。
type request struct {
	Markdown string `json:"markdown,omitempty"`
	Dir      string `json:"dir,omitempty"`  // 画像などの相対パスの基準
	Page     string `json:"page,omitempty"` // 描く HTML (や SVG) のファイル
}

// reply は描画サーバーからの返事。Status だけの行は途中経過で、Pages か Error の行で終わる。
type reply struct {
	Status string   `json:"status,omitempty"`
	Pages  []string `json:"pages,omitempty"`
	Error  string   `json:"error,omitempty"`
}

func (r reply) final() bool { return r.Status == "" }
