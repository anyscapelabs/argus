package Chat

import (
	"context"
	"strings"

	"github.com/anyscapelabs/argus/Services/Search"
	"github.com/anyscapelabs/argus/Services/Shared"
)

// webSearch is the desktop app's web-search path. It prefers the Anyscape Gateway
// (which routes across Brave, Tavily and Exa with plan quota) using the same
// service-account key the model calls use, and falls back to the bundled
// SearXNG metasearch client when the router is unreachable, unconfigured or
// refuses the query. The two share the Answer shape, so the tool and the
// browser feature never see which backend answered.
func (s *Service) webSearch(ctx context.Context, query string, maxResults int) Search.Answer {
	if s.Core != nil && s.Core.Store != nil {
		if provider, ok := s.Core.Store.Get().Provider(Shared.RouterProviderID); ok {
			base := provider.BaseURL
			if ov := s.baseURLOverride(Shared.RouterProviderID); ov != "" {
				base = ov
			}
			if key, err := getKey(Shared.RouterProviderID); err == nil && strings.TrimSpace(key) != "" && base != "" {
				rc := &Search.RouterClient{Base: base, Key: key}
				if ans := rc.Search(ctx, query, maxResults); ans.Err == "" {
					return ans
				}
				// The router could not answer; fall through to the bundled
				// metasearch client so the tool still works on a laptop that
				// is not paired to a router.
			}
		}
	}
	return Search.Search(ctx, query, maxResults)
}
