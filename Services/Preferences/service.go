package Preferences

import (
	"context"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/anyscapelabs/argus/Services/Shared"
)

// Service is the frontend's durable home for user preferences: theme, accent,
// font and motion choices, notification toggles. Webview localStorage is a
// cache for instant painting, but SQLite is the source of truth so a restart —
// or a cleared webview profile — cannot silently reset what the user chose.
type Service struct {
	Core *Shared.Core
}

func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	return s.Core.Bootstrap(ctx)
}

// All returns every stored preference in one call, used once when the UI boots.
func (s *Service) All() Shared.PreferencesResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.PreferencesResult{Error: aerr}
	}
	values, err := s.Core.DB.AllAppSettings()
	if err != nil {
		return Shared.PreferencesResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	return Shared.PreferencesResult{Values: values}
}

// Get returns one preference. A key that was never saved reports Found=false.
func (s *Service) Get(key string) Shared.PreferenceResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.PreferenceResult{Error: aerr}
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return Shared.PreferenceResult{Error: Shared.NewError(Shared.CodeBadInput, "Preference key is required")}
	}
	value, found, err := s.Core.DB.GetAppSetting(key)
	if err != nil {
		return Shared.PreferenceResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	return Shared.PreferenceResult{Value: value, Found: found}
}

// Set saves one preference. The value is stored verbatim; validation of enum
// values belongs to the caller so a new frontend can add choices without a
// backend migration.
func (s *Service) Set(key, value string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Preference key is required")}
	}
	if len(key) > 128 || len(value) > 4096 {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Preference is too large")}
	}
	if err := s.Core.DB.SetAppSetting(key, value); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	return Shared.ActionResult{Ok: true}
}
