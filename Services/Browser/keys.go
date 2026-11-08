package Browser

import (
	"context"
	"fmt"
	"maps"
	"strings"
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
