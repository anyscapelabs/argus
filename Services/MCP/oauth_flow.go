package MCP

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

const (
	// preferredCallbackPort is the loopback port Argus tries first. Google
	// desktop clients accept any port on 127.0.0.1, so this is only a
	// preference: if the port is busy an ephemeral one is used instead.
	preferredCallbackPort = 47831
	// PlaceholderRedirectURL is only used for non-interactive connects, which
	// never open a browser, but the SDK requires a redirect URL to build a
	// handler.
	PlaceholderRedirectURL = "http://127.0.0.1:47831"
)

var ErrNeedsAuth = errors.New("mcp: sign-in required")

// callback is the loopback listener that receives the OAuth redirect.
//
// The URI shape is set by the provider, not by us. A Google "Desktop app"
// client cannot list redirect URIs in the Console and accepts an unregistered
// loopback URI only in the exact form http://127.0.0.1:<port> with no path, so
// Google connects with an empty path. GitHub does the opposite: the OAuth App
// has one registered callback URL whose path must match, so GitHub connects
// with "/callback". GitHub ignores the port for loopback redirects, which is
// what makes the ephemeral fallback below safe.
type callback struct {
	ln     net.Listener
	url    string
	result chan *auth.AuthorizationResult
	failed chan error
	srv    *http.Server
}

func newCallback(path string) (*callback, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredCallbackPort))
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("could not open a loopback port for the sign-in redirect: %w", err)
		}
	}
	c := &callback{
		ln:     ln,
		url:    "http://" + ln.Addr().String() + path,
		result: make(chan *auth.AuthorizationResult, 1),
		failed: make(chan error, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", c.handle)
	c.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go c.srv.Serve(ln)
	return c, nil
}

func (c *callback) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.srv.Shutdown(ctx)
}

func (c *callback) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		fmt.Fprintf(w, donePage, "Sign-in was cancelled")
		select {
		case c.failed <- fmt.Errorf("authorization denied: %s", e):
		default:
		}
		return
	}
	code := q.Get("code")
	if code == "" {
		// Not a sign-in response (a favicon request, a stray probe). Keep
		// listening rather than reporting a bogus code.
		fmt.Fprintf(w, donePage, "Nothing to do here")
		return
	}
	fmt.Fprintf(w, donePage, "Connected")
	select {
	case c.result <- &auth.AuthorizationResult{Code: code, State: q.Get("state"), Iss: q.Get("iss")}:
	default:
	}
}

func openBrowser(u string) error {
	if !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("refusing to open non-https url")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("no browser to open the sign-in page: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		if err == nil {
			return nil // the opener handed the url off and left cleanly
		}
		// The opener died at once: nothing is showing the sign-in page, so
		// report it instead of waiting for a redirect that can never arrive.
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("the browser refused to open the sign-in page: %s", msg)
	case <-time.After(3 * time.Second):
		return nil
	}
}

const donePage = "<!doctype html><meta charset=utf-8><title>Argus</title>" +
	"<body style='font-family:sans-serif;text-align:center;margin-top:20vh'>" +
	"<h2>%s</h2><p>You can close this tab and return to Argus.</p>"

// installedClientURL adds the parameters a Google "Desktop app" client needs
// and the SDK does not send. Without access_type=offline the token endpoint
// returns no refresh token, so the session cannot outlive the one-hour access
// token; prompt=consent makes Google re-issue that refresh token on every
// sign-in instead of only on the first one.
func installedClientURL(u string) (string, error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	if parsed.Host != "accounts.google.com" {
		return u, nil
	}
	q := parsed.Query()
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

// fetchCode opens the real authorization URL and waits for the redirect on cb.
// interactive=false is used at app start: never pop a browser unprompted.
func fetchCode(interactive bool, cb *callback) auth.AuthorizationCodeFetcher {
	return func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		if !interactive {
			return nil, ErrNeedsAuth
		}
		target, err := installedClientURL(args.URL)
		if err != nil {
			return nil, err
		}
		if err := openBrowser(target); err != nil {
			return nil, err
		}
		select {
		case r := <-cb.result:
			return r, nil
		case err := <-cb.failed:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
