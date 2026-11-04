package Browser

// Every interaction here goes through DevTools' Input domain as a real trusted
// event. This is the decision that separates a browser agent that works from one
// that mostly works: element.click() and a synthetic KeyboardEvent are invisible
// to half the web — libraries that check event.isTrusted, components that react
// to pointerdown before they react to click, sliders and menus that track a
// pointer sequence. A dispatched mouse event at a coordinate cannot be told apart
// from a hand on a mouse.
//
// The corollary is that geometry must be live. A rect from a snapshot taken two
// seconds ago is wrong on a page that has re-rendered, so each action re-reads
// the box of the element it is about to hit.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// settleInterval and settleTimeout bound the wait for a page to stop moving.
	// A single-page app keeps mutating the DOM after load, so "loaded" is not the
	// right signal — quiet is.
	settleInterval = 120 * time.Millisecond
	settleTimeout  = 2500 * time.Millisecond
	// maxWaitSeconds caps one explicit wait, so a confused agent cannot stall a
	// run by asking to sleep for an hour.
	maxWaitSeconds = 30
	// loadWait bounds the wait for Chrome's load milestone after a navigation. A
	// page that answers at all answers in well under this.
	loadWait = 8 * time.Second
	// maxTextChars bounds what the text verb returns into a tool result.
	maxTextChars = 6000
)

// keySpec is one keystroke as DevTools wants it.
type keySpec struct {
	Key      string
	Code     string
	VK       int
	Text     string
	Location int
}

// keyTable maps the key names the agent may use to what Input.dispatchKeyEvent
// needs. Modifiers are written "ctrl+a" and read left to right.
var keyTable = map[string]keySpec{
	"enter":     {Key: "Enter", Code: "Enter", VK: 13, Text: "\r", Location: 3},
	"tab":       {Key: "Tab", Code: "Tab", VK: 9, Location: 3},
	"escape":    {Key: "Escape", Code: "Escape", VK: 27, Location: 3},
	"backspace": {Key: "Backspace", Code: "Backspace", VK: 8, Location: 3},
	"delete":    {Key: "Delete", Code: "Delete", VK: 46, Location: 3},
	"up":        {Key: "ArrowUp", Code: "ArrowUp", VK: 38, Location: 3},
	"down":      {Key: "ArrowDown", Code: "ArrowDown", VK: 40, Location: 3},
	"left":      {Key: "ArrowLeft", Code: "ArrowLeft", VK: 37, Location: 3},
	"right":     {Key: "ArrowRight", Code: "ArrowRight", VK: 39, Location: 3},
	"home":      {Key: "Home", Code: "Home", VK: 36, Location: 3},
	"end":       {Key: "End", Code: "End", VK: 35, Location: 3},
	"pageup":    {Key: "PageUp", Code: "PageUp", VK: 33, Location: 3},
	"pagedown":  {Key: "PageDown", Code: "PageDown", VK: 34, Location: 3},
	"space":     {Key: " ", Code: "Space", VK: 32, Text: " ", Location: 3},
}

// modifierMasks are the bit flags Input.dispatchKeyEvent expects.
var modifierMasks = map[string]int{
	"alt": 1, "ctrl": 2, "control": 2, "meta": 8, "cmd": 8, "command": 8, "shift": 16,
}

// rect is the live box of an element, plus the identity marks our snapshot put on
// it — so one round trip both aims the mouse and proves we are still aimed at the
// element the agent was shown.
type rect struct {
	X, Y, W, H    int
	Index         int
	Visible       bool
	InViewport    bool
	Disabled      bool
	Tag           string
	Type          string
	IsTextControl bool
}

