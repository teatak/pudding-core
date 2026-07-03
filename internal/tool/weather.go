package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	weatherDefaultEndpoint = "https://wttr.in"
	weatherTimeout         = 8 * time.Second
	weatherMaxDays         = 3
	weatherDefaultLang     = "en"
	weatherCacheTTL        = 60 * time.Second
)

type weatherCacheEntry struct {
	payload   map[string]any
	expiresAt time.Time
}

type wttrResponse struct {
	CurrentCondition []wttrCurrent `json:"current_condition"`
	Weather          []wttrDay     `json:"weather"`
	NearestArea      []wttrArea    `json:"nearest_area"`
}

type wttrCurrent struct {
	TempC       string      `json:"temp_C"`
	FeelsLikeC  string      `json:"FeelsLikeC"`
	Humidity    string      `json:"humidity"`
	WindKmph    string      `json:"windspeedKmph"`
	WindDir     string      `json:"winddir16Point"`
	PrecipMM    string      `json:"precipMM"`
	ObservedAt  string      `json:"localObsDateTime"`
	WeatherDesc []wttrValue `json:"weatherDesc"`
	LangZhCN    []wttrValue `json:"lang_zh-cn"`
	LangZhTW    []wttrValue `json:"lang_zh-tw"`
	LangZh      []wttrValue `json:"lang_zh"`
}

type wttrDay struct {
	Date      string      `json:"date"`
	AvgTempC  string      `json:"avgtempC"`
	MaxTempC  string      `json:"maxtempC"`
	MinTempC  string      `json:"mintempC"`
	Hourly    []wttrHour  `json:"hourly"`
	Astronomy []wttrAstro `json:"astronomy"`
}

type wttrHour struct {
	Time         string      `json:"time"`
	ChanceOfRain string      `json:"chanceofrain"`
	WeatherDesc  []wttrValue `json:"weatherDesc"`
	LangZhCN     []wttrValue `json:"lang_zh-cn"`
	LangZhTW     []wttrValue `json:"lang_zh-tw"`
	LangZh       []wttrValue `json:"lang_zh"`
}

type wttrAstro struct {
	Sunrise string `json:"sunrise"`
	Sunset  string `json:"sunset"`
}

type wttrArea struct {
	AreaName []wttrValue `json:"areaName"`
	Country  []wttrValue `json:"country"`
	Region   []wttrValue `json:"region"`
}

type wttrValue struct {
	Value string `json:"value"`
}

func (r *BuiltinRunner) weatherGet(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeToolArgs(call.Args)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_arguments", "error": err.Error()})
	}
	location := strings.TrimSpace(stringArg(args, "location"))
	lang := strings.ToLower(strings.TrimSpace(stringArg(args, "lang")))
	if lang == "" {
		lang = weatherDefaultLang
	}
	days := clampInt(intArg(args, "days"), 0, weatherMaxDays)
	cacheLoc := strings.ToLower(location)
	if cacheLoc == "" {
		cacheLoc = "@ip"
	}
	cacheKey := lang + "|" + cacheLoc
	if cached, ok := r.weatherCacheLookup(cacheKey); ok {
		return withResultSummary(toolJSON(out, true, shrinkWeatherPayload(cached, days)), SummaryReturnedFields, len(cached))
	}
	payload, err := r.fetchWeather(ctx, location, lang)
	if err != nil {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": classifyWeatherError(err), "error": err.Error()})
	}
	r.weatherCacheStore(cacheKey, payload)
	return withResultSummary(toolJSON(out, true, shrinkWeatherPayload(payload, days)), SummaryReturnedFields, len(payload))
}

func (r *BuiltinRunner) fetchWeather(ctx context.Context, location, lang string) (map[string]any, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(r.weatherEndpoint), "/")
	if endpoint == "" {
		endpoint = weatherDefaultEndpoint
	}
	var target string
	if location == "" {
		target = endpoint + "/?format=j1&lang=" + url.QueryEscape(lang)
	} else {
		target = endpoint + "/" + url.PathEscape(location) + "?format=j1&lang=" + url.QueryEscape(lang)
	}
	reqCtx, cancel := context.WithTimeout(ctx, weatherTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "curl/8.0 pudding-weather")
	resp, err := r.webHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wttr.in returned status %d", resp.StatusCode)
	}
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "Unknown location") || !strings.HasPrefix(trimmed, "{") {
		return nil, errors.New("unknown_location")
	}
	var parsed wttrResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, err
	}
	return buildWeatherPayload(location, lang, &parsed), nil
}

