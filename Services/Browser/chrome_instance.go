package Browser

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// launchTimeout bounds the wait for Chrome to publish its debug port. A cold
	// start on a slow disk is a second or two; past this the install is broken,
	// and a readable failure beats another wait.
	launchTimeout = 15 * time.Second
	portPoll      = 40 * time.Millisecond

	// initialURL is the blank page Chrome opens with. Headed Chrome needs a
	// window to exist before Browser.setWindowBounds can place it, so we never
	// start with --no-startup-window.
	initialURL = "about:blank"
)

// instance is one running Chrome, the port it listens on, and the protocol
// client connected to it.
type instance struct {
	cmd      *exec.Cmd
	pid      int
	port     int
	baseURL  string
	profile  string
	headless bool
	log      *tail
	client   *Client

	// exited is set once the process is confirmed gone. ProcessState alone
	// cannot do this: it is only ever populated by Wait, and unless we are the
	// one calling kill() nobody waits — a Chrome the user closed by hand is a
	// zombie child that still answers kill(pid, 0). The reaper below is what
	// makes "is the browser alive" answerable at all.
	exited atomic.Bool
}

// start launches Chrome, waits for the debug port, and returns a live client.
func start(ctx context.Context, headless bool) (*instance, error) {
	bin := findChrome()
	if bin == "" {
		return nil, errors.New(chromeTool.Required)
	}
	profile, err := profileDir()
	if err != nil {
		return nil, err
	}

	// A port file left by a crashed run would be read as this run's answer, so
	// the handshake starts from a clean slate.
	_ = os.Remove(filepath.Join(profile, "DevToolsActivePort"))

	args := []string{
		// Port 0 lets Chrome choose and writes the number into the profile.
		// Hardcoding one would collide with anything already using it.
		"--remote-debugging-port=0",
		"--user-data-dir=" + profile,
		// Chrome 111+ rejects a DevTools WebSocket upgrade whose Origin header is
		// not the DevTools frontend. We are a local process talking to a browser we
		// started ourselves on a loopback port, so opting out is safe — and without
		// it every connection is a 403.
		"--remote-allow-origins=*",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-sync",
		"--disable-session-crashed-bubble",
		"--hide-crash-restore-bubble",
		"--disable-popup-blocking",
		"--disable-features=Translate",
		"--window-size=1280,900",
		"--window-position=80,80",
		"--lang=en-US",
	}
	if headless {
		args = append(args, "--headless=new", "--disable-gpu")
	}
	args = append(args, initialURL)

	cmd := exec.Command(bin, args...)
	log := newTail(24)
	cmd.Stdout = log
	cmd.Stderr = log
	if runtime.GOOS != "windows" {
		// Its own process group, so quitting the app can take the whole browser
		// tree with it instead of leaving zygote children behind.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("browser: could not start %s: %w", bin, err)
	}

	inst := &instance{
		cmd:      cmd,
		pid:      cmd.Process.Pid,
		profile:  profile,
		headless: headless,
		log:      log,
	}

	// The reaper: Wait is the only way the exit of a child process becomes
	// observable, and without a waiter a Chrome the user closed sits as a
	// zombie that every other probe — kill(pid, 0) included — still calls
	// alive. One goroutine per launch. Double-Wait from kill() is harmless.
	go func() {
		_ = cmd.Wait()
		inst.exited.Store(true)
	}()

	port, err := waitForPort(ctx, profile, cmd, log)
	if err != nil {
		inst.kill()
		return nil, err
	}
	inst.port = port
	inst.baseURL = "http://127.0.0.1:" + strconv.Itoa(port)

	client, err := waitForClient(ctx, inst)
	if err != nil {
		inst.kill()
		return nil, err
	}
	inst.client = client
	return inst, nil
}

// waitForPort reads DevToolsActivePort until Chrome writes it.
func waitForPort(ctx context.Context, profile string, cmd *exec.Cmd, log *tail) (int, error) {
	path := filepath.Join(profile, "DevToolsActivePort")
	deadline := time.Now().Add(launchTimeout)
	ticker := time.NewTicker(portPoll)
	defer ticker.Stop()
	for {
		if raw, err := os.ReadFile(path); err == nil {
			lines := strings.Split(strings.ReplaceAll(string(raw), "\r", ""), "\n")
			first := strings.TrimSpace(lines[0])
			if first == "" {
				return 0, errors.New("browser: Chrome wrote an empty DevToolsActivePort file")
			}
			port, err := strconv.Atoi(first)
			if err != nil || port <= 0 || port > 65535 {
				return 0, fmt.Errorf("browser: Chrome reported an unusable debug port %q", first)
			}
			return port, nil
		}
		// A browser that already exited cannot publish a port; say why instead of
		// waiting out the full timeout.
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return 0, fmt.Errorf("browser: Chrome exited before opening a debug port — %s", log.line())
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("browser: Chrome did not open a debug port within %v — %s",
				launchTimeout, log.line())
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
		}
	}
}

