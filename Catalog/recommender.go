package Catalog

import (
	"strings"

	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Provider"
)

const cheapMinContext = 32000

func EligibleMain(m Models.Model) (bool, string) {
	if !Models.ProducesText(m) {
		return false, "This model doesn't produce text"
	}
	if !m.CapsKnown || m.Capabilities == nil {
		return false, "Capabilities unknown. Add it as a custom model to confirm what it supports"
	}
	if !m.Capabilities.Tools {
		return false, "This model can't call tools, which the agent needs to act"
	}
	return true, ""
}

func EligibleCheap(m Models.Model) (bool, string) {
	if !Models.ProducesText(m) {
		return false, "This model doesn't produce text"
	}
	return true, ""
}

func RecommendFrom(models []Models.Model, rec Provider.Recommended) (Models.Model, Models.Model) {
	var main, cheap Models.Model
	var mainCandidates []Models.Model
	for _, m := range models {
		if m.Status == Models.StatusStable && Models.ProducesText(m) {
			mainCandidates = append(mainCandidates, m)
		}
	}
	if len(mainCandidates) == 0 {
		return Models.Model{}, Models.Model{}
	}

	for _, id := range rec.Main {
		if m, ok := catalogRef(models, id); ok && m.Status == Models.StatusStable && Models.ProducesText(m) {
			if el, _ := EligibleMain(m); el {
				main = m
				break
			}
		}
	}
	for _, id := range rec.Cheap {
		if m, ok := catalogRef(models, id); ok && m.Status == Models.StatusStable && Models.ProducesText(m) {
			if el, _ := EligibleCheap(m); el {
				cheap = m
				break
			}
		}
	}

	if main.ID == "" {
		for _, m := range mainCandidates {
			if el, _ := EligibleMain(m); el {
				main = m
				break
			}
		}
		if main.ID == "" {
			main = mainCandidates[0]
		}
	}

	if cheap.ID == "" {
		cheap = cheapHeuristic(mainCandidates, main)
	}
	return main, cheap
}

func catalogRef(models []Models.Model, id string) (Models.Model, bool) {
	for _, m := range models {
		if strings.EqualFold(m.ID, id) {
			return m, true
		}
	}
	return Models.Model{}, false
}

func cheapHeuristic(candidates []Models.Model, newest Models.Model) Models.Model {
	newestKey := DateKey(newest.Released)
	var pool []Models.Model
	for _, m := range candidates {
		if m.Limits == nil || m.Limits.Context == 0 || m.Limits.Context >= cheapMinContext {
			if releasedInWindow(DateKey(m.Released), newestKey, 24) {
				pool = append(pool, m)
			}
		}
	}

	var best Models.Model
	var bestPrice float64
	for _, m := range pool {
		if m.Cost == nil || m.Cost.Input == nil || m.Cost.Output == nil {
			continue
		}
		blended := 3**m.Cost.Input + 1**m.Cost.Output
		switch {
		case best.ID == "":
			best, bestPrice = m, blended
		case blended < bestPrice:
			best, bestPrice = m, blended
		case blended == bestPrice && DateKey(m.Released) > DateKey(best.Released):
			best = m
		}
	}
	if best.ID != "" {
		return best
	}
	for _, m := range pool {
		if best.ID == "" || DateKey(m.Released) > DateKey(best.Released) {
			best = m
		}
	}
	if best.ID == "" {
		return newest
	}
	return best
}

func releasedInWindow(key, newestKey int64, months int) bool {
	if key == 0 || newestKey == 0 {
		return true
	}
	miny := int(newestKey / 10000)
	minm := int((newestKey%10000)/100) - months
	for minm <= 0 {
		miny--
		minm += 12
	}
	limit := int64(miny)*10000 + int64(minm)*100 + 1
	return key >= limit
}
