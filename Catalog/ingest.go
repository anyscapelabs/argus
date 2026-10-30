package Catalog

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Provider"
)

const (
	maxWarnings   = 200
	maxNameLength = 200
)

var dropTypes = map[string]bool{
	"embedding":      true,
	"embed":          true,
	"image":          true,
	"audio":          true,
	"realtime":       true,
	"transcription":  true,
	"speech":         true,
	"moderation":     true,
	"rerank":         true,
	"guard":          true,
	"classification": true,
}

type rawModelDev map[string]json.RawMessage

func Build(o *Provider.Overlay, body []byte, buildTime time.Time) (*Catalog, error) {
	if body == nil {
		return nil, fmt.Errorf("catalog: nil body")
	}
	var raw rawModelDev
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("catalog: canonical model file is not valid JSON: %w", err)
	}

	cat := &Catalog{
		providers:  make([]Provider.Provider, 0, len(o.Providers)),
		models:     make(map[string]Models.Model),
		byProvider: make(map[string][]Models.Model),
		report: BuildReport{
			BuildTime:       buildTime.UTC(),
			ProvidersNoData: []string{},
			Warnings:        []string{},
		},
	}

	for _, op := range o.Ordered() {
		provider := Provider.Provider{
			ID:         op.ID,
			Name:       op.Name(),
			Adapter:    op.Adapter,
			BaseURL:    op.BaseURL,
			ModelsPath: op.ModelsPath,
			NoAuth:     op.NoAuth,
			KeyHint:    op.KeyHint,
			KeyHelpURL: op.KeyHelpURL,
			DocURL:     op.DocURL,
			Order:      op.Order,
			Logo:       op.Logo,
		}
		rawBody, found := raw[op.ID]
		if !found {
			cat.report.ProvidersNoData = append(cat.report.ProvidersNoData, op.ID)
			cat.providers = append(cat.providers, provider)
			continue
		}
		provider.InCatalogData = true
		models := ingestProvider(&op, rawBody, &cat.report)
		SortModels(models)
		cat.byProvider[op.ID] = models
		for _, m := range models {
			cat.models[strings.ToLower(m.Ref)] = m
		}
		cat.providers = append(cat.providers, provider)
	}

	cat.report.ProviderCount = len(cat.providers)
	cat.report.ModelCount = len(cat.models)
	return cat, nil
}

