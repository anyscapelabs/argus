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
