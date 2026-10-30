package Models

import (
	"sort"
	"strings"

	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Services/Shared"
)

func searchActive(options Shared.PickerOptions) bool {
	return strings.TrimSpace(options.Search) != ""
}

func applySearch(views []Shared.ModelView, search string) {
	terms := searchTerms(search)
	if len(terms) == 0 {
		return
	}
	for i := range views {
		if !matchesSearch(views[i], terms) {
			views[i] = Shared.ModelView{}
		}
	}
}

func searchTerms(search string) []string {
	return strings.Fields(strings.ToLower(search))
}

func matchesSearch(view Shared.ModelView, terms []string) bool {
	hay := strings.ToLower(strings.Join([]string{view.Name, view.ID, view.Family, view.Ref}, " "))
	for _, term := range terms {
		if !strings.Contains(hay, term) {
			return false
		}
	}
	return true
}

func applyToolsOnly(views []Shared.ModelView, toolsOnly bool) {
	if !toolsOnly {
		return
	}
	for i := range views {
		if !views[i].CapsKnown || !views[i].Caps.Tools {
			views[i] = Shared.ModelView{}
		}
	}
}

func applyBetaHandicap(views []Shared.ModelView, includeBeta bool) {
	if includeBeta {
		return
	}
	for i := range views {
		if views[i].Status == string(Models.StatusBeta) || views[i].Status == string(Models.StatusAlpha) {
			views[i] = Shared.ModelView{}
		}
	}
}

func applyRecommended(views []Shared.ModelView, mainRef, cheapRef string) {
	for i := range views {
		if strings.EqualFold(views[i].Ref, mainRef) {
			views[i].Recommended = Shared.SlotMain
		}
		if strings.EqualFold(views[i].Ref, cheapRef) {
			views[i].Recommended = Shared.SlotCheap
		}
	}
}

func pickAvailability(views []Shared.ModelView, availabilities ...string) []Shared.ModelView {
	var out []Shared.ModelView
	for _, v := range views {
		for _, a := range availabilities {
			if v.Availability == a {
				out = append(out, v)
				break
			}
		}
	}
	return out
}

func mergeViews(groups ...[]Shared.ModelView) []Shared.ModelView {
	out := make([]Shared.ModelView, 0, 64)
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

func viewRef(views []Shared.ModelView, ref string) (Shared.ModelView, bool) {
	if ref == "" {
		return Shared.ModelView{}, false
	}
	needle := strings.ToLower(ref)
	for _, v := range views {
		if strings.ToLower(v.Ref) == needle {
			return v, true
		}
	}
	return Shared.ModelView{}, false
}

func viewRefSet(views []Shared.ModelView) map[string]bool {
	out := map[string]bool{}
	for _, v := range views {
		if v.Ref != "" {
			out[strings.ToLower(v.Ref)] = true
		}
	}
	return out
}

func sortByName(views []Shared.ModelView) {
	sort.SliceStable(views, func(i, j int) bool { return views[i].Name < views[j].Name })
}
