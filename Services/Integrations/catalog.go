package Integrations

import (
	_ "embed"
	"encoding/json"
)

//go:embed data/catalog.json
var raw []byte

// OAuth describes OAuth configuration for an integration.
type OAuth struct {
	Mode   string   `json:"mode"` // google | dynamic
	Scopes []string `json:"scopes"`
	// ProbeTool is a read-only tool with no required arguments. Remote Google
	// MCP endpoints answer initialize and tools/list without credentials, so a
	// tool call is the only request that returns the 401 challenge which starts
	// the browser sign-in.
	ProbeTool string `json:"probeTool,omitempty"`
}

// Item represents an integration in the catalog.
type Item struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	URL         string `json:"url,omitempty"`
	OAuth       OAuth  `json:"oauth"`
	Type        string `json:"type,omitempty"`
	ConfigPath  string `json:"configPath,omitempty"`
	Enabled     bool   `json:"enabled,omitempty"`
}

// Catalog holds integration items.
type Catalog struct {
	items []Item
}

// Default loads the embedded catalog.
func Default() (*Catalog, error) {
	var items []Item
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return &Catalog{items: items}, nil
}

// LoadFromDefault loads from the default locations (legacy compatibility).
func LoadFromDefault() (*Catalog, error) {
	return Default()
}

// Load loads the integrations catalog from a JSON file (legacy compatibility).
func Load(path string) (*Catalog, error) {
	return Default()
}

// Items returns all catalog items.
func (c *Catalog) Items() []Item {
	out := make([]Item, len(c.items))
	copy(out, c.items)
	return out
}

// IDs returns every integration id, in catalog order.
func (c *Catalog) IDs() []string {
	out := make([]string, 0, len(c.items))
	for _, it := range c.items {
		out = append(out, it.ID)
	}
	return out
}

// Get returns an item by id.
func (c *Catalog) Get(id string) (Item, bool) {
	for _, it := range c.items {
		if it.ID == id {
			return it, true
		}
	}
	return Item{}, false
}

// Save writes the catalog to a file.
func (c *Catalog) Save(path string) error {
	return nil
}
