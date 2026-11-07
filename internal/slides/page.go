package slides

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"net/url"
	"os"
	"path/filepath"

	"github.com/chromedp/chromedp"
)

const (
	pageWidth     = 1280
	pageChunk     = 720 // HTML のページを、この高さごとの画像に切る
	maxPageChunks = 40  // これより下は描かない (とても長いページ)
)

// renderPage は HTML (や SVG) のファイルを幅 1280px のブラウザで開いて撮り、
// 上から 1280×720 ずつに切った PNG のパスを返す。スクリプトは動くが、操作はできない静止画。
// ファイルが変わっていなければ前に撮ったものを返す。
func (t *tab) renderPage(file string, status func(string)) ([]string, error) {
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}
	sum := sha1.Sum(fmt.Appendf(nil, "%s\x00%d\x00%d", file, info.Size(), info.ModTime().UnixNano()))
	name := "page-" + hex.EncodeToString(sum[:10])
	index := filepath.Join(t.browser.paths.Images(), name+".json")
	if data, err := os.ReadFile(index); err == nil {
		var chunks []string
		if json.Unmarshal(data, &chunks) == nil {
			return chunks, nil
		}
	}

	if err := t.open(status); err != nil {
		return nil, err
	}
	location := url.URL{Scheme: "file", Path: file}
	if err := chromedp.Do(t.ctx,
		chromedp.EmulateViewport(pageWidth, pageChunk),
		chromedp.Navigate(location.String()),
		chromedp.Evaluate[chromedp.Void]("document.fonts.ready", chromedp.EvalAwaitPromise),
	); err != nil {
		return nil, fmt.Errorf("%s を開けません: %w", filepath.Base(file), err)
	}
	shot, err := chromedp.Run(t.ctx, chromedp.FullScreenshot(100))
	if err != nil {
		return nil, fmt.Errorf("%s を撮れません: %w", filepath.Base(file), err)
	}
	full, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		return nil, err
	}

	var chunks []string
	bounds := full.Bounds()
	for top := bounds.Min.Y; top < bounds.Max.Y && len(chunks) < maxPageChunks; top += pageChunk {
		part := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), min(pageChunk, bounds.Max.Y-top)))
		draw.Draw(part, part.Bounds(), full, image.Pt(bounds.Min.X, top), draw.Src)
		var buf bytes.Buffer
		if err := png.Encode(&buf, part); err != nil {
			return nil, err
		}
		chunk := filepath.Join(t.browser.paths.Images(), fmt.Sprintf("%s-%03d.png", name, len(chunks)))
		if err := writeAtomically(chunk, buf.Bytes()); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	data, err := json.Marshal(chunks)
	if err != nil {
		return nil, err
	}
	return chunks, writeAtomically(index, data) // 全部書けてから、撮り終えた印を置く
}
