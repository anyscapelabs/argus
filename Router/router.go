package Router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Protocol/Anthropic"
	"github.com/anyscapelabs/argus/Protocol/Gemini"
	"github.com/anyscapelabs/argus/Protocol/OpenAI"
	"github.com/anyscapelabs/argus/Provider"
)

const errorBodyCap = 1 << 20

var client = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return fmt.Errorf("redirects are not allowed for model calls")
	},
}

type Router struct{}

func New() *Router {
	return &Router{}
}

type FixedQuirks struct {
	ChatPath       string `json:"chat_path,omitempty"`
	MaxTokensField string `json:"max_tokens_field,omitempty"`
	SystemRole     string `json:"system_role,omitempty"`
	StreamUsage    bool   `json:"stream_usage,omitempty"`
	ReasoningField string `json:"reasoning_field,omitempty"`
	ParallelTools  bool   `json:"parallel_tools,omitempty"`
}

type TargetOptions struct {
	ExtraHeaders map[string]string
	Quirks       *Provider.Quirks
}

func TargetFor(provider Provider.Provider, modelID string, apiKey string, opts TargetOptions) (Protocol.Target, error) {
	if modelID == "" {
		return Protocol.Target{}, errors.New("a model id is required")
	}
	base := strings.TrimSuffix(provider.BaseURL, "/")
	if base == "" {
		return Protocol.Target{}, errors.New("the provider has no base URL")
	}
	t := Protocol.Target{
		Family:       Protocol.AdapterFamily(provider.Adapter),
		BaseURL:      base,
		ModelID:      modelID,
		APIKey:       apiKey,
		ExtraHeaders: opts.ExtraHeaders,
	}
	quirks := opts.Quirks
	if quirks == nil {
		quirks = provider.Quirks
	}
	t.Quirks = QuirksRaw(quirks)
	return t, nil
}

func QuirksRaw(q *Provider.Quirks) json.RawMessage {
	if q == nil {
		return nil
	}
	fixed := FixedQuirks{
		ChatPath:       q.ChatPath,
		MaxTokensField: q.MaxTokensField,
		SystemRole:     q.SystemRole,
		StreamUsage:    q.HasStreamUsage(),
		ReasoningField: q.ReasoningField,
		ParallelTools:  q.AllowsParallelTools(),
	}
	raw, err := json.Marshal(fixed)
	if err != nil {
		return nil
	}
	return raw
}

func adapterFor(t *Protocol.Target) (Protocol.Adapter, error) {
	switch t.Family {
	case Protocol.FamilyOpenAICompat:
		return OpenAI.New(t.Quirks)
	case Protocol.FamilyAnthropic:
		return Anthropic.New(t.Quirks)
	case Protocol.FamilyGemini:
		return Gemini.New(t.Quirks)
	}
	return nil, fmt.Errorf("no adapter for protocol family %q", t.Family)
}

func (r *Router) Run(ctx context.Context, req *Protocol.Request, target *Protocol.Target) iter.Seq[Protocol.Event] {
	return func(yield func(Protocol.Event) bool) {
		g := &gate{ok: true}
		g.yield = yield
		if req == nil || target == nil || target.ModelID == "" {
			g.fail(Protocol.NewError(Protocol.CodeBadInput, "A model id is required to call the provider"))
			return
		}
		wireReq := *req
		wireReq.Model = target.ModelID
		ad, err := adapterFor(target)
		if err != nil {
			g.fail(Protocol.NewError(Protocol.CodeBadInput, "This provider speaks no supported protocol"))
			return
		}
		httpReq, err := ad.BuildRequest(ctx, &wireReq, target)
		if err != nil {
			g.fail(Protocol.Errorf(Protocol.CodeBadInput, "The request for this model could not be built (%s)", err.Error()))
			return
		}
		if ctx.Err() != nil {
			g.fail(Protocol.CancelledError())
			return
		}
		resp, err := client.Do(httpReq)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				g.fail(Protocol.CancelledError())
				return
			}
			g.fail(Protocol.NetworkError(err))
			return
		}
		if resp.StatusCode != http.StatusOK {
			body := readLimited(resp.Body, errorBodyCap)
			resp.Body.Close()
			g.fail(ad.MapError(resp.StatusCode, body, retryAfter(resp.Header.Get("Retry-After"))))
			return
		}
		for ev := range ad.ParseStream(resp.Body) {
			if !g.emit(ev) {
				resp.Body.Close()
				return
			}
		}
		resp.Body.Close()
	}
}

func readLimited(body io.Reader, cap int64) []byte {
	data, _ := io.ReadAll(io.LimitReader(body, cap))
	return data
}

func retryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if secs, err := time.ParseDuration(raw + "s"); err == nil && secs >= 0 {
		return secs
	}
	return 0
}

type gate struct {
	yield func(Protocol.Event) bool
	ok    bool
}

func (g *gate) emit(ev Protocol.Event) bool {
	if !g.ok {
		return false
	}
	if !g.yield(ev) {
		g.ok = false
		return false
	}
	return true
}

func (g *gate) fail(err *Protocol.Error) bool {
	if !g.ok {
		return false
	}
	g.emit(Protocol.ErrorEvent(err))
	g.ok = false
	return false
}
