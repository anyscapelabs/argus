package Shared

// PreferenceResult is one stored preference. Found distinguishes an unset key
// from one deliberately saved as an empty string.
type PreferenceResult struct {
	Value string       `json:"value"`
	Found bool         `json:"found"`
	Error *ActionError `json:"error,omitempty"`
}

// PreferencesResult is the whole preference map the frontend loads on startup.
type PreferencesResult struct {
	Values map[string]string `json:"values"`
	Error  *ActionError      `json:"error,omitempty"`
}