// rectOf re-reads where the element is now.
func (p *Page) rectOf(ctx context.Context, objectID string) (rect, error) {
	const script = `function(){
  var el=this;
  var b=el.getBoundingClientRect?el.getBoundingClientRect():{left:0,top:0,width:0,height:0};
  var cs=window.getComputedStyle(el);
  var vh=window.innerHeight||0, vw=window.innerWidth||0;
  var cx=b.left+b.width/2, cy=b.top+b.height/2;
  var tag=(el.tagName||'').toLowerCase();
  var type=(el.getAttribute&&el.getAttribute('type')||'').toLowerCase();
  return {
    x: Math.round(cx), y: Math.round(cy),
    w: Math.round(b.width||0), h: Math.round(b.height||0),
    idx: Number(el.getAttribute('data-af-idx')||'-1'),
    visible: cs.display!=='none' && cs.visibility!=='hidden' && parseFloat(cs.opacity||'1')>0.05,
    inViewport: cx>=0 && cy>=0 && cx<=vw && cy<=vh && b.width>0 && b.height>0,
    disabled: !!el.disabled || el.getAttribute('aria-disabled')==='true',
    tag: tag, type: type,
    isTextControl: tag==='textarea' || tag==='select' || el.isContentEditable===true ||
      (tag==='input' && ['text','search','url','tel','email','password','number','date','time','datetime-local','month','week'].indexOf(type)>=0)
  };
}`
	value, err := p.session.CallOn(ctx, objectID, script)
	if err != nil {
		return rect{}, err
	}
	m, ok := value.(map[string]any)
	if !ok {
		return rect{}, fmt.Errorf("browser: the page returned an unusable box")
	}
	num := func(k string) int {
		if v, ok := m[k].(float64); ok {
			return int(v)
		}
		return 0
	}
	flag := func(k string) bool { v, _ := m[k].(bool); return v }
	return rect{
		X: num("x"), Y: num("y"), W: num("w"), H: num("h"),
		Index:   num("idx"),
		Visible: flag("visible"), InViewport: flag("inViewport"),
		Disabled: flag("disabled"), Tag: fmt.Sprint(m["tag"]),
		Type: fmt.Sprint(m["type"]), IsTextControl: flag("isTextControl"),
	}, nil
}

// beginAction is the only way an action reaches an element. It resolves the ref,
// re-reads its box, and checks the two things that must still be true: the page
// has not changed since we printed the tree, and the node behind the number is
// the node we numbered. Anything else comes back as a stale ref with a fresh
// snapshot, never as a click on a stranger.
func (p *Page) beginAction(ctx context.Context, ref int) (string, *Element, error) {
	el, err := p.lookup(ctx, ref)
	if err != nil {
		return "", nil, err
	}
	objectID, err := p.session.RefHandle(ctx, ref)
	if err != nil {
		return "", nil, fmt.Errorf("%w (ref [%d] no longer resolves to an element)", errStaleRef, ref)
	}
	box, err := p.rectOf(ctx, objectID)
	if err != nil {
		return "", nil, err
	}
	if box.Index != el.Index {
		return "", nil, fmt.Errorf("%w (the element at [%d] is a different node now)", errStaleRef, ref)
	}
	return objectID, el, nil
}

// scrollIntoView centres the element before a click that would otherwise land
// outside the viewport. It asks the page politely, with no smooth behaviour,
// because an animation in flight makes the next coordinate wrong.
func (p *Page) scrollIntoView(ctx context.Context, objectID string) error {
	const script = `function(){
  try { this.scrollIntoView({block:'center', inline:'center', behavior:'instant'}); }
  catch (e) { try { this.scrollIntoView(false); } catch (e2) {} }
  return true;
}`
	_, err := p.session.CallOn(ctx, objectID, script)
	return err
}

// mouse sends one mouse event at a viewport coordinate.
func (p *Page) mouse(ctx context.Context, kind string, x, y int, button string, clickCount int, wheelX, wheelY float64) error {
	params := map[string]any{
		"type": kind,
		"x":    x,
		"y":    y,
		"time": float64(time.Now().UnixMilli()),
	}
	switch kind {
	case "mousePressed", "mouseReleased", "mouseMoved":
		if button != "" {
			params["button"] = button
		} else {
			params["button"] = "none"
		}
		params["clickCount"] = clickCount
		params["buttons"] = buttonBits(button, kind)
	case "mouseWheel":
		params["deltaX"] = wheelX
		params["deltaY"] = wheelY
	}
	_, err := p.session.Call(ctx, mInputMouse, params)
	return err
}

// buttonBits is the `buttons` bitmask Chrome reports while a button is down.
func buttonBits(button string, kind string) int {
	if kind == "mouseReleased" {
		return 0
	}
	switch button {
	case "left":
		return 1
	case "right":
		return 2
	case "middle":
		return 4
	}
	return 0
}

// Click presses and releases on the element's own centre point.
func (p *Page) Click(ctx context.Context, ref int) error {
	objectID, _, err := p.beginAction(ctx, ref)
	if err != nil {
		return err
	}
	return p.clickHandle(ctx, ref, objectID)
}

