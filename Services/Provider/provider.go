package Providers

import (
	"context"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/anyscapelabs/argus/Catalog"
	"github.com/anyscapelabs/argus/Keyring"
	"github.com/anyscapelabs/argus/Provider"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

type Service struct {
	Core *Shared.Core
}

func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	return s.Core.Bootstrap(ctx)
}

func (s *Service) ListProviders() []Shared.ProviderView {
	out := []Shared.ProviderView{}
	if err := s.Core.Ready(); err != nil {
		return out
	}
	cat := s.Core.Store.Get()
	for _, p := range cat.Providers() {
		out = append(out, s.ProviderView(cat, p))
	}
	return out
}

func (s *Service) ProviderView(cat *Catalog.Catalog, p Provider.Provider) Shared.ProviderView {
	config, found, _ := s.Core.DB.GetProviderConfig(p.ID)
	view := Shared.ProviderView{
		ID:         p.ID,
		Name:       p.Name,
		Logo:       p.Logo,
		Monogram:   Monogram(p.Name),
		NoAuth:     p.NoAuth,
		KeyHint:    p.KeyHint,
		KeyHelpURL: p.KeyHelpURL,
		DocURL:     p.DocURL,
		Configured: found,
		ModelCount: len(cat.ByProvider(p.ID)),
		Order:      p.Order,
	}
	if found {
		if refs, err := s.Core.DB.ListProviderSelection(p.ID); err == nil {
			view.SelectedModelCount = len(refs)
		}
	}
	if found {
		view.KeyLast4 = config.KeyLast4
		view.ValidatedAt = config.ValidatedAt
	}
	return view
}

func (s *Service) ValidateAndSaveKey(providerID, key string, refs []string, baseURLOverride string) Shared.SaveKeyResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.SaveKeyResult{Error: aerr}
	}
	overlayProvider, ok := s.Core.Overlay.Lookup(providerID)
	if !ok {
		return Shared.SaveKeyResult{Error: Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")}
	}
	provider, found := s.Core.Store.Get().Provider(providerID)
	if !found {
		return Shared.SaveKeyResult{Error: Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")}
	}
	normalized := ""
	if !provider.NoAuth {
		valid, aerr := NormalizeKey(key)
		if aerr != nil {
			return Shared.SaveKeyResult{Error: aerr}
		}
		normalized = valid
	}

	existing, _, _ := s.Core.DB.GetProviderConfig(providerID)
	override := existing.BaseURLOverride
	if baseURLOverride != "" {
		override = baseURLOverride
	}
	cfgForLive := Storage.ProviderConfig{BaseURLOverride: override}

	// preserve original key for rollback
	var originalKey string
	hasOriginal := false
	if !provider.NoAuth {
		if k, err := Keyring.Get(providerID); err == nil {
			originalKey = k
			hasOriginal = true
		}
	}

	// clear cache so stale results can't mask a bad key
	s.Core.DropLive(providerID)

	if !provider.NoAuth {
		if err := Keyring.Set(providerID, normalized); err != nil {
			return Shared.SaveKeyResult{Error: Shared.NewError(Shared.CodeStorage, "The secrets store refused the key. Nothing was changed")}
		}
	}

	live := s.Core.FetchLive(context.Background(), provider, overlayProvider.ExtraHeaders, cfgForLive, true)
	if live == nil || live.Stale {
		// rollback key
		if !provider.NoAuth {
			if hasOriginal {
				_ = Keyring.Set(providerID, originalKey)
			} else {
				_ = Keyring.Delete(providerID)
			}
		}
		if err := s.Core.LiveError(providerID); err != nil {
			return Shared.SaveKeyResult{Error: err}
		}
		return Shared.SaveKeyResult{Error: Shared.NewError(Shared.CodeProviderError, "The provider could not be reached to validate the key")}
	}

	secretRef := ""
	var last4 string
	if !provider.NoAuth {
		secretRef = Keyring.Ref(providerID)
		last4 = LastFour(normalized)
	}
	saved := Storage.ProviderConfig{
		ProviderID:      providerID,
		SecretRef:       secretRef,
		KeyLast4:        last4,
		ValidatedAt:     time.Now().UTC().Format(time.RFC3339),
		BaseURLOverride: override,
	}
	if err := s.Core.DB.SaveProviderConfig(saved); err != nil {
		// rollback key
		if !provider.NoAuth {
			if hasOriginal {
				_ = Keyring.Set(providerID, originalKey)
			} else {
				_ = Keyring.Delete(providerID)
			}
		}
		return Shared.SaveKeyResult{Error: Shared.NewError(Shared.CodeStorage, "The key could not be saved. Nothing was changed")}
	}
	// save selection in same logical step
	if err := s.Core.DB.SetProviderSelection(providerID, refs); err != nil {
		return Shared.SaveKeyResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}

	s.Core.DropLive(providerID)
	s.Core.EmitEvent(Shared.EventProvidersChanged, nil)
	if s.Core.PickerHook == nil {
		return Shared.SaveKeyResult{Ok: true}
	}
	picker := s.Core.PickerHook(providerID, Shared.PickerOptions{})
	if picker.Error != nil {
		return Shared.SaveKeyResult{Ok: true, Picker: nil}
	}
	return Shared.SaveKeyResult{Ok: true, Picker: picker.Picker}
}

func (s *Service) RemoveKey(providerID string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	overlayProvider, ok := s.Core.Overlay.Lookup(providerID)
	if !ok {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")}
	}
	_, found, err := s.Core.DB.GetProviderConfig(providerID)
	if err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if !found {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotConfigured, "This provider has no saved key")}
	}
	cleared, err := s.Core.DB.RemoveProviderSlots(providerID)
	if err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if err := s.Core.DB.ClearProviderSelection(providerID); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if err := s.Core.DB.RemoveProviderConfig(providerID); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if !overlayProvider.NoAuth {
		if err := Keyring.Delete(providerID); err != nil {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, "The secrets store refused to remove the key")}
		}
	}
	s.Core.DropLive(providerID)
	s.Core.EmitEvent(Shared.EventProvidersChanged, nil)
	if len(cleared) > 0 {
		s.Core.EmitEvent(Shared.EventSlotsChanged, nil)
	}
	return Shared.ActionResult{Ok: true, ClearedSlots: cleared}
}

func LastFour(key string) string {
	if len(key) <= 4 {
		return key
	}
	return key[len(key)-4:]
}

func NormalizeKey(key string) (string, *Shared.ActionError) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(key, "\r", ""), "\n", ""))
	if len(trimmed) >= 2 && (trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"' || trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'') {
		trimmed = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	}
	if len(trimmed) >= 7 && strings.EqualFold(trimmed[:7], "Bearer ") {
		trimmed = strings.TrimSpace(trimmed[7:])
	}
	if trimmed == "" {
		return "", Shared.NewError(Shared.CodeKeyFormat, "Paste an API key first")
	}
	if strings.ContainsAny(trimmed, " \t") {
		return "", Shared.NewError(Shared.CodeKeyFormat, "The API key contains whitespace")
	}
	if len(trimmed) < 8 {
		return "", Shared.NewError(Shared.CodeKeyFormat, "The API key looks too short")
	}
	return trimmed, nil
}

func Monogram(name string) string {
	s := strings.TrimSpace(name)
	if s == "" {
		return "?"
	}
	return strings.ToUpper(string([]rune(s)[0]))
}