func buildWeatherPayload(location, lang string, data *wttrResponse) map[string]any {
	out := map[string]any{"ok": true, "lang": lang}
	if location == "" {
		out["source"] = "ip"
	} else {
		out["source"] = "argument"
	}
	displayLocation := location
	if data != nil && len(data.NearestArea) > 0 {
		area := data.NearestArea[0]
		resolved := map[string]any{}
		if value := wttrFirstValue(area.AreaName); value != "" {
			resolved["name"] = value
			if displayLocation == "" {
				displayLocation = value
			}
		}
		if value := wttrFirstValue(area.Country); value != "" {
			resolved["country"] = value
		}
		if value := wttrFirstValue(area.Region); value != "" {
			resolved["region"] = value
		}
		if len(resolved) > 0 {
			out["resolved"] = resolved
		}
	}
	out["location"] = displayLocation
	if data != nil && len(data.CurrentCondition) > 0 {
		current := data.CurrentCondition[0]
		desc := wttrPickLocalized(lang, current.LangZhCN, current.LangZhTW, current.LangZh, current.WeatherDesc)
		out["current"] = map[string]any{
			"temp_c":       wttrFloatOrString(current.TempC),
			"feels_like_c": wttrFloatOrString(current.FeelsLikeC),
			"humidity":     wttrIntOrString(current.Humidity),
			"wind_kmph":    wttrFloatOrString(current.WindKmph),
			"wind_dir":     current.WindDir,
			"precip_mm":    wttrFloatOrString(current.PrecipMM),
			"description":  desc,
			"observed_at":  current.ObservedAt,
		}
		out["short_text"] = buildWeatherShortText(displayLocation, desc, current.TempC)
	}
	if data != nil && len(data.Weather) > 0 {
		forecast := make([]map[string]any, 0, len(data.Weather))
		for _, day := range data.Weather {
			item := map[string]any{
				"date":  day.Date,
				"max_c": wttrFloatOrString(day.MaxTempC),
				"min_c": wttrFloatOrString(day.MinTempC),
				"avg_c": wttrFloatOrString(day.AvgTempC),
			}
			if len(day.Astronomy) > 0 {
				item["sunrise"] = day.Astronomy[0].Sunrise
				item["sunset"] = day.Astronomy[0].Sunset
			}
			desc, rain := summarizeWttrHourly(lang, day.Hourly)
			if desc != "" {
				item["description"] = desc
			}
			if rain >= 0 {
				item["chance_of_rain"] = rain
			}
			forecast = append(forecast, item)
		}
		out["forecast"] = forecast
	}
	return out
}

func shrinkWeatherPayload(payload map[string]any, days int) map[string]any {
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		out[key] = value
	}
	if days <= 0 {
		delete(out, "forecast")
		return out
	}
	if days >= weatherMaxDays {
		return out
	}
	if forecast, ok := out["forecast"].([]map[string]any); ok && len(forecast) > days {
		out["forecast"] = forecast[:days]
	}
	return out
}

func summarizeWttrHourly(lang string, hours []wttrHour) (string, int) {
	if len(hours) == 0 {
		return "", -1
	}
	maxRain := -1
	noonDesc := ""
	for _, hour := range hours {
		if chance, err := strconv.Atoi(strings.TrimSpace(hour.ChanceOfRain)); err == nil && chance > maxRain {
			maxRain = chance
		}
		if strings.TrimSpace(hour.Time) == "1200" && noonDesc == "" {
			noonDesc = wttrPickLocalized(lang, hour.LangZhCN, hour.LangZhTW, hour.LangZh, hour.WeatherDesc)
		}
	}
	if noonDesc == "" {
		first := hours[0]
		noonDesc = wttrPickLocalized(lang, first.LangZhCN, first.LangZhTW, first.LangZh, first.WeatherDesc)
	}
	return noonDesc, maxRain
}

func buildWeatherShortText(location, desc, tempC string) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(location))
	if desc != "" {
		b.WriteString(": ")
		b.WriteString(strings.TrimSpace(desc))
	}
	if tempC != "" {
		b.WriteString(" ")
		b.WriteString(strings.TrimSpace(tempC))
		b.WriteString(" C")
	}
	return b.String()
}

func wttrPickLocalized(lang string, zhCN, zhTW, zh, en []wttrValue) string {
	switch lang {
	case "zh-cn":
		if value := wttrFirstValue(zhCN); value != "" {
			return value
		}
	case "zh-tw":
		if value := wttrFirstValue(zhTW); value != "" {
			return value
		}
	case "zh":
		if value := wttrFirstValue(zh); value != "" {
			return value
		}
	}
	if value := wttrFirstValue(en); value != "" {
		return value
	}
	return wttrFirstValue(zh)
}

func wttrFirstValue(values []wttrValue) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0].Value)
}

func wttrFloatOrString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := strconv.ParseFloat(value, 64); err == nil {
		return parsed
	}
	return value
}

func wttrIntOrString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := strconv.Atoi(value); err == nil {
		return parsed
	}
	return value
}

func classifyWeatherError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case msg == "unknown_location" || strings.Contains(msg, "unknown location"):
		return "unknown_location"
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "timeout"):
		return "timeout"
	case strings.Contains(msg, "no such host"):
		return "dns"
	case strings.Contains(msg, "connection refused"):
		return "connection_refused"
	default:
		return "network_error"
	}
}

func (r *BuiltinRunner) weatherCacheLookup(key string) (map[string]any, bool) {
	r.weatherMu.Lock()
	defer r.weatherMu.Unlock()
	entry, ok := r.weatherCache[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(r.weatherCache, key)
		return nil, false
	}
	return entry.payload, true
}

func (r *BuiltinRunner) weatherCacheStore(key string, payload map[string]any) {
	r.weatherMu.Lock()
	defer r.weatherMu.Unlock()
	r.weatherCache[key] = weatherCacheEntry{payload: payload, expiresAt: time.Now().Add(weatherCacheTTL)}
}
