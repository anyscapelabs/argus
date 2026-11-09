package Shared

type ArtifactView struct {
	ID      string `json:"id"`
	AgentID string `json:"agent_id"`
	Title   string `json:"title"`
	// Format is the authoring format, and there is only one: "markdown". It
	// stays in the row because the export targets are formats a document can
	// become, never formats it is written in.
	Format string `json:"format"`
	Body   string `json:"body,omitempty"`
	// Meta is the document's front matter — title, author, date, language,
	// paper, orientation — the fields the exporters honour.
	Meta      map[string]string `json:"meta,omitempty"`
	Words     int               `json:"words"`
	Bytes     int               `json:"bytes"`
	Version   int               `json:"version"`
	CreatedAt int64             `json:"created_at"`
	UpdatedAt int64             `json:"updated_at"`
}

type ArtifactsResult struct {
	Artifacts []ArtifactView `json:"artifacts"`
	Error     *ActionError   `json:"error,omitempty"`
}

type ArtifactResult struct {
	Artifact ArtifactView `json:"artifact"`
	Found    bool         `json:"found"`
	Error    *ActionError `json:"error,omitempty"`
}

// ArtifactWriteResult is the reply to a writer. A document that broke the rules
// is refused rather than stored, and Rejected plus Findings is how the agent
// hears that: Error stays nil, because the call worked and the answer was no.
type ArtifactWriteResult struct {
	Artifact ArtifactView `json:"artifact"`
	Rejected bool         `json:"rejected"`
	Findings string       `json:"findings"`
	Error    *ActionError `json:"error,omitempty"`
}

// ExportFormatView is one entry of the export menu. A format the machine cannot
// produce is still listed, disabled, with Required explaining what to install —
// hiding an option the user can fix by installing something is worse than
// showing it greyed out.
type ExportFormatView struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Extension string `json:"extension"`
	Available bool   `json:"available"`
	Hint      string `json:"hint"`
	Required  string `json:"required,omitempty"`
}

type ExportFormatsResult struct {
	Formats []ExportFormatView `json:"formats"`
	Error   *ActionError       `json:"error,omitempty"`
}

// ArtifactExportResult reports where the derived file landed. Path is absolute,
// because the panel offers to reveal it.
type ArtifactExportResult struct {
	Path   string       `json:"path"`
	Format string       `json:"format"`
	Bytes  int          `json:"bytes"`
	Error  *ActionError `json:"error,omitempty"`
}
