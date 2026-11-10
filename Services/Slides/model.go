// The slide-deck engine: turn the agent's structured deck description into a
// PowerPoint file. The agent authors slide JSON; this package turns that into a
// flat ODP document and asks headless LibreOffice to convert it to .pptx — the
// same external-tool pattern the document exporters already use, so a deck needs
// no new dependency and follows the one-authored-format philosophy.
package Slides

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Deck is one authored presentation.
type Deck struct {
	Title  string  `json:"title"`
	Theme  Theme   `json:"theme"`
	Slides []Slide `json:"slides"`
}

// Slide is one page of the deck.
type Slide struct {
	// Layout selects the page template: "title", "section", "content" or
	// "two_col".
	Layout   string   `json:"layout"`
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle,omitempty"`
	Bullets  []string `json:"bullets,omitempty"`
	Left     []string `json:"left,omitempty"`
	Right    []string `json:"right,omitempty"`
	Notes    string   `json:"notes,omitempty"`
}

// Theme is the visual identity the agent chooses. Colors are CSS hex strings;
// fonts are names LibreOffice is expected to have. Every field is optional — an
// empty theme falls back to the built-in default.
type Theme struct {
	Name        string `json:"name"`
	Background  string `json:"background"`
	Accent      string `json:"accent"`
	Text        string `json:"text"`
	HeadingFont string `json:"heading_font"`
	BodyFont    string `json:"body_font"`
}

// Layout ids the generator knows how to draw.
const (
	LayoutTitle   = "title"
	LayoutSection = "section"
	LayoutContent = "content"
	LayoutTwoCol  = "two_col"
)

// Parse decodes a deck from the tool's JSON input, normalizing themes and
// layouts and rejecting anything that could not become a page.
func Parse(raw string) (Deck, error) {
	var d Deck
	if strings.TrimSpace(raw) == "" {
		return Deck{}, fmt.Errorf("no deck was supplied")
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return Deck{}, fmt.Errorf("the deck could not be read: %w", err)
	}
	if strings.TrimSpace(d.Title) == "" {
		return Deck{}, fmt.Errorf("a deck needs a title")
	}
	if len(d.Slides) == 0 {
		return Deck{}, fmt.Errorf("a deck needs at least one slide")
	}
	for i := range d.Slides {
		d.Slides[i].Layout = normalizeLayout(d.Slides[i].Layout)
	}
	d.Theme = normalizeTheme(d.Theme)
	return d, nil
}

// normalizeLayout maps an unknown or empty layout to the content layout, which
// is the safe default for any page that has bullets.
func normalizeLayout(layout string) string {
	switch strings.ToLower(strings.TrimSpace(layout)) {
	case LayoutTitle, LayoutSection, LayoutTwoCol:
		return strings.ToLower(strings.TrimSpace(layout))
	default:
		return LayoutContent
	}
}

// TitleOf reads just the deck's title from its JSON body, without the cost of a
// full parse — used by storage when labelling the artifact.
func TitleOf(body string) string {
	var t struct {
		Title string `json:"title"`
	}
	_ = json.Unmarshal([]byte(body), &t)
	return strings.TrimSpace(t.Title)
}

// CountOf reads how many slides a deck's JSON body has.
func CountOf(body string) int {
	var c struct {
		Slides []Slide `json:"slides"`
	}
	_ = json.Unmarshal([]byte(body), &c)
	return len(c.Slides)
}