// clickHandle does the mouse work for a ref already proved current.
func (p *Page) clickHandle(ctx context.Context, ref int, objectID string) error {
	box, err := p.rectOf(ctx, objectID)
	if err != nil {
		return err
	}
	if !box.Visible {
		return fmt.Errorf("browser: ref [%d] is hidden right now (%s) — it may live in a closed menu or tab", ref, box.Tag)
	}
	if !box.InViewport {
		if err := p.scrollIntoView(ctx, objectID); err != nil {
			return err
		}
		box, err = p.rectOf(ctx, objectID)
		if err != nil {
			return err
		}
	}
	// The move first: components that show a hover state or open on pointerover
	// need to see it before the press arrives, exactly as a person does.
	if err := p.mouse(ctx, "mouseMoved", box.X, box.Y, "", 0, 0, 0); err != nil {
		return err
	}
	if err := p.mouse(ctx, "mousePressed", box.X, box.Y, "left", 1, 0, 0); err != nil {
		return err
	}
	if err := p.mouse(ctx, "mouseReleased", box.X, box.Y, "left", 1, 0, 0); err != nil {
		return err
	}
	return nil
}

// Fill types into a control as if the keyboard were attached to this machine.
//
// The order matters: focus, optional clear, trusted text insertion, then verify.
// Input.insertText goes through the same path as an IME commit, so React, Vue and
// Angular all see it. If the value did not land — a controlled component that
// re-rendered from empty state, a masked input that swallowed the text — we fall
// back to the native value setter and dispatch the events a real edit produces.
func (p *Page) Fill(ctx context.Context, ref int, text string, clear bool) error {
	objectID, _, err := p.beginAction(ctx, ref)
	if err != nil {
		return err
	}
	box, err := p.rectOf(ctx, objectID)
	if err != nil {
		return err
	}
	if !box.Visible {
		return fmt.Errorf("browser: ref [%d] is hidden, so there is nothing to type into", ref)
	}
	if !box.InViewport {
		if err := p.scrollIntoView(ctx, objectID); err != nil {
			return err
		}
	}
	if _, err := p.session.CallOn(ctx, objectID, `function(){ try { this.focus(); } catch (e) {} return true; }`); err != nil {
		return err
	}
	if clear {
		if _, err := p.session.CallOn(ctx, objectID, clearScript); err != nil {
			return err
		}
	}
	if text == "" {
		return nil
	}
	if _, err := p.session.Call(ctx, mInputText, map[string]any{"text": text}); err != nil {
		return err
	}

	// Verify against the framework's own view of the value.
	got, err := p.session.CallOn(ctx, objectID, `function(){
      var el=this;
      if (el.isContentEditable) return el.innerText || el.textContent || '';
      return typeof el.value === 'string' ? el.value : '';
    }`)
	if err != nil {
		return nil // the text is in; a failed read-back is not worth an error
	}
	if strings.Contains(collapseSpace(fmt.Sprint(got)), collapseSpace(text)) {
		return nil
	}
	return p.setValueDirectly(ctx, objectID, text)
}

// clearScript empties a control the way a user's selection would, including the
// contenteditable case where there is no value to assign.
const clearScript = `function(){
  var el=this;
  if (el.isContentEditable) { el.textContent=''; el.dispatchEvent(new Event('input',{bubbles:true})); return true; }
  if (typeof el.select === 'function') { try { el.select(); } catch (e) {} }
  if ('value' in el) {
    el.value='';
    el.dispatchEvent(new Event('input',{bubbles:true}));
  }
  return true;
}`

// setValueDirectly is the escalation path: write through the *native* setter so a
// framework's own property override cannot intercept it, reset its change tracker,
// and fire the events a real keystroke would have fired.
const setValueScript = `function(text){
  var el=this;
  if (el.isContentEditable) {
    el.textContent = text;
    el.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText',data:text}));
    return el.innerText||'';
  }
  var proto = el.tagName==='TEXTAREA' ? window.HTMLTextAreaElement.prototype
            : el.tagName==='INPUT' ? window.HTMLInputElement.prototype : null;
  var desc = proto ? Object.getOwnPropertyDescriptor(proto,'value') : null;
  if (desc && desc.set) desc.set.call(el, text); else el.value = text;
  // React keeps the last value it rendered on the node; clearing it makes the
  // next input event look like a real change to its synthetic system.
  if (el._valueTracker && typeof el._valueTracker.setValue === 'function') el._valueTracker.setValue('');
  el.dispatchEvent(new Event('input',{bubbles:true}));
  el.dispatchEvent(new Event('change',{bubbles:true}));
  return typeof el.value==='string'?el.value:'';
}`

func (p *Page) setValueDirectly(ctx context.Context, objectID, text string) error {
	_, err := p.session.CallOn(ctx, objectID, setValueScript, text)
	return err
}

