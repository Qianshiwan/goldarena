package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/goldarena/goldarena/internal/common"
	"github.com/spf13/viper"
)

// klineMaxBars caps how many candles we keep per period (in-memory + REST).
// 1500 1m candles ≈ 25h — enough to show a full XAU/USD (London gold spot) trading session.
const klineMaxBars = 1500

// twelveDataTimeSeries mirrors Twelve Data /time_series response (1m bars).
type twelveDataTimeSeries struct {
	Meta struct {
		Symbol   string `json:"symbol"`
		Interval string `json:"interval"`
	} `json:"meta"`
	Values []struct {
		Datetime string `json:"datetime"`
		Open     string `json:"open"`
		High     string `json:"high"`
		Low      string `json:"low"`
		Close    string `json:"close"`
	} `json:"values"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// seedHistory ensures every supported period has real K-line history.
//
// Strategy (resilient + quota-friendly):
//   - If a period already has enough bars cached locally (from kline_history.json,
//     restored at startup), we TRUST it and skip the network entirely. This keeps
//     restarts cheap, never destroys good data, and avoids hammering the API.
//   - Only periods that are thin/empty are fetched from Twelve Data /time_series
//     (with retry). Daily (1d) in particular cannot be derived from the ~25h of
//     1m we retain, so it is fetched from the network whenever it is thin.
//   - Any period whose fetch fails falls back to deriving from the in-memory 1m
//     set, so the chart always shows multiple timeframes even fully offline.
//
// Source: Twelve Data time_series (XAU/USD). Activated when an API key is
// configured (market.twelvedata.api_key or TWELVEDATA_API_KEY). Without a key it
// derives intraday from 1m only.
func (s *MarketService) seedHistory(ctx context.Context) {
	apiKey := getTwelveDataAPIKey()
	backfill := viper.GetInt("market.history.backfill_minutes")
	if backfill <= 0 {
		backfill = 1440 // 24h of 1m bars
	}

	// Minimum cached bars per period before we trust local data and skip the
	// network. Prevents redundant API calls and never destroys good history.
	minBars := map[string]int{
		"1m": 200, "5m": 100, "15m": 60, "30m": 40, "1h": 100, "4h": 60, "1d": 30,
	}
	specs := []struct {
		period     string
		outputsize int
	}{
		{"1m", backfill}, // 1m honors the configured backfill window
		{"5m", 1440},     // ~5 trading days
		{"15m", 960},     // ~10 trading days
		{"30m", 720},     // ~15 trading days
		{"1h", 720},      // ~30 trading days (≈1 month)
		{"4h", 180},      // ~30 trading days
		{"1d", 180},      // ~6 months of daily candles
	}

	if apiKey == "" || apiKey == "demo" || !viper.GetBool("market.history.enabled") {
		log.Printf("[seed] Twelve Data backfill disabled/unconfigured — deriving intraday from 1m only")
		s.deriveIntradayFrom1m()
		return
	}

	totalBackfilled := 0
	for _, spec := range specs {
		key := fmt.Sprintf("XAU:SPOT:%s", spec.period)
		s.klineHistoryMu.Lock()
		cached := len(s.klineHistory[key])
		s.klineHistoryMu.Unlock()
		if cached >= minBars[spec.period] {
			log.Printf("[seed] %s already has %d bars (cached) — skip network fetch", spec.period, cached)
			continue
		}
		bars, err := s.fetchHistoryFromTwelveData(ctx, apiKey, spec.period, spec.outputsize)
		if err != nil {
			log.Printf("[seed] %s backfill failed: %v", spec.period, err)
			// Fallback: derive from the 1m set already in memory (best effort).
			if spec.period != "1m" {
				s.deriveAndMerge(spec.period)
			}
			continue
		}
		if len(bars) == 0 {
			continue
		}
		// Keep only completed bars strictly before the current period boundary so
		// the live forming candle is never overwritten or duplicated.
		nowStart := periodStart(time.Now(), spec.period).UnixMilli()
		completed := make([]common.KLine, 0, len(bars))
		for _, k := range bars {
			if k.Timestamp < nowStart {
				completed = append(completed, k)
			}
		}
		if len(completed) == 0 {
			continue
		}
		s.klineHistoryMu.Lock()
		s.klineHistory[key] = mergeKLines(s.klineHistory[key], completed)
		s.klineHistoryMu.Unlock()
		totalBackfilled += len(completed)
		// Respect Twelve Data free-tier rate limit (~8 req/min): small gap between calls.
		time.Sleep(800 * time.Millisecond)
	}

	s.persistKLineHistory()
	log.Printf("[seed] backfilled K-lines, %d new bars added (cached periods skipped)", totalBackfilled)
}

// deriveIntradayFrom1m derives 5m/15m/30m/1h/4h from the in-memory 1m set when
// no network backfill is available, so the chart still shows multiple timeframes.
func (s *MarketService) deriveIntradayFrom1m() {
	for _, p := range []string{"5m", "15m", "30m", "1h", "4h"} {
		s.deriveAndMerge(p)
	}
}

// deriveAndMerge derives `period` candles from the in-memory 1m set and merges
// them into history. Used as a fallback when a direct per-period fetch fails.
func (s *MarketService) deriveAndMerge(period string) {
	if period == "1m" {
		return
	}
	s.klineHistoryMu.Lock()
	defer s.klineHistoryMu.Unlock()
	merged := s.klineHistory["XAU:SPOT:1m"]
	derived := aggregate1mToPeriod(merged, period)
	key := fmt.Sprintf("XAU:SPOT:%s", period)
	s.klineHistory[key] = mergeKLines(s.klineHistory[key], derived)
	log.Printf("[seed] derived %s from 1m fallback (%d bars)", period, len(derived))
}

// twelveDataInterval maps a platform period code (1m/5m/.../1d) to Twelve Data's
// interval vocabulary (1min/5min/.../1day). Used only for the API request; stored
// candles keep the platform period so live tick alignment stays correct.
func twelveDataInterval(period string) string {
	switch period {
	case "1m":
		return "1min"
	case "5m":
		return "5min"
	case "15m":
		return "15min"
	case "30m":
		return "30min"
	case "1h":
		return "1h"
	case "4h":
		return "4h"
	case "1d":
		return "1day"
	case "1w":
		return "1week"
	case "1mo":
		return "1month"
	default:
		return period
	}
}

// fetchHistoryFromTwelveData pulls the last `outputsize` XAU/USD candles for the
// given `interval` from Twelve Data and returns them oldest-first, aligned to
// the period boundary. It retries up to 3 times (Twelve Data can intermittently
// rate-limit or return transient TLS errors).
func (s *MarketService) fetchHistoryFromTwelveData(ctx context.Context, apiKey, interval string, outputsize int) ([]common.KLine, error) {
	baseURL := viper.GetString("market.twelvedata.base_url")
	if baseURL == "" {
		baseURL = "https://api.twelvedata.com"
	}
	if outputsize > 5000 {
		outputsize = 5000
	}
	// Twelve Data uses its own interval vocabulary (1min/5min/.../1day), while
	// our platform uses 1m/5m/.../1d. Map here for the request only; the stored
	// KLine keeps the platform period so live ticks align correctly.
	tdInterval := twelveDataInterval(interval)
	q := url.Values{}
	q.Set("symbol", "XAU/USD")
	q.Set("interval", tdInterval)
	q.Set("outputsize", strconv.Itoa(outputsize))
	q.Set("timezone", "UTC")
	q.Set("apikey", apiKey)
	reqURL := fmt.Sprintf("%s/time_series?%s", baseURL, q.Encode())

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(2 * time.Second)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			lastErr = fmt.Errorf("build request: %w", err)
			continue
		}
		resp, err := s.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("http (attempt %d): %w", attempt+1, err)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read (attempt %d): %w", attempt+1, err)
			continue
		}
		var ts twelveDataTimeSeries
		if err := json.Unmarshal(body, &ts); err != nil {
			lastErr = fmt.Errorf("parse (attempt %d): %w", attempt+1, err)
			continue
		}
		if ts.Status != "ok" || len(ts.Values) == 0 {
			lastErr = fmt.Errorf("empty/invalid response (status=%s msg=%s)", ts.Status, ts.Message)
			continue
		}

		// values are newest-first; convert to oldest-first KLine slice.
		out := make([]common.KLine, 0, len(ts.Values))
		for i := len(ts.Values) - 1; i >= 0; i-- {
			v := ts.Values[i]
			t, err := time.ParseInLocation("2006-01-02 15:04:05", v.Datetime, time.UTC)
			if err != nil {
				continue
			}
			o, _ := strconv.ParseFloat(v.Open, 64)
			h, _ := strconv.ParseFloat(v.High, 64)
			l, _ := strconv.ParseFloat(v.Low, 64)
			c, _ := strconv.ParseFloat(v.Close, 64)
			if o == 0 || c == 0 {
				continue
			}
			out = append(out, common.KLine{
				Symbol:        "XAU",
				ContractMonth: "SPOT",
				Period:        interval,
				Open:          o,
				High:          h,
				Low:           l,
				Close:         c,
				Volume:        1,
				// Align to the period boundary so seeded bars line up exactly with the
				// live-accumulated candles built by updateCandles().
				Timestamp: periodStart(t, interval).UnixMilli(),
			})
		}
		return out, nil
	}
	return nil, lastErr
}

// mergeKLines dedups by timestamp, sorts ascending, and trims to klineMaxBars.
func mergeKLines(existing, incoming []common.KLine) []common.KLine {
	seen := map[int64]common.KLine{}
	order := []int64{}
	add := func(k common.KLine) {
		if _, ok := seen[k.Timestamp]; !ok {
			order = append(order, k.Timestamp)
		}
		seen[k.Timestamp] = k
	}
	for _, k := range existing {
		add(k)
	}
	for _, k := range incoming {
		add(k)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	out := make([]common.KLine, 0, len(order))
	for _, ts := range order {
		out = append(out, seen[ts])
	}
	if len(out) > klineMaxBars {
		out = out[len(out)-klineMaxBars:]
	}
	return out
}

// aggregate1mToPeriod buckets completed 1m bars into the target period.
// For "1m" it returns the input unchanged.
func aggregate1mToPeriod(oneMin []common.KLine, period string) []common.KLine {
	if period == "1m" {
		return oneMin
	}
	if _, ok := periodDurations[period]; !ok {
		return nil
	}
	buckets := map[int64]*common.KLine{}
	order := []int64{}
	for _, k := range oneMin {
		ps := periodStart(time.UnixMilli(k.Timestamp), period).UnixMilli()
		b, ok := buckets[ps]
		if !ok {
			buckets[ps] = &common.KLine{
				Symbol:        k.Symbol,
				ContractMonth: k.ContractMonth,
				Period:        period,
				Open:          k.Open,
				High:          k.High,
				Low:           k.Low,
				Close:         k.Close,
				Volume:        k.Volume,
				Timestamp:     ps,
			}
			order = append(order, ps)
			continue
		}
		if k.High > b.High {
			b.High = k.High
		}
		if k.Low < b.Low {
			b.Low = b.Low
		}
		b.Close = k.Close
		b.Volume += k.Volume
	}
	out := make([]common.KLine, 0, len(order))
	for _, ps := range order {
		out = append(out, *buckets[ps])
	}
	return out
}

// historyRollover re-seeds history when the UTC calendar day changes, so a
// long-running server always shows a fresh session without a restart.
func (s *MarketService) historyRollover(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	lastDay := time.Now().UTC().Day()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Now().UTC().Day() != lastDay {
				lastDay = time.Now().UTC().Day()
				log.Printf("[seed] new UTC day detected — re-backfilling history")
				s.seedHistory(ctx)
			}
		}
	}
}
