package Models

import "strings"

type Status string

const (
	StatusStable Status = "stable"
	StatusBeta   Status = "beta"
	StatusAlpha  Status = "alpha"
)

type Source string

const (
	SourceCatalog Source = "catalog"
	SourceLive    Source = "live"
	SourceCustom  Source = "custom"
)

type Capabilities struct {
	Tools            bool   `json:"tools"`
	Reasoning        bool   `json:"reasoning"`
	Vision           bool   `json:"vision"`
	Files            bool   `json:"files"`
	StructuredOutput bool   `json:"structured_output"`
	Temperature      bool   `json:"temperature"`
	InterleavedField string `json:"interleaved_field,omitempty"`
}

type Limits struct {
	Context int64 `json:"context"`
	Input   int64 `json:"input"`
	Output  int64 `json:"output"`
}

type Cost struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	Reasoning  *float64 `json:"reasoning,omitempty"`
	CacheRead  *float64 `json:"cache_read,omitempty"`
	CacheWrite *float64 `json:"cache_write,omitempty"`
}

type Model struct {
	Ref            string        `json:"ref"`
	ProviderID     string        `json:"provider_id"`
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Family         string        `json:"family,omitempty"`
	Released       string        `json:"released,omitempty"`
	Updated        string        `json:"updated,omitempty"`
	Knowledge      string        `json:"knowledge,omitempty"`
	Capabilities   *Capabilities `json:"capabilities,omitempty"`
	CapsKnown      bool          `json:"caps_known"`
	Limits         *Limits       `json:"limits,omitempty"`
	Cost           *Cost         `json:"cost,omitempty"`
	Status         Status        `json:"status,omitempty"`
	Source         Source        `json:"source"`
	OpenWeights    bool          `json:"open_weights"`
	InputModality  []string      `json:"input_modalities,omitempty"`
	OutputModality []string      `json:"output_modalities,omitempty"`
}

func MakeRef(providerID, modelID string) string {
	return providerID + "/" + modelID
}

func SplitRef(ref string) (string, string) {
	if i := strings.Index(ref, "/"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ""
}

func ProducesText(m Model) bool {
	if len(m.OutputModality) == 0 {
		return false
	}
	for _, t := range m.OutputModality {
		if strings.EqualFold(t, "text") {
			return true
		}
	}
	return false
}

func IsDeprecated(m Model) bool {
	return strings.EqualFold(string(m.Status), "deprecated")
}
