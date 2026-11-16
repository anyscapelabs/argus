package Catalog

import (
	"context"
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/anyscapelabs/argus/Catalog"
	"github.com/anyscapelabs/argus/Services/Shared"
)

type Service struct {
	Core *Shared.Core
}

func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	return s.Core.Bootstrap(ctx)
}

func (s *Service) CatalogStatus() Shared.StatusView {
	if err := s.Core.Ready(); err != nil {
		return Shared.StatusView{Source: "none"}
	}
	st := s.Core.Store.Status()
	return statusView(st)
}

func (s *Service) RefreshCatalog() Shared.RefreshResult {
	if err := s.Core.Ready(); err != nil {
		return Shared.RefreshResult{Error: err}
	}
	if err := s.Core.Store.Refresh(context.Background()); err != nil {
		return Shared.RefreshResult{Error: Shared.NewError(Shared.CodeBadResponse, fmt.Sprintf("The catalog refresh failed: %v", err))}
	}
	st := s.Core.Store.Status()
	view := statusView(st)
	return Shared.RefreshResult{Ok: true, Status: &view}
}

func statusView(st Catalog.Status) Shared.StatusView {
	return Shared.StatusView{
		Source:        st.Source,
		FetchedAt:     st.FetchedAt,
		LastAttempt:   st.LastAttempt,
		LastError:     st.LastError,
		ProviderCount: st.ProviderCount,
		ModelCount:    st.ModelCount,
		Stale:         st.Stale,
		Placeholder:   st.Placeholder,
	}
}