// Select chooses an option. A native <select> has no geometry worth clicking —
// the popup is OS chrome — so the value is set the way a change event would, and
// role=option/listbox items are clicked like any other control.
func (p *Page) Select(ctx context.Context, ref int, value string) error {
	objectID, _, err := p.beginAction(ctx, ref)
	if err != nil {
		return err
	}
	kind, err := p.session.CallOn(ctx, objectID, `function(){ return (this.tagName||'').toLowerCase(); }`)
	if err != nil {
		return err
	}
	if fmt.Sprint(kind) == "select" {
		_, err := p.session.CallOn(ctx, objectID, selectOptionScript, value)
		return err
	}
	return p.Click(ctx, ref)
}

// selectOptionScript matches by value first, then by visible text — an agent says
// "Paris" and gets the option labelled Paris even when its value is a postcode.
const selectOptionScript = `function(want){
  var el=this, lower=String(want).toLowerCase();
  var best=-1, fuzzy=-1;
  for (var i=0;i<el.options.length;i++){
    var o=el.options[i];
    if (String(o.value).toLowerCase()===lower) { best=i; break; }
    if (fuzzy<0 && (o.textContent||'').trim().toLowerCase()===lower) fuzzy=i;
    if (fuzzy<0 && (o.textContent||'').toLowerCase().indexOf(lower)>=0) fuzzy=i;
  }
  if (best<0) best=fuzzy;
  if (best<0) return 'no match';
  el.selectedIndex=best;
  el.dispatchEvent(new Event('input',{bubbles:true}));
  el.dispatchEvent(new Event('change',{bubbles:true}));
  return (el.options[best].textContent||'').trim();
}`

// Press sends one keystroke, with optional modifiers, to whatever has focus.
func (p *Page) Press(ctx context.Context, combo string) error {
	modifiers, keyName, err := splitCombo(combo)
	if err != nil {
		return err
	}
	spec, known := keyTable[keyName]
	if !known {
		// A single printable character is a character, not a named key.
		if len([]rune(keyName)) == 1 {
			spec = keySpec{Key: keyName, Code: "Key" + strings.ToUpper(keyName), Text: keyName}
		} else {
			return fmt.Errorf("browser: %q is not a key I know — use enter, tab, escape, backspace, delete, up, down, left, right, home, end, pageup, pagedown, space, or one character", combo)
		}
	}

	if spec.Text != "" && modifiers == 0 && !known {
		_, err := p.session.Call(ctx, mInputText, map[string]any{"text": spec.Text})
		return err
	}
	params := map[string]any{
		"key":                   spec.Key,
		"code":                  spec.Code,
		"windowsVirtualKeyCode": spec.VK,
		"nativeVirtualKeyCode":  spec.VK,
		"modifiers":             modifiers,
	}
	if spec.Location > 0 {
		params["location"] = spec.Location
	}
	down := maps.Clone(params)
	down["type"] = "keyDown"
	if spec.Text != "" {
		down["text"] = spec.Text
	}
	if _, err := p.session.Call(ctx, mInputKey, down); err != nil {
		return err
	}
	up := maps.Clone(params)
	up["type"] = "keyUp"
	_, err = p.session.Call(ctx, mInputKey, up)
	return err
}

func splitCombo(combo string) (modifiers int, key string, err error) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(combo)), "+")
	if len(parts) == 0 {
		return 0, "", fmt.Errorf("browser: empty key")
	}
	key = parts[len(parts)-1]
	for _, m := range parts[:len(parts)-1] {
		mask, ok := modifierMasks[strings.TrimSpace(m)]
		if !ok {
			return 0, "", fmt.Errorf("browser: %q is not a modifier (use ctrl, alt, shift or meta)", m)
		}
		modifiers |= mask
	}
	if key == "" {
		return 0, "", fmt.Errorf("browser: %q has no key after the modifiers", combo)
	}
	return modifiers, key, nil
}

// Scroll moves the page the way a wheel would, so lazy loaders and scroll
// listeners fire.
func (p *Page) Scroll(ctx context.Context, direction string, ref int) error {
	x, y := 640, 450
	if ref > 0 {
		objectID, _, err := p.beginAction(ctx, ref)
		if err != nil {
			return err
		}
		box, err := p.rectOf(ctx, objectID)
		if err != nil {
			return err
		}
		if box.InViewport {
			x, y = box.X, box.Y
		}
	}
	// A wheel over a nested scroll container scrolls that container, which is
	// what pointing the agent at a list means.
	const page = 600.0
	var dx, dy float64
	switch strings.ToLower(direction) {
	case "down", "":
		dy = page
	case "up":
		dy = -page
	case "right":
		dx = page
	case "left":
		dx = -page
	default:
		return fmt.Errorf("browser: scroll direction must be up, down, left or right, not %q", direction)
	}
	if err := p.mouse(ctx, "mouseMoved", x, y, "", 0, 0, 0); err != nil {
		return err
	}
	_, err := p.session.Call(ctx, mInputMouse, map[string]any{
		"type": "mouseWheel", "x": x, "y": y, "deltaX": dx, "deltaY": dy,
	})
	return err
}

