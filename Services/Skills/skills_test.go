package Skills

import "testing"

func TestBusinessSkillsHaveEmbeddedDocs(t *testing.T) {
	registry := &Service{}
	for _, id := range []string{
		"humanizer",
		"finance",
		"accounting",
		"sales",
		"marketing",
		"customer-support",
		"data-analysis",
	} {
		meta, ok := registry.Get(id)
		if !ok {
			t.Fatalf("skill %q missing from registry", id)
		}
		if meta.AutoEnable {
			t.Fatalf("skill %q should be opt-in", id)
		}
		doc, ok := registry.ReadSkill("default", id)
		if !ok || doc.Body == "" {
			t.Fatalf("skill %q has no embedded document", id)
		}
	}
}
