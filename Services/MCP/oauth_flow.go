package MCP

import (
	"encoding/json"
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

func jsonString(v string) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func renderDonePage(title, message string) string {
	return fmt.Sprintf(donePage, jsonString(title), jsonString(message))
}

func (c *callback) handle(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		_, _ = w.Write([]byte(renderDonePage("Sign-in was cancelled", "You can close this tab and return to Argus.")))
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
		_, _ = w.Write([]byte(renderDonePage("Nothing to do here", "You can close this tab and return to Argus.")))
		return
	}
	_, _ = w.Write([]byte(renderDonePage("Connected", "You can close this tab and return to Argus.")))
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

const donePage = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Argus</title>
<style>
  @import url('https://fonts.googleapis.com/css2?family=Geist:wght@400;500&display=swap');
  :root { color-scheme: dark; }
  html, body { height: 100%%; margin: 0; }
  body {
    display: flex;
    align-items: center;
    justify-content: center;
    background: #000;
    color: #e6e6e8;
    font-family: "Geist", ui-sans-serif, system-ui, -apple-system, sans-serif;
    text-align: center;
  }
  #root { width: 100%%; height: 100%%; display: flex; align-items: center; justify-content: center; }
  .wrap { display: flex; flex-direction: column; align-items: center; gap: 16px; padding: 24px; }
  .logo svg { width: 72px; height: 72px; display: block; }
  h2 { margin: 0; font-size: 20px; font-weight: 500; color: #fff; }
  p { margin: 0; font-size: 14px; color: #b0b1b6; }
</style>
<body>
  <div id="root"></div>
  <script crossorigin src="https://unpkg.com/react@18/umd/react.production.min.js"></script>
  <script crossorigin src="https://unpkg.com/react-dom@18/umd/react-dom.production.min.js"></script>
  <script>
    const e = React.createElement;
    const logo = e('svg', { width: 72, height: 72, viewBox: '200 200 624 624', fill: 'none', xmlns: 'http://www.w3.org/2000/svg', 'aria-hidden': true },
      e('path', { d: 'M268.195 317.692C274.667 293.536 293.536 274.667 317.692 268.195L558.373 203.705C582.529 197.232 608.304 204.138 625.988 221.822L802.178 398.012C819.862 415.696 826.768 441.471 820.296 465.627L755.805 706.308C749.333 730.464 730.464 749.333 706.308 755.805L465.627 820.295C441.471 826.768 415.696 819.862 398.012 802.178L221.822 625.988C204.138 608.304 197.232 582.529 203.705 558.373L268.195 317.692Z', fill: 'white' }),
      e('rect', { x: '298.6', y: '513.768', width: '105', height: '300', rx: '52.5', transform: 'rotate(-45 298.6 513.768)', fill: '#121214' }),
      e('rect', { x: '440.022', y: '372.346', width: '105', height: '300', rx: '52.5', transform: 'rotate(-45 440.022 372.346)', fill: '#121214' })
    );
    function App() {
      return e('div', { className: 'wrap' },
        e('div', { className: 'logo' }, logo),
        e('h2', null, %s),
        e('p', null, %s)
      );
    }
    ReactDOM.createRoot(document.getElementById('root')).render(e(App));
  </script>
</body>
</html>`

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
