// Package vault は開いたフォルダ (vault) の読み書きと検索。Markdown 以外のテキストファイルも扱う。
package vault

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	maxTextBytes = 5 * 1024 * 1024
	sniffBytes   = 8192
)

var (
	markdownSuffixes = []string{".md", ".markdown", ".mdx"}
	skipDirs         = []string{"node_modules", "__pycache__", "dist", "build", "target"}
)

// ErrNotText はテキストとして開けないファイル (バイナリ・大きすぎる・UTF-8 でない)。
var ErrNotText = errors.New("テキストとして開けません")

func IsMarkdown(path string) bool {
	return path == "" || slices.Contains(markdownSuffixes, strings.ToLower(filepath.Ext(path)))
}

// IsListed はファイル一覧・検索に出すか (隠しファイルや依存物のフォルダは出さない)。
func IsListed(name string) bool {
	return !strings.HasPrefix(name, ".") && !slices.Contains(skipDirs, name)
}

// ReadText はテキストとして読む。無いファイルは空として扱う。
func ReadText(path string) (string, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	name := filepath.Base(path)
	if info.Size() > maxTextBytes {
		return "", fmt.Errorf("%w: %s は大きすぎます (%.1f MB)", ErrNotText, name, float64(info.Size())/1024/1024)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(data[:min(len(data), sniffBytes)], 0) >= 0 {
		return "", fmt.Errorf("%w: %s はバイナリファイルです", ErrNotText, name)
	}
	if !utf8.Valid(data) {
		// Shift_JIS などを UTF-8 で書き戻すと壊れるので、開かない
		return "", fmt.Errorf("%w: %s は UTF-8 のテキストではありません", ErrNotText, name)
	}
	return string(data), nil
}

type Vault struct{ Root string }

func Open(root string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Vault{Root: abs}, nil
}

// Entry はフォルダの中身の1件。
type Entry struct {
	Path  string
	Name  string
	IsDir bool
}

// List はフォルダの直下を、フォルダ→ファイルの順・名前順で返す。
func (v *Vault) List(dir string) []Entry {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var dirs, files []Entry
	for _, item := range items {
		if !IsListed(item.Name()) {
			continue
		}
		e := Entry{filepath.Join(dir, item.Name()), item.Name(), item.IsDir()}
		if e.IsDir {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}
	return append(dirs, files...)
}

// Files は vault 内の全ファイルを順に呼ぶ。visit が false を返したら止める。
func (v *Vault) Files(visit func(path string) bool) {
	stop := errors.New("stop")
	_ = filepath.WalkDir(v.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path != v.Root && !IsListed(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() && !visit(path) {
			return stop
		}
		return nil
	})
}

func (v *Vault) Relative(path string) string {
	rel, err := filepath.Rel(v.Root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

// PathFor はユーザーが入力した名前のパス。拡張子が無ければ .md を付ける。
func (v *Vault) PathFor(name string) string {
	path := filepath.Join(v.Root, strings.TrimSpace(name))
	if filepath.Ext(path) == "" {
		path += ".md"
	}
	return path
}

func (v *Vault) Create(name string) (string, error) {
	path := v.PathFor(name)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s は既にあります", v.Relative(path))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	body := ""
	if IsMarkdown(path) {
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		body = "# " + stem + "\n\n"
	}
	return path, os.WriteFile(path, []byte(body), 0o644)
}

func (v *Vault) Rename(path, name string) (string, error) {
	target := v.PathFor(name)
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("%s は既にあります", v.Relative(target))
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	return target, os.Rename(path, target)
}

type Hit struct {
	Path string
	Line int // ファイル名だけの一致なら -1
	Text string
}

// Search はファイル名と本文を、大文字小文字を区別せずに探す。cancelled が true になったら打ち切る。
func (v *Vault) Search(query string, limit int, cancelled func() bool) []Hit {
	needle := strings.ToLower(query)
	var hits []Hit
	v.Files(func(path string) bool {
		if cancelled() {
			return false
		}
		if strings.Contains(strings.ToLower(v.Relative(path)), needle) {
			hits = append(hits, Hit{path, -1, ""})
		}
		text, err := ReadText(path)
		if err != nil {
			return true
		}
		for i, line := range strings.Split(text, "\n") {
			if strings.Contains(strings.ToLower(line), needle) {
				hits = append(hits, Hit{path, i, strings.TrimSpace(line)})
			}
		}
		return len(hits) < limit
	})
	return hits[:min(len(hits), limit)]
}
