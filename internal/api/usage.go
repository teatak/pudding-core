package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/teatak/cart/v3"
)

const maxUsageDailyDays = 370

type dailyUsageResponse struct {
	Days []dailyUsageStat `json:"days"`
}

type dailyUsageStat struct {
	Date                  string `json:"date"`
	RequestCount          int    `json:"requestCount"`
	InputUncachedTokens   int    `json:"inputUncachedTokens"`
	InputCachedTokens     int    `json:"inputCachedTokens"`
	CacheCreationTokens   int    `json:"cacheCreationTokens"`
	OutputContentTokens   int    `json:"outputContentTokens"`
	OutputReasoningTokens int    `json:"outputReasoningTokens"`
	TotalTokens           int    `json:"totalTokens"`
}

func (s *Server) getDailyUsage(c *cart.Context) error {
	days, err := usageDays(c.Request.URL.Query().Get("days"))
	if err != nil {
		return badRequest(c, "invalid days")
	}
	loc := time.Local
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	from := today.AddDate(0, 0, -(days - 1))
	to := today.AddDate(0, 0, 1)

	hours, err := s.store.UsageHourlyStats(c.Request.Context(), from, to)
	if err != nil {
		return s.fail(c, err)
	}

	byDate := make(map[string]int, days)
	out := make([]dailyUsageStat, 0, days)
	for day := from; !day.After(today); day = day.AddDate(0, 0, 1) {
		date := day.Format("2006-01-02")
		stat := dailyUsageStat{Date: date}
		out = append(out, stat)
		byDate[date] = len(out) - 1
	}
	for _, hour := range hours {
		date := hour.HourStartAt.In(loc).Format("2006-01-02")
		index, ok := byDate[date]
		if !ok {
			continue
		}
		stat := &out[index]
		stat.RequestCount += hour.RequestCount
		stat.InputUncachedTokens += hour.InputUncachedTokens
		stat.InputCachedTokens += hour.InputCachedTokens
		stat.CacheCreationTokens += hour.CacheCreationTokens
		stat.OutputContentTokens += hour.OutputContentTokens
		stat.OutputReasoningTokens += hour.OutputReasoningTokens
		stat.TotalTokens += hour.TotalTokens()
	}
	c.JSON(http.StatusOK, dailyUsageResponse{Days: out})
	return nil
}

func usageDays(raw string) (int, error) {
	if raw == "" {
		return 365, nil
	}
	days, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if days < 1 {
		return 0, strconv.ErrSyntax
	}
	if days > maxUsageDailyDays {
		days = maxUsageDailyDays
	}
	return days, nil
}
