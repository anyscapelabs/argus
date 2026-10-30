package Catalog

import (
	"strconv"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Models"
)

func ContextLabel(n int64) string {
	if n <= 0 {
		return "Context not listed"
	}
	if n >= 1000000 {
		return humanNumber(n, 1000000) + "M"
	}
	if n >= 10000 {
		return humanNumber(n, 1000) + "K"
	}
	return strconv.FormatInt(n, 10)
}

func humanNumber(n, div int64) string {
	v := float64(n) / float64(div)
	s := strconv.FormatFloat(v, 'f', 1, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	if s == "" {
		return "0"
	}
	return s
}

func priceNumber(p float64) string {
	if p < 0.1 && p > 0 {
		s := strings.TrimRight(strconv.FormatFloat(p, 'f', 3, 64), "0")
		parts := strings.SplitN(s, ".", 2)
		if len(parts) == 2 && len(parts[1]) < 2 {
			return strconv.FormatFloat(p, 'f', 2, 64)
		}
		return s
	}
	return strconv.FormatFloat(p, 'f', 2, 64)
}

func PriceLabel(c *Models.Cost) string {
	if c == nil || (c.Input == nil && c.Output == nil) {
		return "Price not listed"
	}
	var in, out float64
	if c.Input != nil {
		in = *c.Input
	}
	if c.Output != nil {
		out = *c.Output
	}
	if in == 0 && out == 0 {
		return "Free"
	}
	return "$" + priceNumber(in) + " in, $" + priceNumber(out) + " out per 1M tokens"
}

func CapabilityBadges(c *Models.Capabilities, known bool) []string {
	if !known || c == nil {
		return nil
	}
	var badges []string
	if c.Tools {
		badges = append(badges, "Tools")
	}
	if c.Vision {
		badges = append(badges, "Vision")
	}
	if c.Reasoning {
		badges = append(badges, "Reasoning")
	}
	if c.Files {
		badges = append(badges, "Files")
	}
	return badges
}

func ReleasedLabel(released string) string {
	switch len(released) {
	case 4:
		return released
	case 7:
		if t, err := time.Parse("2006-01", released); err == nil {
			return t.Format("Jan 2006")
		}
		return released
	case 10:
		return released
	default:
		return released
	}
}

func DateKey(released string) int64 {
	switch len(released) {
	case 10:
		return parseDateKey(released, "2006-01-02")
	case 7:
		return parseDateKey(released+"/01", "2006-01-02")
	case 4:
		return parseDateKey(released+"/01/01", "2006-01-02")
	default:
		return 0
	}
}

func parseDateKey(s, layout string) int64 {
	t, err := time.Parse(layout, s)
	if err != nil {
		return 0
	}
	return int64(t.Year())*10000 + int64(t.Month())*100 + int64(t.Day())
}