// waitForClient retries the DevTools HTTP handshake, which becomes reachable a
// moment after the port file appears.
func waitForClient(ctx context.Context, inst *instance) (*Client, error) {
	deadline := time.Now().Add(launchTimeout)
	var lastErr error
	for {
		dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		client, err := Dial(dialCtx, inst.baseURL)
		cancel()
		if err == nil {
			if client.VersionMajor() != 0 && client.VersionMajor() < 111 {
				client.Close()
				return nil, fmt.Errorf("browser: %s is too old to drive; Chrome 111 or newer is required",
					client.Version())
			}
			return client, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("browser: the devtools endpoint on port %d never answered: %w",
				inst.port, lastErr)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(portPoll):
		}
	}
}

// kill stops Chrome and its process group. SIGTERM first: Chrome flushes the
// profile on the way out, and a forced kill makes the next start show the user a
// crash-recovery bubble.
func (i *instance) kill() {
	if i.client != nil {
		_ = i.client.Close()
		i.client = nil
	}
	if i.cmd == nil || i.cmd.Process == nil {
		return
	}
	if runtime.GOOS != "windows" {
		if err := syscall.Kill(-i.pid, syscall.SIGTERM); err == nil {
			done := make(chan struct{})
			go func() {
				_ = i.cmd.Wait()
				close(done)
			}()
			select {
			case <-done:
				return
			case <-time.After(3 * time.Second):
				_ = syscall.Kill(-i.pid, syscall.SIGKILL)
				_ = i.cmd.Wait()
				return
			}
		}
	}
	_ = i.cmd.Process.Kill()
	_ = i.cmd.Wait()
}

// running reports whether the browser is still alive, so a session the user
// closed by hand gets rebuilt instead of erroring for the rest of the run.
// The reaper's flag is the reliable answer; the ProcessState and signal-0
// checks below are belt-and-braces for a launch that never got to spawn the
// reaper, or a process that vanished before Wait could report it.
func (i *instance) running() bool {
	if i.exited.Load() {
		return false
	}
	if i.cmd == nil || i.cmd.Process == nil {
		return false
	}
	if i.cmd.ProcessState != nil && i.cmd.ProcessState.Exited() {
		return false
	}
	if runtime.GOOS == "windows" {
		// The reaper above observes exit on every platform, so reaching this
		// line on Windows means it has not fired yet; a live DevTools client
		// is the honest answer available.
		return i.client.alive()
	}
	// Signal 0 asks only whether the process exists.
	return syscall.Kill(i.pid, 0) == nil
}

// describe is the one line the app can show about the current browser.
func (i *instance) describe() string {
	mode := "headed"
	if i.headless {
		mode = "headless"
	}
	probe := "Chrome"
	if i.client != nil && i.client.Version() != "" {
		probe = i.client.Version()
	}
	return fmt.Sprintf("%s, %s, profile %s", probe, mode, filepath.Base(i.profile))
}

// tail keeps only the last few lines of a subprocess, which is how much of
// Chrome's stderr is useful in an error message.
type tail struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func newTail(max int) *tail { return &tail{max: max} }

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	scanner := bufio.NewScanner(strings.NewReader(string(p)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		t.lines = append(t.lines, line)
		if len(t.lines) > t.max {
			t.lines = t.lines[len(t.lines)-t.max:]
		}
	}
	return len(p), nil
}

// line is the single most informative thing Chrome printed.
func (t *tail) line() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.lines) == 0 {
		return "no output from Chrome"
	}
	for i := len(t.lines) - 1; i >= 0; i-- {
		if lower := strings.ToLower(t.lines[i]); strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
			return shorten(t.lines[i], 200)
		}
	}
	return shorten(t.lines[len(t.lines)-1], 200)
}

// recent returns the kept lines as one block, for logs.
func (t *tail) recent() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.lines, "\n")
}
