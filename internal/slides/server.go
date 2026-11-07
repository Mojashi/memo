package slides

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// idleTimeout は誰もつながっていない描画サーバーが終わるまでの時間。
	idleTimeout = 5 * time.Minute
	// imageLifetime は使われていないスライドの PNG を残しておく期間。
	imageLifetime = 7 * 24 * time.Hour
)

// ErrServerRunning は描画サーバーがもう動いているとき。
var ErrServerRunning = errors.New("描画サーバーはもう動いています")

// Serve は描画サーバーとして動く。誰もつながらない状態が idleTimeout 続くと戻る。
func Serve(paths Paths) error { return serve(paths, idleTimeout) }

func serve(paths Paths, idleTimeout time.Duration) error {
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return err
	}
	unlock, err := lockExclusive(paths.Lock())
	if err != nil {
		return err
	}
	defer unlock()
	_ = os.Remove(paths.Socket()) // 前の描画サーバーが残したもの (ロックが取れたので誰も使っていない)
	listener, err := net.Listen("unix", paths.Socket())
	if err != nil {
		return err
	}
	defer os.Remove(paths.Socket())
	removeOldImages(paths.Images())

	s := &server{browser: &browser{paths: paths}, listener: listener, idleTimeout: idleTimeout}
	defer s.browser.close()
	s.idle()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return nil // idle で閉じた
		}
		s.connected(+1)
		go func() {
			defer s.connected(-1)
			s.serve(conn)
		}()
	}
}

type server struct {
	browser     *browser
	listener    net.Listener
	idleTimeout time.Duration

	mu      sync.Mutex
	clients int
	timer   *time.Timer
}

// connected はつながっている memo の数を増減し、0 になったら終わるまでの時計を動かす。
func (s *server) connected(delta int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients += delta
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.clients == 0 {
		s.idleLocked()
	}
}

func (s *server) idle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idleLocked()
}

func (s *server) idleLocked() {
	s.timer = time.AfterFunc(s.idleTimeout, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.clients == 0 {
			s.listener.Close()
		}
	})
}

// serve は1つの memo とのやりとり。memo ごとにタブを1つ使う。
func (s *server) serve(conn net.Conn) {
	defer conn.Close()
	t := &tab{browser: s.browser}
	defer t.close()
	pages := &tab{browser: s.browser, blank: true}
	defer pages.close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	encoder := json.NewEncoder(conn)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			_ = encoder.Encode(reply{Error: "依頼を読めません: " + err.Error()})
			continue
		}
		status := func(message string) { _ = encoder.Encode(reply{Status: message}) }
		var images []string
		var err error
		if req.Page != "" {
			images, err = pages.renderPage(req.Page, status)
		} else {
			images, err = t.render(req.Markdown, req.Dir, status)
		}
		if err != nil {
			t.close() // 次の依頼でタブを開き直す
			pages.close()
			_ = encoder.Encode(reply{Error: err.Error()})
			continue
		}
		_ = encoder.Encode(reply{Pages: images})
	}
}

// removeOldImages はしばらく使われていない画像 (と HTML の撮影の目録) を消す。
func removeOldImages(dir string) {
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	for _, file := range files {
		if info, err := os.Stat(file); err == nil && time.Since(info.ModTime()) > imageLifetime {
			_ = os.Remove(file)
		}
	}
}
