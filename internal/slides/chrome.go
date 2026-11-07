package slides

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// versionsURL は Chrome for Testing の配布一覧 (安定版の chrome-headless-shell の URL が載っている)。
const versionsURL = "https://googlechromelabs.github.io/chrome-for-testing/last-known-good-versions-with-downloads.json"

// installedChromes は OS ごとに、入っていればそのまま使うブラウザ。
var installedChromes = map[string][]string{
	"darwin": {
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
	},
	"linux": {"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge"},
}

// headlessShellPlatforms は Chrome for Testing の配布の名前。
var headlessShellPlatforms = map[string]string{
	"darwin/arm64": "mac-arm64",
	"darwin/amd64": "mac-x64",
	"linux/amd64":  "linux64",
}

// findChrome はスライドを描く Chrome を探す。
// 入っている Chrome を使い、無ければ描画専用の chrome-headless-shell を cache に取ってくる。
func findChrome(cache string, status func(string)) (string, error) {
	if path := os.Getenv("CHROME_PATH"); path != "" {
		return path, nil
	}
	for _, name := range installedChromes[runtime.GOOS] {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	if path, ok := downloadedShell(cache); ok {
		return path, nil
	}
	return downloadShell(cache, status)
}

// downloadedShell は取ってきてある chrome-headless-shell (新しい版)。
func downloadedShell(cache string) (string, bool) {
	found, _ := filepath.Glob(filepath.Join(cache, "*", "chrome-headless-shell-*", "chrome-headless-shell"))
	if len(found) == 0 {
		return "", false
	}
	slices.Sort(found)
	return found[len(found)-1], true
}

type chromeVersions struct {
	Channels map[string]struct {
		Version   string `json:"version"`
		Downloads map[string][]struct {
			Platform string `json:"platform"`
			URL      string `json:"url"`
		} `json:"downloads"`
	} `json:"channels"`
}

func downloadShell(cache string, status func(string)) (string, error) {
	platform, ok := headlessShellPlatforms[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("Chrome が見つかりません。この OS (%s/%s) 向けの描画用 Chrome は配られていないので、Chrome を入れるか CHROME_PATH で場所を指定してください", runtime.GOOS, runtime.GOARCH)
	}
	status("描画用の Chrome を探しています…")
	res, err := http.Get(versionsURL)
	if err != nil {
		return "", fmt.Errorf("描画用の Chrome の配布一覧を取れません: %w", err)
	}
	defer res.Body.Close()
	var versions chromeVersions
	if err := json.NewDecoder(res.Body).Decode(&versions); err != nil {
		return "", fmt.Errorf("描画用の Chrome の配布一覧を読めません: %w", err)
	}
	stable := versions.Channels["Stable"]
	url := ""
	for _, d := range stable.Downloads["chrome-headless-shell"] {
		if d.Platform == platform {
			url = d.URL
		}
	}
	if url == "" {
		return "", fmt.Errorf("描画用の Chrome (%s) の配布が見つかりません", platform)
	}

	archive, err := os.CreateTemp(cache, "download-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	if err := fetch(url, archive, func(percent int) {
		status(fmt.Sprintf("描画用の Chrome (約 100MB) を取ってきています… %d%%", percent))
	}); err != nil {
		return "", fmt.Errorf("描画用の Chrome を取ってこれません: %w", err)
	}
	status("描画用の Chrome を展開しています…")
	final := filepath.Join(cache, stable.Version)
	partial := final + ".partial"
	_ = os.RemoveAll(partial)
	if err := unzip(archive.Name(), partial); err != nil {
		return "", fmt.Errorf("描画用の Chrome を展開できません: %w", err)
	}
	_ = os.RemoveAll(final)
	if err := os.Rename(partial, final); err != nil {
		return "", err
	}
	path, ok := downloadedShell(cache)
	if !ok {
		return "", errors.New("取ってきた描画用の Chrome の中に chrome-headless-shell がありません")
	}
	return path, nil
}

// fetch は url を w に書き、進み具合を 5% ごとに知らせる。
func fetch(url string, w io.Writer, progress func(percent int)) error {
	res, err := http.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, res.Status)
	}
	counter := &progressWriter{total: res.ContentLength, report: progress, last: -1}
	_, err = io.Copy(w, io.TeeReader(res.Body, counter))
	return err
}

type progressWriter struct {
	total, done int64
	last        int
	report      func(int)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.total > 0 {
		if percent := int(p.done * 100 / p.total); percent/5 != p.last/5 {
			p.last = percent
			p.report(percent)
		}
	}
	return len(b), nil
}

// unzip は zip を dir に展開する (実行権限とシンボリックリンクを保つ)。
func unzip(archive, dir string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		target := filepath.Join(dir, f.Name)
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) {
			return fmt.Errorf("zip の中に外を指すパスがあります: %s", f.Name)
		}
		if err := extract(f, target); err != nil {
			return err
		}
	}
	return nil
}

func extract(f *zip.File, target string) error {
	mode := f.Mode()
	if mode.IsDir() {
		return os.MkdirAll(target, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	if mode&os.ModeSymlink != 0 {
		link, err := io.ReadAll(src)
		if err != nil {
			return err
		}
		return os.Symlink(string(link), target)
	}
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm()|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}
