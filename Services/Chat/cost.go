package Chat

import (
	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Protocol"
)

func (s *Service) computeCost(usage Protocol.Usage, modelRef string) *float64 {
	// try catalog
	cat := s.Core.Store.Get()
	if m, ok := cat.Get(modelRef); ok && m.Cost != nil {
		return computeCostFromPrice(usage, m.Cost)
	}
	// custom models
	rows, err := s.Core.DB.ListAllCustomModels()
	if err == nil {
		for _, cm := range rows {
			if cm.Ref() == modelRef {
				// build cost
				cost := &Models.Cost{}
				if cm.PriceKnown {
					cost.Input = &cm.PriceInput
					cost.Output = &cm.PriceOutput
				}
				return computeCostFromPrice(usage, cost)
			}
		}
	}
	return nil
}

func computeCostFromPrice(usage Protocol.Usage, cost *Models.Cost) *float64 {
	if cost == nil {
		return nil
	}
	total := 0.0
	if usage.InputTokens > 0 {
		if cost.Input == nil {
			return nil
		}
		total += float64(usage.InputTokens) / 1e6 * *cost.Input
	}
	if usage.CacheReadTokens > 0 {
		price := cost.CacheRead
		if price == nil {
			price = cost.Input
		}
		if price == nil {
			return nil
		}
		total += float64(usage.CacheReadTokens) / 1e6 * *price
	}
	if usage.CacheWriteTokens > 0 {
		price := cost.CacheWrite
		if price == nil {
			price = cost.Input
		}
		if price == nil {
			return nil
		}
		total += float64(usage.CacheWriteTokens) / 1e6 * *price
	}
	if usage.OutputTokens > 0 {
		if cost.Output == nil {
			return nil
		}
		total += float64(usage.OutputTokens) / 1e6 * *cost.Output
	}
	if usage.ReasoningTokens > 0 {
		price := cost.Reasoning
		if price == nil {
			price = cost.Output
		}
		if price == nil {
			return nil
		}
		total += float64(usage.ReasoningTokens) / 1e6 * *price
	}
	return &total
}
