// Package Location answers, per user, where they are and what time it is,
// so the agent can date its answers, make searches place-aware and schedule
// against the user's real clock.
//
// It is internal to the app: there is no settings page for it. Time comes
// from the OS timezone (device-local, no network). The city and coordinates
// are resolved over free, keyless IP geolocation, cached so the network is
// touched a few times a day at most. A manual location stored in app_settings
// always wins and needs no network — the agent's get_time_and_location tool
// and the system prompt block both read through this same resolver.
package Location

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anyscapelabs/argus/Storage"
)

const (
	cacheTTL       = 6 * time.Hour
	requestTimeout = 8 * time.Second
)

// Settings keys in app_settings — the manual override, readable even though
// no UI writes it today.
const (
	keyCity    = "location.city"
	keyRegion  = "location.region"
	keyCountry = "location.country"
	keyTZ      = "location.timezone"
)

// Info is the resolved time-and-place picture the agent works from. Geo
// fields stay empty when IP resolution has no data.
type Info struct {
	Timezone  string  `json:"timezone"`
	OffsetMin int     `json:"offset_min"`
	City      string  `json:"city,omitempty"`
	Region    string  `json:"region,omitempty"`
	Country   string  `json:"country,omitempty"`
	Lat       float64 `json:"lat,omitempty"`
	Lon       float64 `json:"lon,omitempty"`
	Source    string  `json:"source"` // manual | ip | os
}

type ipGeo struct {
	City, Region, Country, TZ string
	Lat, Lon                  float64
}

var defaultClient = NewClient()

// Current resolves the picture for this user right now: manual override (if
// stored), else cached IP geo, else the OS timezone alone.
func Current(ctx context.Context, db *Storage.DB) Info {
	return defaultClient.Current(ctx, db)
}

// FlushCache drops the cached IP geo so the next Current re-resolves.
func FlushCache() { defaultClient.flush() }

// Client resolves location with a small cache. Safe for concurrent use.
type Client struct {
	mu       sync.Mutex
	next     int
	ip       []*ipInstance
	cached   *ipGeo
	cachedAt time.Time
}

func NewClient() *Client {
	return &Client{
		ip: []*ipInstance{
			{base: "https://ipwho.is"},
			{base: "https://ipapi.co"},
			{base: "https://ipinfo.io"},
		},
	}
}

type ipInstance struct {
	mu           sync.Mutex
	base         string
	blockedUntil time.Time
	failures     int
}

// Current builds the snapshot: manual data first, then IP geo, and always a
// timezone (manual > IP-reported > OS) with the live local time.
func (c *Client) Current(ctx context.Context, db *Storage.DB) Info {
	city := strings.TrimSpace(setting(db, keyCity))
	country := strings.TrimSpace(setting(db, keyCountry))
	tz := strings.TrimSpace(setting(db, keyTZ))

	info := Info{Timezone: tz}
	if city != "" {
		info.City = city
		info.Region = strings.TrimSpace(setting(db, keyRegion))
		info.Country = country
		info.Source = "manual"
	} else if g := c.cachedGeo(ctx, db); g != nil {
		info.City, info.Region, info.Country = g.City, g.Region, g.Country
		info.Lat, info.Lon = g.Lat, g.Lon
		info.Source = "ip"
		if info.Timezone == "" {
			info.Timezone = g.TZ
		}
	}

	loc, err := time.LoadLocation(strings.TrimSpace(info.Timezone))
	if err != nil || loc == nil {
		loc = time.Local
	}
	info.Timezone = loc.String()
	_, off := time.Now().In(loc).Zone()
	info.OffsetMin = off / 60
	if info.Source == "" {
		info.Source = "os"
	}
	return info
}

func (c *Client) flush() {
	c.mu.Lock()
	c.cached = nil
	c.mu.Unlock()
}

func (c *Client) cachedGeo(ctx context.Context, _ *Storage.DB) *ipGeo {
	c.mu.Lock()
	if c.cached != nil && time.Since(c.cachedAt) < cacheTTL {
		out := c.cached
		c.mu.Unlock()
		return out
	}
	c.mu.Unlock()
	// Resolve outside the lock so simultaneous runs never stack requests;
	// the last completion wins the cache slot.
	geo, ok := c.resolve(ctx)
	c.mu.Lock()
	if ok || c.cached == nil {
		c.cached = geo
		c.cachedAt = time.Now()
	}
	out := c.cached
	c.mu.Unlock()
	return out
}

func (c *Client) resolve(ctx context.Context) (*ipGeo, bool) {
	order := c.rotate()
	if order == nil {
		return nil, false
	}
	for _, inst := range order {
		inst.mu.Lock()
		blocked := time.Now().Before(inst.blockedUntil)
		inst.mu.Unlock()
		if blocked {
			continue
		}
		geo, err := inst.query(ctx)
		inst.mu.Lock()
		if err != nil {
			inst.failures++
			inst.blockedUntil = time.Now().Add(min(10*time.Minute, 30*time.Second<<inst.failures))
		} else {
			inst.failures = 0
		}
		inst.mu.Unlock()
		if err != nil {
			continue
		}
		return geo, true
	}
	return nil, false
}

func (c *Client) rotate() []*ipInstance {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(c.ip)
	if n == 0 {
		return nil
	}
	c.next = (c.next + 1) % n
	out := make([]*ipInstance, 0, n)
	out = append(out, c.ip[c.next:]...)
	out = append(out, c.ip[:c.next]...)
	return out
}

