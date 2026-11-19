package Preferences

import (
	"path/filepath"
	"testing"

	"github.com/anyscapelabs/argus/Catalog"
	"github.com/anyscapelabs/argus/Provider"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func testService(t *testing.T) (*Service, *Storage.DB) {
	t.Helper()
	db, err := Storage.Open(filepath.Join(t.TempDir(), "preferences.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	core := &Shared.Core{DB: db, Overlay: &Provider.Overlay{}, Store: &Catalog.Store{}}
	return &Service{Core: core}, db
}

func TestSetGetAndAll(t *testing.T) {
	svc, _ := testService(t)

	if res := svc.Set("argus-accent", "purple"); res.Error != nil {
		t.Fatalf("Set: %v", res.Error)
	}
	if res := svc.Set("argus-theme", "dark"); res.Error != nil {
		t.Fatalf("Set: %v", res.Error)
	}

	got := svc.Get("argus-accent")
	if got.Error != nil || !got.Found || got.Value != "purple" {
		t.Fatalf("Get accent = %+v", got)
	}

	all := svc.All()
	if all.Error != nil {
		t.Fatalf("All: %v", all.Error)
	}
	if all.Values["argus-accent"] != "purple" || all.Values["argus-theme"] != "dark" {
		t.Fatalf("All = %+v", all.Values)
	}
}

func TestGetMissingKey(t *testing.T) {
	svc, _ := testService(t)
	got := svc.Get("never-saved")
	if got.Error != nil || got.Found || got.Value != "" {
		t.Fatalf("Get missing = %+v", got)
	}
}

func TestOverwriteAndEmptyKey(t *testing.T) {
	svc, _ := testService(t)
	if res := svc.Set("argus-accent", "blue"); res.Error != nil {
		t.Fatalf("Set: %v", res.Error)
	}
	if res := svc.Set("argus-accent", "amber"); res.Error != nil {
		t.Fatalf("Set again: %v", res.Error)
	}
	if got := svc.Get("argus-accent"); got.Value != "amber" {
		t.Fatalf("overwrite = %+v", got)
	}
	if res := svc.Set("  ", "x"); res.Error == nil {
		t.Fatal("empty key should be rejected")
	}
}
