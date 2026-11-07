package ui

import (
	"errors"
	"fmt"
	"image"
	_ "image/png" // 撮った HTML のページの大きさを読む
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/Mojashi/memo/internal/graphics"
	"github.com/Mojashi/memo/internal/theme"
)

// pageSuffixes はブラウザで描いて見るファイル (HTML と SVG)。
var pageSuffixes = []string{".html", ".htm", ".svg"}

func isPage(path string) bool {
	return slices.Contains(pageSuffixes, strings.ToLower(filepath.Ext(path)))
}

// isViewable は編集せずに見るファイル (画像・HTML・SVG) か。
func isViewable(path string) bool { return graphics.IsImage(path) || isPage(path) }

// viewer は画像や HTML のファイルを、エディタの代わりに見せる。
//
//   - 画像は縦横比を保って画面に収める
//   - HTML と SVG は描画サーバーの Chrome で撮った静止画 (操作はできない)。縦に長いページは
//     1280×720 ずつに切ったものを並べ、↑↓ でめくる
//   - 「↗ 開く」(または o) で、OS の既定のアプリ (ブラウザなど) で開く
type viewer struct {
	path string
	page bool

	image graphics.ImageFile // 画像

	chunks    []string // HTML: 上から順の画像
	heights   []int    // HTML: 各画像の高さ (ピクセル)
	top       int      // HTML: 画面の一番上に出す画像
	rendering bool
	progress  string

	err        error
	openButton rect
}

// openViewer は path を見る準備をする。HTML は描画サーバーに頼み、描けたら post で受け取る。
func openViewer(path string, render *renderService, post func(func())) *viewer {
	v := &viewer{path: path, page: isPage(path)}
	if !v.page {
		cache, err := imageCache()
		if err == nil {
			v.image, err = graphics.LoadImage(path, cache)
		}
		v.err = err
		return v
	}
	r, err := render.get()
	if err != nil {
		v.err = err
		return v
	}
	v.rendering = true
	go func() {
		chunks, err := r.RenderPage(path, func(message string) {
			post(func() { v.progress = message })
		})
		heights := make([]int, len(chunks))
		for i, chunk := range chunks {
			heights[i] = pngHeight(chunk)
		}
		post(func() {
			v.rendering, v.progress = false, ""
			v.chunks, v.heights, v.err = chunks, heights, err
		})
	}()
	return v
}

func imageCache() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("キャッシュフォルダが分かりません: %w", err)
	}
	return filepath.Join(cache, "memo", "images"), nil
}

func pngHeight(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 720
	}
	defer f.Close()
	config, _, err := image.DecodeConfig(f)
	if err != nil {
		return 720
	}
	return config.Height
}

// scroll は HTML のページを n 枚ぶんめくる。
func (v *viewer) scroll(n int) {
	v.top = max(0, min(v.top+n, len(v.chunks)-1))
}

// info は見出しに出す説明 (大きさなど)。
func (v *viewer) info() string {
	switch {
	case v.page && len(v.chunks) > 0:
		return fmt.Sprintf("ブラウザで描いた静止画  %d / %d 画面", v.top+1, len(v.chunks))
	case v.page:
		return "ブラウザで描いた静止画"
	case v.err == nil:
		return fmt.Sprintf("%s  %d × %d  %s", strings.ToUpper(v.image.Format), v.image.Width, v.image.Height, byteSize(v.image.Bytes))
	}
	return ""
}

func byteSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func (v *viewer) draw(s tcell.Screen, r rect, t theme.Theme, images *imageLayer) {
	base := t.Text()
	fill(s, r, base)
	header := rect{r.x + 2, r.y, r.w - 4, 1}
	button := " ↗ 開く "
	bw := textWidth(button)
	v.openButton = rect{header.x + header.w - bw, header.y, bw, 1}
	put(s, v.openButton.x, v.openButton.y, bw, button, theme.Style{Fg: t.Background, Bg: t.Primary, Bold: true})
	putSegments(s, header.x, header.y, header.w-bw-1, base,
		segment{filepath.Base(v.path), theme.Style{Fg: t.Accent, Bold: true}},
		segment{"  " + v.info(), theme.Style{Fg: t.Muted()}})

	body := rect{r.x + 2, r.y + 2, r.w - 4, r.h - 2}
	message := v.err
	if message == nil {
		message = images.problem()
	}
	switch {
	case message != nil:
	case v.rendering && v.progress != "":
		message = errors.New(v.progress)
	case v.rendering:
		message = errors.New("ブラウザで描いています…")
	case v.page:
		v.drawPage(s, body, base, images)
	default:
		cols, rows := fitImage(v.image.Width, v.image.Height, body.w, body.h)
		images.place(s, rect{body.x + (body.w-cols)/2, body.y, cols, rows}, fileImageID, v.image.PNG, base)
	}
	if message != nil {
		for i, line := range wrapText(message.Error(), body.w) {
			if i < body.h {
				put(s, body.x, body.y+i, body.w, line, base.Over(theme.Style{Fg: t.Muted()}))
			}
		}
	}
}

// drawPage は HTML のページを、top 枚目から画面に入るだけ縦に並べる。
func (v *viewer) drawPage(s tcell.Screen, body rect, base theme.Style, images *imageLayer) {
	y := body.y
	for k, i := 0, v.top; i < len(v.chunks) && k < maxPageSlots; k, i = k+1, i+1 {
		cols, rows := fitImage(1280, v.heights[i], body.w, body.y+body.h-y)
		if rows <= 0 || y+rows > body.y+body.h {
			break
		}
		images.place(s, rect{body.x + (body.w-cols)/2, y, cols, rows}, pageImageID+k, v.chunks[i], base)
		y += rows
	}
}

// openExternally は OS の既定のアプリでファイルを開く (テストで差し替える)。
var openExternally = openWithApp

func openWithApp(path string) error {
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}
	cmd := exec.Command(command, path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s を開けません: %w", filepath.Base(path), err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
