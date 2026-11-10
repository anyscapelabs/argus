package Slides

import "strings"

// DefaultTheme is what an empty theme becomes: a dark, high-contrast look that
// works in a meeting room. The agent is free to override any field; this is only
// the fallback so a deck without an explicit theme still looks intentional.
var DefaultTheme = Theme{
	Name:        "dark",
	Background:  "#1f2430",
	Accent:      "#ff9d6c",
	Text:        "#f5f0e6",
	HeadingFont: "Arial",
	BodyFont:    "Arial",
}

// normalizeTheme fills every gap in an authored theme with the default, and
// drops any value that cannot be rendered safely — a non-hex color or an empty
// font name. The result is always a complete, valid theme.
func normalizeTheme(t Theme) Theme {
	out := DefaultTheme
	if n := strings.TrimSpace(t.Name); n != "" {
		out.Name = n
	}
	if c := validColor(t.Background); c != "" {
		out.Background = c
	}
	if c := validColor(t.Accent); c != "" {
		out.Accent = c
	}
	if c := validColor(t.Text); c != "" {
		out.Text = c
	}
	if f := validFont(t.HeadingFont); f != "" {
		out.HeadingFont = f
	}
	if f := validFont(t.BodyFont); f != "" {
		out.BodyFont = f
	}
	return out
}

// validColor accepts a #rgb or #rrggbb hex color and returns it normalized, or
// "" when the value is not a usable color. An invalid authored color silently
// falls back rather than failing the whole deck.
func validColor(c string) string {
	c = strings.TrimSpace(strings.ToLower(c))
	if len(c) != 4 && len(c) != 7 {
		return ""
	}
	if c[0] != '#' {
		return ""
	}
	for _, r := range c[1:] {
		if !isHex(r) {
			return ""
		}
	}
	return c
}

func isHex(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
}

// validFont accepts a font name that is not empty and has no XML-hostile
// characters; everything else falls back to the default.
func validFont(f string) string {
	f = strings.TrimSpace(f)
	if f == "" {
		return ""
	}
	if strings.ContainsAny(f, "<>&\"") {
		return ""
	}
	if len(f) > 40 {
		return ""
	}
	return f
}