// ScrollTo jumps to an absolute place on the page, which wheeling cannot promise.
func (p *Page) ScrollTo(ctx context.Context, target string) error {
	var expr string
	switch strings.ToLower(target) {
	case "top":
		expr = `window.scrollTo(0,0)`
	case "bottom":
		expr = `window.scrollTo(0, document.documentElement.scrollHeight)`
	default:
		return fmt.Errorf("browser: scroll to is either top or bottom, not %q", target)
	}
	_, err := p.session.Eval(ctx, "(function(){"+expr+";return true;})()")
	return err
}

// Open navigates and waits for the page to go quiet.
func (p *Page) Open(ctx context.Context, url string) error {
	if strings.TrimSpace(url) == "" {
		return errors.New("browser: no url to open")
	}
	if !strings.Contains(url, "://") && looksLikeHost(url) {
		url = "https://" + url
	}
	// Listening starts before the navigation, so Chrome's own load milestone is
	// caught rather than missed and then waited out.
	waitLoad := p.session.ExpectLifecycle(ctx, "load")
	if _, err := p.session.Call(ctx, mPageNavigate, map[string]any{"url": url}); err != nil {
		return err
	}
	p.reset()
	// A loaded page fires the milestone in well under a second; the ceiling exists
	// only for a Chrome that never answers, and it must not become the reason a
	// simple open takes half a minute.
	waitLoad(loadWait)
	_, err := p.Wait(ctx, 0)
	return err
}

// Back walks one step through this tab's history.
func (p *Page) Back(ctx context.Context) error {
	var out struct {
		Entries []struct {
			ID  int    `json:"id"`
			URL string `json:"url"`
		} `json:"entries"`
		Current int `json:"currentIndex"`
	}
	if err := p.session.CallInto(ctx, mPageHistory, nil, &out); err != nil {
		return err
	}
	if out.Current < 1 || out.Current >= len(out.Entries) {
		return errors.New("browser: there is no page before this one in the tab's history")
	}
	// History navigation has no direct CDP verb on this Chrome surface, so the
	// honest equivalent is to go to the recorded URL.
	if _, err := p.session.Call(ctx, mPageNavigate, map[string]any{"url": out.Entries[out.Current-1].URL}); err != nil {
		return err
	}
	p.reset()
	return nil
}

// Wait settles the page. With ms it waits at least that long first; either way it
// returns when two observations in a row look identical, which is the signal that
// a re-render has finished.
func (p *Page) Wait(ctx context.Context, ms int) (bool, error) {
	if ms > 0 {
		if ms > maxWaitSeconds*1000 {
			ms = maxWaitSeconds * 1000
		}
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	previous := ""
	deadline := time.Now().Add(settleTimeout)
	for {
		sig, err := p.session.EvalString(ctx, signatureScript)
		if err != nil {
			return false, err
		}
		if previous != "" && sig == previous {
			return true, nil
		}
		previous = sig
		if time.Now().After(deadline) {
			return false, nil // settled never means the page kept moving
		}
		select {
		case <-time.After(settleInterval):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

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

// IsDirectAddress answers whether the caller handed us something to navigate to
// as-is, or words that have to be searched for first. Exported because Chat owns
// the search backend and needs to know when to use it.
func IsDirectAddress(s string) bool { return looksLikeHost(s) }

// looksLikeHost tells "example.com" from "cheap flights to lisbon" — the first is
// a site to open, the second is a search to make.
func looksLikeHost(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t\n") {
		return false
	}
	host := s
	if parsed, err := url.Parse(s); err == nil && parsed.Host != "" {
		host = parsed.Host
	} else if i := strings.IndexAny(host, "/?#"); i >= 0 {
		// "example.com/path" with no scheme: the authority is everything before
		// the first separator.
		host = host[:i]
	}
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	if bare, _, err := net.SplitHostPort(host); err == nil {
		host = bare
	}
	host = strings.ToLower(strings.Trim(host, "."))
	if host == "" {
		return false
	}
	// A local server is a legitimate target and has no dot in it.
	if host == "localhost" || strings.HasPrefix(host, "127.") || strings.HasPrefix(host, "[") {
		return true
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return false
	}
	last := parts[len(parts)-1]
	if len(last) < 2 || len(last) > 24 {
		return false
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
