package Browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// maxTextChars bounds what the text verb returns into a tool result.
	maxTextChars = 6000
)

// Text returns the readable prose of the page — the verb for "what does this
// page say", where a list of controls would be useless.
func (p *Page) Text(ctx context.Context) (string, error) {
	const script = `(function(){
  function deepText(el, out, depth){
    if (!el || depth>30) return;
    if (el.nodeType===3){ out.push(el.nodeValue); return; }
    if (el.nodeType!==1) return;
    var tag=(el.tagName||'').toLowerCase();
    if (tag==='script'||tag==='style'||tag==='noscript'||tag==='svg') return;
    if (el.getAttribute && el.getAttribute('aria-hidden')==='true') return;
    if (el.shadowRoot) { var kids=el.shadowRoot.childNodes; for (var i=0;i<kids.length;i++) deepText(kids[i],out,depth+1); return; }
    var kids2=el.childNodes;
    for (var j=0;j<kids2.length;j++) deepText(kids2[j],out,depth+1);
  }
  var scope = document.querySelector('article') || document.querySelector('main') || document.body;
  if (!scope) return '';
  var out=[]; deepText(scope,out,0);
  return out.join(' ').replace(/\s+/g,' ').trim().slice(0, %d);
})()`
	return p.session.EvalString(ctx, fmt.Sprintf(script, maxTextChars))
}

// Screenshot saves the current viewport as a PNG and returns where it went. The
// image itself is not handed to the model: an agent that can read the accessibility
// tree rarely needs pixels, and a base64 blob in a tool result is the most
// expensive thing we could do to a context window.
func (p *Page) Screenshot(ctx context.Context, dir string) (string, error) {
	raw, err := p.screenshotPNG(ctx)
	if err != nil {
		return "", err
	}
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "argus", "screenshots")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("browser: could not create the screenshot dir: %w", err)
	}
	name := filepath.Join(dir, fmt.Sprintf("browser-%d.png", time.Now().UnixMilli()))
	if err := os.WriteFile(name, raw, 0o600); err != nil {
		return "", fmt.Errorf("browser: could not write the screenshot: %w", err)
	}
	return name, nil
}

// screenshotPNG captures the viewport as raw PNG bytes. The takeover card is
// the one consumer that wants the pixels themselves: a human judging whether
// to take over a page reads the picture, and the model never sees these bytes.
func (p *Page) screenshotPNG(ctx context.Context) ([]byte, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := p.session.CallInto(ctx, mPageScreenshot, map[string]any{
		"format": "png", "captureBeyondViewport": false,
	}, &out); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil {
		return nil, fmt.Errorf("browser: Chrome sent a screenshot we could not decode: %w", err)
	}
	return raw, nil
}

// EvalJS is the escape hatch, with the result kept small on purpose: a script
// that returns the whole DOM is not more useful than a snapshot, it is only more
// expensive.
func (p *Page) EvalJS(ctx context.Context, expr string) (string, error) {
	value, err := p.session.Eval(ctx, expr)
	if err != nil {
		return "", err
	}
	if value == nil {
		return "", nil
	}
	text := fmt.Sprint(value)
	if len(text) > maxTextChars {
		text = text[:maxTextChars] + "\n… [result cut]"
	}
	return text, nil
}
