package Shared

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/anyscapelabs/argus/Catalog"
	"github.com/anyscapelabs/argus/Provider"
	"github.com/anyscapelabs/argus/Storage"
)

type Core struct {
	once    sync.Once
	bootErr error

	Overlay *Provider.Overlay
	DB      *Storage.DB
	Store   *Catalog.Store

	emit       func(string, any)
	PickerHook func(providerID string, options PickerOptions) PickerResult

	liveMu sync.Mutex
	live   map[string]*liveEntry
	lockCS sync.Map
}

func NewCore() *Core {
	return &Core{live: map[string]*liveEntry{}}
}

func (c *Core) Notifier(fn func(string, any)) {
	c.emit = fn
}

func (c *Core) EmitEvent(name string, data any) {
	if c.emit != nil {
		c.emit(name, data)
	}
}

func (c *Core) Bootstrap(ctx context.Context) error {
	c.once.Do(func() {
		c.bootErr = c.bootstrap(ctx)
	})
	return c.bootErr
}

func (c *Core) bootstrap(ctx context.Context) error {
	overlay, err := Provider.Load()
	if err != nil {
		return err
	}
	c.Overlay = overlay

	dbPath, err := Storage.DefaultPath()
	if err != nil {
		return err
	}
	db, err := Storage.Open(dbPath)
	if err != nil {
		return err
	}
	c.DB = db

	cacheDir, err := CacheDir()
	if err != nil {
		return err
	}
	store, err := Catalog.NewStore(overlay, cacheDir, func() {
		c.EmitEvent(EventCatalogUpdated, nil)
	})
	if err != nil {
		return err
	}
	c.Store = store
	Catalog.StartBackground(store, ctx)
	return nil
}

func (c *Core) Ready() *ActionError {
	if c.Overlay == nil || c.DB == nil || c.Store == nil {
		return NewError(CodeStorage, "The backend is still starting. Try again in a moment")
	}
	return nil
}

func (c *Core) Shutdown() error {
	if c.DB != nil {
		return c.DB.Close()
	}
	return nil
}

func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("catalog: cache directory unavailable: %w", err)
	}
	return filepath.Join(base, "argus"), nil
}
