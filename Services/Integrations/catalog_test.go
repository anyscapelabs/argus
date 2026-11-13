package Integrations

import "testing"

func TestDefaultCatalogIncludesProactiveConnectors(t *testing.T) {
	catalog, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"cloudflare":    false,
		"context7":      false,
		"google-slides": false,
		"atlassian":     false,
		"sentry":        false,
	}
	seen := map[string]bool{}
	for _, item := range catalog.Items() {
		if seen[item.ID] {
			t.Errorf("duplicate integration id %q", item.ID)
		}
		seen[item.ID] = true
		if _, ok := want[item.ID]; ok {
			want[item.ID] = true
			if item.URL == "" || item.Icon == "" {
				t.Errorf("integration %q is incomplete: %+v", item.ID, item)
			}
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("missing proactive integration %q", id)
		}
	}
}