func ingestProvider(op *Provider.OverlayProvider, data []byte, report *BuildReport) []Models.Model {
	warn := func(w string) {
		if len(report.Warnings) < maxWarnings {
			report.Warnings = append(report.Warnings, w)
		} else {
			report.WarningsTruncated = true
		}
	}

	var rp struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(data, &rp); err != nil {
		warn(fmt.Sprintf("provider %s unparseable: %v", op.ID, err))
		return nil
	}

	hideSet := make(map[string]bool, len(op.Hide))
	for _, id := range op.Hide {
		hideSet[strings.ToLower(id)] = true
	}

	out := make([]Models.Model, 0, len(rp.Models))
	for rawID, raw := range rp.Models {
		var rm struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Family      string `json:"family"`
			Attachment  bool   `json:"attachment"`
			Reasoning   bool   `json:"reasoning"`
			ToolCall    bool   `json:"tool_call"`
			Structured  bool   `json:"structured_output"`
			Temperature bool   `json:"temperature"`
			Modalities  *struct {
				Input  []string `json:"input"`
				Output []string `json:"output"`
			} `json:"modalities"`
			OpenWeights bool `json:"open_weights"`
			Limit       *struct {
				Context *float64 `json:"context"`
				Input   *float64 `json:"input"`
				Output  *float64 `json:"output"`
			} `json:"limit"`
			Cost *struct {
				Input      *float64 `json:"input"`
				Output     *float64 `json:"output"`
				Reasoning  *float64 `json:"reasoning"`
				CacheRead  *float64 `json:"cache_read"`
				CacheWrite *float64 `json:"cache_write"`
			} `json:"cost"`
			Status      string          `json:"status"`
			ReleaseDate string          `json:"release_date"`
			LastUpdated string          `json:"last_updated"`
			Knowledge   string          `json:"knowledge"`
			Interleaved json.RawMessage `json:"interleaved"`
			Type        string          `json:"type"`
		}
		if err := json.Unmarshal(raw, &rm); err != nil {
			warn(fmt.Sprintf("%s/%s unparseable: %v", op.ID, rawID, err))
			continue
		}
		id := rm.ID
		if id == "" {
			id = rawID
		}
		ref := Models.MakeRef(op.ID, id)

		if hideSet[strings.ToLower(id)] {
			continue
		}
		status := normalizeStatus(rm.Status)
		if status == "" {
			continue
		}
		if len(rm.Modalities.Output) > 0 && !hasText(rm.Modalities.Output) {
			continue
		}
		if t := strings.ToLower(rm.Type); t != "" && dropTypes[t] {
			continue
		}

		name := rm.Name
		if name == "" {
			name = id
		}
		if override, ok := op.NameOverrides[id]; ok {
			name = override
		}
		name = capString(name)
		family := capString(rm.Family)

		caps := &Models.Capabilities{
			Tools:            rm.ToolCall,
			Reasoning:        rm.Reasoning,
			Vision:           hasImage(rm.Modalities.Input),
			Files:            rm.Attachment,
			StructuredOutput: rm.Structured,
			Temperature:      rm.Temperature,
			InterleavedField: interleavedField(rm.Interleaved),
		}

		inMod, outMod := []string{}, []string{}
		if rm.Modalities != nil {
			inMod = append(inMod, rm.Modalities.Input...)
			outMod = append(outMod, rm.Modalities.Output...)
		}

		model := Models.Model{
			Ref:            ref,
			ProviderID:     op.ID,
			ID:             id,
			Name:           name,
			Family:         family,
			Released:       capString(rm.ReleaseDate),
			Updated:        capString(rm.LastUpdated),
			Knowledge:      capString(rm.Knowledge),
			Capabilities:   caps,
			CapsKnown:      true,
			Status:         status,
			Source:         Models.SourceCatalog,
			OpenWeights:    rm.OpenWeights,
			InputModality:  inMod,
			OutputModality: outMod,
		}
		if rm.Limit != nil {
			model.Limits = &Models.Limits{
				Context: int64From(rm.Limit.Context),
				Input:   int64From(rm.Limit.Input),
				Output:  int64From(rm.Limit.Output),
			}
		}
		if rm.Cost != nil {
			model.Cost = &Models.Cost{
				Input:      rm.Cost.Input,
				Output:     rm.Cost.Output,
				Reasoning:  rm.Cost.Reasoning,
				CacheRead:  rm.Cost.CacheRead,
				CacheWrite: rm.Cost.CacheWrite,
			}
		}

		out = append(out, model)
	}
	return out
}

func normalizeStatus(s string) Models.Status {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "deprecated":
		return ""
	case "alpha":
		return Models.StatusAlpha
	case "beta":
		return Models.StatusBeta
	default:
		return Models.StatusStable
	}
}

func hasText(list []string) bool {
	for _, t := range list {
		if strings.EqualFold(t, "text") {
			return true
		}
	}
	return false
}

func hasImage(list []string) bool {
	for _, t := range list {
		if strings.EqualFold(t, "image") {
			return true
		}
	}
	return false
}

func int64From(p *float64) int64 {
	if p == nil {
		return 0
	}
	return int64(*p)
}

func interleavedField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Field != "" {
		return obj.Field
	}
	return ""
}

func capString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxNameLength {
		s = s[:maxNameLength]
	}
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' || r >= 0x20 {
			b.WriteRune(r)
		}
	}
	return b.String()
}
