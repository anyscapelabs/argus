package Integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Item represents an integration in the catalog.
type Item struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Type        string `json:"type"`
	ConfigPath  string `json:"configPath"`
	Enabled     bool   `json:"enabled"`
}

// Catalog holds integration items.
type Catalog struct {
	items []Item
}

// Load loads the integrations catalog from a JSON file.
func Load(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var items []Item
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, err
	}
	return &Catalog{items: items}, nil
}

// LoadFromDefault loads from the default locations.
func LoadFromDefault() (*Catalog, error) {
	candidates := []string{
		"Services/Integrations/data/catalog.json",
		"integrations/catalog.json",
		"data/integrations/catalog.json",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return Load(c)
		}
	}
	return &Catalog{items: []Item{}}, nil
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
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
