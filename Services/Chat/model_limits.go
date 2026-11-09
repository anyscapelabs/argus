package Chat

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Keyring"
	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Router"
)

func clampToModel(s *Service, modelRef, providerID, modelID string, target *Protocol.Target, maxOut *int64) {
	if maxOut == nil {
		return
	}
	ad, aerr := Router.AdapterFor(target)
	if aerr != nil {
		return
	}
	info := Protocol.ModelInfo{ID: modelID}
	if m, ok := s.Core.Store.Get().Get(modelRef); ok && m.Limits != nil {
		info.ContextLen = m.Limits.Context
		info.MaxOutput = m.Limits.Output
	} else if rows, lerr := s.Core.DB.ListCustomModels(providerID); lerr == nil {
		for _, cm := range rows {
			if cm.ModelID == modelID {
				info.ContextLen = cm.ContextWindow
				info.MaxOutput = cm.MaxOutput
				break
			}
		}
	}
	if info.MaxOutput <= 0 {
		return
	}
	scratch := &Protocol.Request{MaxOutputTokens: *maxOut}
	Protocol.ClampRequest(scratch, ad.Capabilities(info))
	*maxOut = scratch.MaxOutputTokens
}

func (s *Service) baseURLOverride(providerID string) string {
	cfg, found, err := s.Core.DB.GetProviderConfig(providerID)
	if err != nil || !found {
		return ""
	}
	return strings.TrimSpace(cfg.BaseURLOverride)
}

func getKey(providerID string) (string, error) {
	return Keyring.GetFor(providerID)
}

func (s *Service) allowedModel(modelID string) bool {
	if s.Core == nil || s.Core.DB == nil {
		return true
	}
	payload, _, found, err := s.Core.DB.GetAccountEntitlements()
	if err != nil || !found || payload == "" {
		return true
	}
	var ent struct {
		Allowlist []any `json:"allowlist"`
	}
	if err := json.Unmarshal([]byte(payload), &ent); err != nil || len(ent.Allowlist) == 0 {
		return true
	}
	for _, item := range ent.Allowlist {
		switch v := item.(type) {
		case string:
			if strings.EqualFold(strings.TrimSpace(v), modelID) {
				return true
			}
		case map[string]any:
			if id, _ := v["id"].(string); strings.EqualFold(strings.TrimSpace(id), modelID) {
				return true
			}
		}
	}
	return false
}

// sCtx gives the location resolver its own bounded network budget without
// touching the run lifecycle.
func sCtx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = cancel // lifetime bounded by the inner request timeout
	return ctx
}
