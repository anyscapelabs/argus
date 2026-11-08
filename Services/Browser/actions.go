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
	"fmt"
	"strings"
	"time"
)

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
