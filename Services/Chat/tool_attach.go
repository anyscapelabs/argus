package Chat

import (
	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Router"
	"github.com/anyscapelabs/argus/Services/Search"
	"github.com/anyscapelabs/argus/Services/Shared"
	"strings"
)

// activitySources converts search hits into the compact view the live
// indicator cycles through; host names matter more than full URLs there.
func activitySources(results []Search.Result) []Shared.ActivitySource {
	out := make([]Shared.ActivitySource, 0, 4)
	for _, r := range results {
		if len(out) == cap(out) {
			break
		}
		title := r.Title
		if i := strings.IndexRune(title, '\n'); i >= 0 {
			title = title[:i]
		}
		if len(title) > 90 {
			title = title[:90]
		}
		host := hostOf(r.URL)
		if host == "" || title == "" {
			continue
		}
		out = append(out, Shared.ActivitySource{Host: host, Title: title})
	}
	return out
}

// baseToolDefs is the tool list for every model that can take tools. It lives in
// one function because attachTools and modelSupportsTools both need it: when the
// two lists were written out separately, adding a tool to one and not the other
// silently changed what the capability probe tested.
//
// browser is included only when this machine has a browser to drive — a tool the
// model cannot possibly succeed at is worse than not offering it.
func (s *Service) baseToolDefs() []Protocol.ToolDef {
	defs := []Protocol.ToolDef{terminalToolDef, readFileToolDef, writeFileToolDef, grepFilesToolDef,
		webSearchToolDef, timeLocationToolDef, rememberToolDef, forgetToolDef,
		scheduleReminderToolDef, createRoutineToolDef, listScheduledToolDef, cancelScheduledToolDef,
		writeArtifactToolDef, createSlidesToolDef, conversationReadToolDef, s.connectToolDef()}
	if s.Browser != nil {
		if ok, _ := s.Browser.Available(); ok {
			defs = append(defs, browserToolDef)
		}
	}
	return defs
}

// attachTools turns on tool calling when the model supports it. The
// connector schemas ride along only for the integrations the replayed
// history already touched; everything else waits behind the loader. Returns
// the system prompt suffix describing the tools (empty when tools are off).
func (s *Service) attachTools(req *Protocol.Request, target *Protocol.Target, activeServers []string) bool {
	baseTools := s.baseToolDefs()
	baseTools = append(baseTools, mcpLoadToolDef)
	for _, id := range activeServers {
		baseTools = append(baseTools, s.mcpServerToolDefs(id)...)
	}
	if !s.modelSupportsTools(target) {
		return false
	}
	req.Tools = baseTools
	return true
}

func (s *Service) modelSupportsTools(target *Protocol.Target) bool {
	ad, err := Router.AdapterFor(target)
	if err != nil {
		return false
	}
	scratch := &Protocol.Request{Tools: s.baseToolDefs()}
	scratch.Tools = append(scratch.Tools, mcpLoadToolDef)
	Protocol.ClampRequest(scratch, ad.Capabilities(Protocol.ModelInfo{ID: target.ModelID}))
	return len(scratch.Tools) > 0
}

// maxToolResultChars bounds what one tool result can add to the transcript.
// A `find /` or a fat email should cost the run a couple of KB, not the
// context window.
const maxToolResultChars = 8000

// browserResultChars is the exception: an accessibility snapshot of a real page
// is dense, useful and legitimately longer than a shell command's output. The
// snapshot itself caps what it prints, so this is headroom, not a free pass.
const browserResultChars = 20000

func maxResultChars(tool string) int {
	if tool == "browser" {
		return browserResultChars
	}
	return maxToolResultChars
}
