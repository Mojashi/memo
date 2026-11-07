// Package slides は Marp のスライドを marp-cli で PNG にする。
package slides

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// renderTimeout は1回の書き出しを待つ上限 (marp-cli は中で Chrome を起動する)。
const renderTimeout = 90 * time.Second

// ErrNoMarp は marp-cli が入っていないとき。
var ErrNoMarp = errors.New("marp が見つかりません。\n入れるとスライドを画像で確認できます:\n\n  npm i -g @marp-team/marp-cli")

// Marp は marp-cli のコマンド。
type Marp struct{ path string }

// Find は PATH から marp-cli を探す。
func Find() (Marp, error) {
	path, err := exec.LookPath("marp")
	if err != nil {
		return Marp{}, ErrNoMarp
	}
	return Marp{path}, nil
}

// Render は Markdown のソースを1枚ずつの PNG にして out フォルダに書き、そのパスをスライド順に返す。
// 画像などの相対パスは dir から探す。
func (m Marp) Render(source, dir, out string) ([]string, error) {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), renderTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, m.path, "--images", "png", "--allow-local-files", "-o", filepath.Join(out, "slide.png"), "-")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(source)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("marp でスライドを描けません: %w\n%s", err, strings.TrimSpace(stderr.String()))
	}
	pages, err := filepath.Glob(filepath.Join(out, "slide.*.png"))
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("marp が画像を書き出しませんでした\n%s", strings.TrimSpace(stderr.String()))
	}
	return pages, nil // slide.001.png, slide.002.png, … の名前順 = スライド順
}
