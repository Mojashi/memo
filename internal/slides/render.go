package slides

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/chromedp/chromedp"

	_ "embed"
)

// marp.js は marp-core をブラウザ用の1ファイルにしたもの (tools/marp-bundle で作る)。
//
//go:embed marp.js
var marpScript []byte

// shellPage はスライドを描くページ。marp.js で Markdown を HTML にし、1枚ずつ並べて撮る。
const shellPage = `<!doctype html>
<html><head><meta charset="utf-8"><base id="base">
<style id="theme"></style>
<style>
  html, body { margin: 0; padding: 0; background: transparent; }
  svg[data-marpit-svg] { display: block; width: 1280px; height: 720px; }
</style>
<script src="marp.js"></script>
<script>
  // memoShow はスライドを並べ、画像とフォントを読み終えるまで待つ。
  window.memoShow = async (base, css, slides) => {
    document.getElementById('base').href = base
    document.getElementById('theme').textContent = css
    document.body.innerHTML = slides.map((s, i) => '<div class="marpit" id="s' + i + '">' + s + '</div>').join('')
    const urls = []
    for (const el of document.querySelectorAll('*')) {
      const bg = getComputedStyle(el).backgroundImage
      for (const m of bg.matchAll(/url\("?([^")]+)"?\)/g)) urls.push(m[1])
    }
    const images = [...document.images].map((img) => img.decode().catch(() => {}))
    const backgrounds = urls.map((src) => { const img = new Image(); img.src = src; return img.decode().catch(() => {}) })
    await Promise.all([...images, ...backgrounds])
    await document.fonts.ready
  }
</script>
</head><body></body></html>`

// browser は起動したままの Chrome。最初に使うときに起動する。
type browser struct {
	paths Paths

	mu     sync.Mutex
	ctx    context.Context // Chrome 本体 (タブはここから開く)
	cancel context.CancelFunc
}

// start は Chrome を (止まっていれば) 起動する。
func (b *browser) start(status func(string)) (context.Context, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx != nil && b.ctx.Err() == nil {
		return b.ctx, nil
	}
	if err := os.MkdirAll(b.paths.Images(), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(b.paths.Dir, "marp.js"), marpScript, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(b.paths.page(), []byte(shellPage), 0o644); err != nil {
		return nil, err
	}
	path, err := findChrome(b.paths.Chrome(), status)
	if err != nil {
		return nil, err
	}
	status("Chrome を起動しています…")
	options := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(path),
		chromedp.Flag("allow-file-access-from-files", true),
		chromedp.Flag("hide-scrollbars", true),
	)
	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	ctx, cancelBrowser := chromedp.NewContext(allocator)
	if err := chromedp.Do(ctx); err != nil { // Chrome を起動する
		cancelBrowser()
		cancelAllocator()
		return nil, fmt.Errorf("Chrome (%s) を起動できません: %w", path, err)
	}
	b.ctx = ctx
	b.cancel = func() { cancelBrowser(); cancelAllocator() }
	return ctx, nil
}

func (b *browser) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil {
		b.cancel()
		b.ctx, b.cancel = nil, nil
	}
}

// tab は1つの memo が使うタブ。
type tab struct {
	browser *browser
	ctx     context.Context
	cancel  context.CancelFunc
}

func (t *tab) open(status func(string)) error {
	if t.ctx != nil && t.ctx.Err() == nil {
		return nil
	}
	parent, err := t.browser.start(status)
	if err != nil {
		return err
	}
	ctx, cancel := chromedp.NewContext(parent)
	page := url.URL{Scheme: "file", Path: t.browser.paths.page()}
	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(1280, 720),
		chromedp.Navigate(page.String()),
	); err != nil {
		cancel()
		return fmt.Errorf("スライドを描くページを開けません: %w", err)
	}
	t.ctx, t.cancel = ctx, cancel
	return nil
}

func (t *tab) close() {
	if t.cancel != nil {
		t.cancel()
	}
}

type marpOutput struct {
	Slides []string `json:"slides"`
	CSS    string   `json:"css"`
}

// render は Markdown を1枚ずつの PNG にし、そのパスをスライド順に返す。
// PNG の名前はスライドの中身から決めるので、前と同じスライドは描き直さない。
func (t *tab) render(markdown, dir string, status func(string)) ([]string, error) {
	if err := t.open(status); err != nil {
		return nil, err
	}
	out, err := chromedp.Run(t.ctx, chromedp.Evaluate[marpOutput]("renderMarp("+jsString(markdown)+")"))
	if err != nil {
		return nil, fmt.Errorf("Marp で HTML にできません: %w", err)
	}
	pages := make([]string, len(out.Slides))
	var missing []int
	for i, slide := range out.Slides {
		sum := sha1.Sum([]byte(out.CSS + "\x00" + dir + "\x00" + slide))
		pages[i] = filepath.Join(t.browser.paths.Images(), hex.EncodeToString(sum[:10])+".png")
		if _, err := os.Stat(pages[i]); err != nil {
			missing = append(missing, i)
		}
	}
	if len(missing) == 0 {
		return pages, nil
	}
	slides := make([]string, len(missing))
	for k, i := range missing {
		slides[k] = out.Slides[i]
	}
	base := url.URL{Scheme: "file", Path: dir + "/"}
	show := fmt.Sprintf("memoShow(%s, %s, %s)", jsString(base.String()), jsString(out.CSS), jsValue(slides))
	if err := chromedp.Do(t.ctx, chromedp.Evaluate[chromedp.Void](show, chromedp.EvalAwaitPromise)); err != nil {
		return nil, fmt.Errorf("スライドを並べられません: %w", err)
	}
	for k, i := range missing {
		png, err := chromedp.Run(t.ctx, chromedp.Screenshot(chromedp.CSS(fmt.Sprintf("#s%d > svg", k))))
		if err != nil {
			return nil, fmt.Errorf("%d 枚目を撮れません: %w", i+1, err)
		}
		if err := writeAtomically(pages[i], png); err != nil {
			return nil, err
		}
	}
	return pages, nil
}

func jsString(s string) string { return jsValue(s) }

func jsValue(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // 文字列と文字列の配列しか渡さない
	}
	return string(b)
}

// writeAtomically は他の memo が読みかけでも壊れないよう、別名で書いてから置き換える。
func writeAtomically(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*.png")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
