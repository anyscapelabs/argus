package Chat

import (
	"encoding/json"

	"github.com/anyscapelabs/argus/Protocol"
)

// createSlidesToolDef is the presentation tool. Like write_artifact it authors
// a kept artifact rather than touching the user's disk, but it is structured:
// the model hands over slide JSON (title, theme, slides) and the app renders a
// real .pptx. The theme lets the model choose colors and fonts per deck.
var createSlidesToolDef = Protocol.ToolDef{
	Name: "create_slides",
	Description: "Build a slide deck as an artifact: title, a theme (colors and fonts the deck should use) " +
		"and an array of slides. The app turns it into a .pptx the user can save from the Artifacts panel. " +
		"Free, no approval, nothing touches the user's disk. Use it when the ask is a presentation, not a document.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"title": {
				"type": "string",
				"description": "Deck title, e.g. \"Q3 Sales Review\""
			},
			"theme": {
				"type": "object",
				"description": "Visual identity. All fields optional; anything omitted falls back to the default dark theme.",
				"properties": {
					"name": {"type": "string", "description": "Theme name for the user, e.g. \"sunset\" or \"corporate blue\"."},
					"background": {"type": "string", "description": "Background color as #rgb or #rrggbb hex."},
					"accent": {"type": "string", "description": "Accent color (title bars, highlights) as hex."},
					"text": {"type": "string", "description": "Body text color as hex."},
					"heading_font": {"type": "string", "description": "Font name for titles, e.g. \"Arial\", \"Georgia\"."},
					"body_font": {"type": "string", "description": "Font name for body text."}
				}
			},
			"slides": {
				"type": "array",
				"description": "The pages of the deck.",
				"items": {
					"type": "object",
					"properties": {
						"layout": {
							"type": "string",
							"enum": ["title", "section", "content", "two_col"],
							"description": "title=cover, section=divider, content=title+bullets, two_col=two bullet columns."
						},
						"title": {"type": "string", "description": "The slide's headline."},
						"subtitle": {"type": "string", "description": "For title slides, the line under the headline."},
						"bullets": {"type": "array", "items": {"type": "string"}, "description": "For content slides, the bullet points."},
						"left": {"type": "array", "items": {"type": "string"}, "description": "For two_col slides, the left column."},
						"right": {"type": "array", "items": {"type": "string"}, "description": "For two_col slides, the right column."},
						"notes": {"type": "string", "description": "Speaker notes (informational)."}
					},
					"required": ["title"]
				}
			}
		},
		"required": ["title", "slides"]
	}`),
}
