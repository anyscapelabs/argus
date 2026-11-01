package MCP

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

const (
	callbackAddr = "127.0.0.1:47831"
	// CallbackURL is the fixed redirect URI.
	CallbackURL = "http://127.0.0.1:47831/oauth/callback"
)

var ErrNeedsAuth = errors.New("mcp: sign-in required")

func openBrowser(u string) error {
	if !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("refusing to open non-https url")
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	default:
		return exec.Command("xdg-open", u).Start()
	}
}

const donePage = "<!doctype html><meta charset=utf-8><title>Argus</title>" +
	"<body style='font-family:sans-serif;text-align:center;margin-top:20vh'>" +
	"<h2>%s</h2><p>You can close this tab and return to Argus.</p>"

// fetchCode opens the real authorization URL and waits for the redirect.
// interactive=false is used at app start: never pop a browser unprompted.
func fetchCode(interactive bool) auth.AuthorizationCodeFetcher {
	return func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		if !interactive {
			return nil, ErrNeedsAuth
		}
		ln, err := net.Listen("tcp", callbackAddr)
		if err != nil {
			return nil, fmt.Errorf("sign-in port 47831 is in use: %w", err)
		}
		res := make(chan *auth.AuthorizationResult, 1)
		fail := make(chan error, 1)
		mux := http.NewServeMux()
		mux.HandleFunc("/oauth/callback", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if e := q.Get("error"); e != "" {
				fmt.Fprintf(w, donePage, "Sign-in was cancelled")
				select {
				case fail <- fmt.Errorf("authorization denied: %s", e):
				default:
				}
				return
			}
			fmt.Fprintf(w, donePage, "Connected")
			select {
			case res <- &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}:
			default:
			}
		})
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go srv.Serve(ln)
		defer func() {
			c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = srv.Shutdown(c)
		}()
		if err := openBrowser(args.URL); err != nil {
			return nil, err
		}
		select {
		case r := <-res:
			return r, nil
		case err := <-fail:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