func (inst *ipInstance) query(ctx context.Context) (*ipGeo, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var url string
	switch inst.base {
	case "https://ipapi.co":
		url = "https://ipapi.co/json/"
	case "https://ipinfo.io":
		url = "https://ipinfo.io/json"
	default:
		url = "https://ipwho.is/"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if err != nil {
		return nil, err
	}
	return parseGeo(body, inst.base)
}

type ipwhoShape struct {
	Success  bool   `json:"success"`
	City     string `json:"city"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	Timezone struct {
		ID string `json:"id"`
	} `json:"timezone"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type ipapiShape struct {
	City        string  `json:"city"`
	Region      string  `json:"region"`
	CountryName string  `json:"country_name"`
	Timezone    string  `json:"timezone"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
}

type ipinfoShape struct {
	City     string `json:"city"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	Loc      string `json:"loc"`
	Timezone string `json:"timezone"`
}

func parseGeo(body []byte, source string) (*ipGeo, error) {
	var out ipGeo
	switch source {
	case "https://ipwho.is":
		var w ipwhoShape
		if err := json.Unmarshal(body, &w); err != nil || !w.Success {
			return nil, fmt.Errorf("bad ipwho response")
		}
		out.City, out.Region, out.Country, out.TZ = w.City, w.Region, w.Country, w.Timezone.ID
		out.Lat, out.Lon = w.Latitude, w.Longitude
	case "https://ipapi.co":
		var a ipapiShape
		if err := json.Unmarshal(body, &a); err != nil || a.City == "" {
			return nil, fmt.Errorf("bad ipapi response")
		}
		out.City, out.Region, out.Country, out.TZ = a.City, a.Region, a.CountryName, a.Timezone
		out.Lat, out.Lon = a.Latitude, a.Longitude
	default:
		var f ipinfoShape
		if err := json.Unmarshal(body, &f); err != nil || f.City == "" {
			return nil, fmt.Errorf("bad ipinfo response")
		}
		out.City, out.Region, out.Country, out.TZ = f.City, f.Region, f.Country, f.Timezone
		if lat, lon, ok := strings.Cut(f.Loc, ","); ok {
			fmt.Sscanf(lat, "%f", &out.Lat)
			fmt.Sscanf(lon, "%f", &out.Lon)
		}
	}
	if out.City == "" {
		return nil, fmt.Errorf("no city in response")
	}
	return &out, nil
}

// Prompt renders the block the agent receives, or "" when there is nothing to
// say. The timezone alone is always worth one line: dates gate everything.
func (i Info) Prompt() string {
	var b strings.Builder
	b.WriteString("\n## Time & place\n")
	if i.Timezone != "" {
		local := time.Now()
		if loc, err := time.LoadLocation(i.Timezone); err == nil {
			local = time.Now().In(loc)
		}
		fmt.Fprintf(&b, "- The user's local time is %s (%s) — use it for \"now\", \"today\", dates, and deadlines.\n",
			local.Format("Monday 15:04"), i.Timezone)
	}
	if i.City != "" {
		across := " (approximate, from IP)"
		if i.Source == "manual" {
			across = ""
		}
		region := i.Region
		if region != "" {
			region = ", " + region
		}
		fmt.Fprintf(&b, "- The user is in %s%s, %s%s — make searches place-aware when the question is local (news, weather, \"near me\").\n",
			i.City, region, i.Country, across)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ToolResult renders the snapshot for the get_time_and_location tool: an
// exact, machine-readable time block the agent can schedule against, plus
// the place line (or an honest note that only the timezone is trusted).
func (i Info) ToolResult() string {
	loc := time.Local
	offsetLabel := ""
	if l, err := time.LoadLocation(i.Timezone); err == nil {
		loc = l
	}
	now := time.Now().In(loc)
	_, off := now.Zone()
	switch {
	case off == 0:
		offsetLabel = "UTC"
	default:
		sign, abs := "+", off
		if off < 0 {
			sign, abs = "-", -off
		}
		if m := abs % 60; m != 0 {
			offsetLabel = fmt.Sprintf("UTC%s%d:%02d", sign, abs/60, m)
		} else {
			offsetLabel = fmt.Sprintf("UTC%s%d", sign, abs/60)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Local time: %s (%s, %s)\n", now.Format("Monday 2006-01-02 15:04:05"), i.Timezone, offsetLabel)
	fmt.Fprintf(&b, "UTC time: %s\n", time.Now().UTC().Format(time.RFC3339))
	if i.City != "" {
		place := i.City
		if i.Region != "" {
			place += ", " + i.Region
		}
		if i.Country != "" {
			place += ", " + i.Country
		}
		b.WriteString("Location: " + place)
		if i.Lat != 0 || i.Lon != 0 {
			fmt.Fprintf(&b, " (lat %.4f, lon %.4f)", i.Lat, i.Lon)
		}
		if i.Source == "ip" {
			b.WriteString(" — approximate, from IP")
		}
		b.WriteString("\n")
	} else {
		b.WriteString("Location: not resolved (no network or no data); only the timezone is reliable.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func setting(db *Storage.DB, key string) string {
	v, _, err := db.GetAppSetting(key)
	if err != nil {
		return ""
	}
	return v
}
