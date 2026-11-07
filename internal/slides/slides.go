// Package slides は Marp のスライドを PNG にする。
//
// marp-cli を毎回起動すると Chrome の起動だけで 2 秒ほどかかるので、
// marp-cli に入っている marp-core と puppeteer-core を使う小さな描画サーバー (server.js) を
// node で常駐させ、Chrome を起動したまま使い回す。中身の変わらないスライドは描き直さない。
package slides

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "embed"
)

// renderTimeout は1回の描画を待つ上限 (最初の1回は Chrome の起動を含む)。
const renderTimeout = 60 * time.Second

// ErrNoMarp は marp-cli が入っていないとき。
var ErrNoMarp = errors.New("marp が見つかりません。\n入れるとスライドを画像で確認できます:\n\n  npm i -g @marp-team/marp-cli")

//go:embed server.js
var serverScript []byte

// Server は常駐する描画サーバー。最初の Render で起動し、Close で止める。
type Server struct {
	node, cliDir string
	scriptDir    string

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	replies chan reply
}

type request struct {
	Markdown string `json:"markdown"`
	Dir      string `json:"dir"`
	Out      string `json:"out"`
}

type reply struct {
	Pages []string `json:"pages"`
	Error string   `json:"error"`
}

// Find は marp-cli と node を探し、描画サーバーを用意する (まだ起動しない)。
func Find() (*Server, error) {
	marp, err := exec.LookPath("marp")
	if err != nil {
		return nil, ErrNoMarp
	}
	entry, err := filepath.EvalSymlinks(marp)
	if err != nil {
		return nil, err
	}
	cliDir := filepath.Dir(entry)
	if _, err := os.Stat(filepath.Join(cliDir, "node_modules", "@marp-team", "marp-core")); err != nil {
		return nil, fmt.Errorf("marp-cli (%s) の中に marp-core が見つかりません。npm i -g @marp-team/marp-cli で入れ直してください", cliDir)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return nil, errors.New("node が見つかりません")
	}
	scriptDir, err := os.MkdirTemp("", "memo-marp-server-")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "server.js"), serverScript, 0o644); err != nil {
		return nil, err
	}
	return &Server{node: node, cliDir: cliDir, scriptDir: scriptDir}, nil
}

// Render は Markdown のソースを1枚ずつの PNG にして out フォルダに書き、そのパスをスライド順に返す。
// PNG の名前はスライドの中身で決まるので、変わっていないスライドは同じパスのまま。
// 画像などの相対パスは dir から探す。同時に呼ばないこと。
func (s *Server) Render(source, dir, out string) ([]string, error) {
	replies, err := s.send(request{Markdown: source, Dir: dir, Out: out})
	if err != nil {
		return nil, err
	}
	select {
	case r, ok := <-replies:
		switch {
		case !ok:
			s.stop()
			return nil, errors.New("スライドの描画サーバーが止まりました")
		case r.Error != "":
			return nil, errors.New(r.Error)
		}
		return r.Pages, nil
	case <-time.After(renderTimeout):
		s.stop()
		return nil, errors.New("スライドの描画が時間内に終わりませんでした")
	}
}

// send は (止まっていれば起動してから) 描画を頼む。
func (s *Server) send(req request) (chan reply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd == nil {
		if err := s.start(); err != nil {
			return nil, err
		}
	}
	line, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := s.stdin.Write(append(line, '\n')); err != nil {
		s.kill()
		return nil, fmt.Errorf("スライドの描画サーバーに送れません: %w", err)
	}
	return s.replies, nil
}

func (s *Server) start() error {
	cmd := exec.Command(s.node, filepath.Join(s.scriptDir, "server.js"), s.cliDir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("スライドの描画サーバーを起動できません: %w", err)
	}
	replies := make(chan reply, 1)
	go func() {
		defer close(replies)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1<<20), 1<<24)
		for scanner.Scan() {
			var r reply
			if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
				r = reply{Error: "描画サーバーの返事を読めません: " + strings.TrimSpace(scanner.Text())}
			}
			replies <- r
		}
	}()
	s.cmd, s.stdin, s.replies = cmd, stdin, replies
	return nil
}

func (s *Server) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kill()
}

func (s *Server) kill() {
	if s.cmd == nil {
		return
	}
	_ = s.stdin.Close()
	_ = s.cmd.Process.Kill()
	_ = s.cmd.Wait()
	s.cmd = nil
}

// Close は描画サーバーを止める。入力を閉じると Chrome を閉じて終わる。少し待っても終わらなければ止める。
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil {
		_ = s.stdin.Close()
		done := make(chan struct{})
		go func() { _ = s.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = s.cmd.Process.Kill()
			<-done
		}
		s.cmd = nil
	}
	_ = os.RemoveAll(s.scriptDir)
}
