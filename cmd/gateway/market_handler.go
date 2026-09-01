package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/goldarena/goldarena/internal/common"
	"github.com/goldarena/goldarena/pkg/errs"
	"github.com/goldarena/goldarena/pkg/redis"
	ws "github.com/goldarena/goldarena/pkg/websocket"
	"github.com/spf13/viper"
)

// quoteCacheDuration: how long a real quote stays valid before re-fetching.
// Must be < pollQuotes interval (1s) so every poll actually hits Sina and the
// forming candle ticks at the full poll rate instead of half rate.
const quoteCacheDuration = 800 * time.Millisecond

// goldApiStaleThreshold: if the live source's reported update time is older
// than this, we treat the quote as "no data" (market closed / feed dead) and
// freeze the feed instead of showing a stale price as if it were live.
// 10 minutes keeps the feed live through brief flat (non-moving) periods while
// still freezing on genuine closures (weekend ~2d stale, daily break ~1h).
const goldApiStaleThreshold = 10 * time.Minute

// longGapResetThreshold: after a long gap with no accepted price (e.g. the
// market was closed over a weekend), the first post-reopen price may
// legitimately jump; don't reject it as a bad tick.
const longGapResetThreshold = 10 * time.Minute

// badTickMaxDeviation: a single quote moving more than this fraction from the
// last accepted price is rejected as a bad tick (prevents phantom candle wicks).
// XAU essentially never moves >3% within one 2s poll; real glitches do.
const badTickMaxDeviation = 0.03

// twelveDataCooldown throttles Twelve Data probes. The free tier allows only
// ~800 credits/day, so we probe at most once per minute and let the synthetic
// ticks cover the gaps between probes — otherwise 1s polling burns the daily
// quota in ~13 minutes and the feed dies for the rest of the day.
const twelveDataCooldown = 60 * time.Second

// marketPushTokenDefault is the shared secret for the local MT4 / quote-bridge
// push endpoint (POST /api/v1/market/push). Override via market.push.token in
// config or the MARKET_PUSH_TOKEN env var. Keep it out of public repos.
const marketPushTokenDefault = "GAmt4Push_8Kx2qL9vRtZ"

// externalQuoteMaxAge: an externally pushed quote older than this is treated as
// stale (the local bridge is offline) and we fall back to the autonomous cloud
// sources (gold-api.io → Sina → Twelve Data). Set generously: MT4 ticks arrive
// far more often than this, so 10s only trips when the bridge truly dies.
const externalQuoteMaxAge = 10 * time.Second

type MarketService struct {
	rdb          *redis.Redis
	hub          *ws.Hub
	symbols      []string // e.g. ["XAU"]
	basePrice    float64  // fallback simulated base price
	mu           sync.RWMutex
	cachedKLines sync.Map // in-memory K-line cache
	tradeSvc     *TradeService // back-ref for order matching on ticks

	// Real API quote cache (per-symbol)
	cachedQuote   map[string]*common.Quote
	cachedQuoteAt map[string]time.Time
	quoteCacheMu  sync.Mutex

	// lastValidPrice: most recent price accepted as sane, used to reject
	// bad ticks (transient exchange glitches / fallback to fake prices).
	lastValidPrice map[string]float64
	// lastValidIsReal marks whether lastValidPrice came from a real source.
	// When false (simulated fallback), the next real quote re-anchors
	// unconditionally instead of being rejected by the bad-tick filter.
	lastValidIsReal map[string]bool
	// lastValidAt: when the last accepted price was recorded. Used to relax
	// the bad-tick filter after a long gap (market reopen after closure).
	lastValidAt     map[string]time.Time
	lastValidMu     sync.Mutex

	// twelveDataTriedAt throttles Twelve Data probes (free-tier credit limit).
	twelveCooldownMu   sync.Mutex
	twelveDataTriedAt  map[string]time.Time

	// externalQuote: latest quote pushed in from an external source (MT4/local
	// bridge) per symbol. Highest-priority real source when fresh.
	externalQuote   map[string]*externalQuoteData
	externalQuoteMu sync.Mutex

	// Price tick history for real K-line generation
	priceHistory   []priceTick
	priceHistoryMu sync.Mutex

	// K-line history per period (rolling window, in-memory)
	klineHistory   map[string][]common.KLine // key: "XAU:SPOT:1m" etc.
	klineHistoryMu sync.Mutex

	// Current forming candle per period
	currentCandle   map[string]*common.KLine
	currentCandleMu sync.Mutex

	// Local persistence of accumulated K-line history (survives restarts)
	historyFilePath string
	historyDirty    bool

	// High-frequency synthetic tick settings (0.1s live-feel updates).
	// Real quotes only arrive ~1s from Sina; between real polls we synthesize
	// tiny mean-reverting micro-moves so the chart/quote bar feel alive at 10fps
	// while staying anchored to the last *real* price.
	tickInterval    time.Duration // synthetic broadcast cadence (default 100ms)
	simulateTicks  bool          // legacy 0.1s synthetic tick flag — disabled (always false)
	tickVolatility float64       // max per-tick step as fraction of price

	httpClient *http.Client
}

// priceTick stores a single real price observation
type priceTick struct {
	Price     float64
	Timestamp time.Time
}

// externalQuoteData holds a quote pushed in from an external source (e.g. the
// user's MT4 broker feed via the local push bridge) together with the time it
// was received, so we can tell a live bridge from a dead one.
type externalQuoteData struct {
	Quote *common.Quote
	At    time.Time
}

func NewMarketService(rdb *redis.Redis, hub *ws.Hub, basePrice float64) *MarketService {
	if basePrice <= 0 {
		basePrice = 4139.0
	}
	log.Printf("MarketService: basePrice = %.2f", basePrice)
	log.Printf("MarketService: symbols = [XAU]")
	ms := &MarketService{
		rdb:             rdb,
		hub:             hub,
		symbols:         []string{"XAU"},
		basePrice:       basePrice,
		cachedQuote:     make(map[string]*common.Quote),
		cachedQuoteAt:   make(map[string]time.Time),
		lastValidPrice:  make(map[string]float64),
		lastValidIsReal: make(map[string]bool),
		lastValidAt:     make(map[string]time.Time),
		twelveDataTriedAt: make(map[string]time.Time),
		externalQuote:     make(map[string]*externalQuoteData),
		priceHistory:    make([]priceTick, 0, 5000),
		klineHistory:    make(map[string][]common.KLine),
		currentCandle:   make(map[string]*common.KLine),
		historyFilePath: "data/kline_history.json",
		httpClient:      &http.Client{Timeout: 15 * time.Second},
	}

	// High-frequency synthetic tick config (0.1s live-feel). Defaults keep the
	// previous 1s behaviour if the keys are absent.
	ms.tickInterval = viper.GetDuration("market.tick_interval")
	if ms.tickInterval <= 0 {
		ms.tickInterval = 100 * time.Millisecond
	}
	// Synthetic ticks / simulation are DISABLED by design: the user wants real
	// 1s updates only — no 0.1s synthetic micro-moves and no simulated data.
	// When the market is closed or the live source is unavailable, the feed
	// simply freezes instead of showing fake prices.
	ms.simulateTicks = false
	ms.tickVolatility = viper.GetFloat64("market.tick_volatility")
	if ms.tickVolatility <= 0 {
		ms.tickVolatility = 0.0003 // ±0.03% max step per tick
	}
	log.Printf("MarketService: synthetic ticks=%v interval=%s volatility=%.4f",
		ms.simulateTicks, ms.tickInterval, ms.tickVolatility)
	// Clean stale Redis cache from old XAU spot data on startup
	if rdb != nil && rdb.Client != nil {
		ctx := context.Background()
		keys, err := rdb.Client.Keys(ctx, "market:kline:XAU:*").Result()
		if err == nil && len(keys) > 0 {
			if delErr := rdb.Client.Del(ctx, keys...).Err(); delErr == nil {
				log.Printf("Cleaned %d stale Redis keys for XAU spot", len(keys))
			}
		}
		keys2, err := rdb.Client.Keys(ctx, "market:quote:XAU:*").Result()
		if err == nil && len(keys2) > 0 {
			if delErr := rdb.Client.Del(ctx, keys2...).Err(); delErr == nil {
				log.Printf("Cleaned %d stale Redis keys for XAU quotes", len(keys2))
			}
		}
		// Also clear all kline keys so fresh K-lines are generated with correct high/low
		keys3, err := rdb.Client.Keys(ctx, "market:kline:*").Result()
		if err == nil && len(keys3) > 0 {
			if delErr := rdb.Client.Del(ctx, keys3...).Err(); delErr == nil {
				log.Printf("Cleaned %d stale Redis kline keys (will regenerate)", len(keys3))
			}
		}
	}
	return ms
}

// Start begins polling for quotes
func (s *MarketService) Start(ctx context.Context) {
	// Load accumulated K-line history from local file (survives restarts)
	s.loadKLineHistory()

	// Backfill real intraday history (1m) from a historical source so the chart
	// shows a full session immediately. No-ops gracefully when no API key is set.
	// Runs before pollQuotes so the first REST/WS history response is already full.
	s.seedHistory(ctx)

	go s.pollQuotes(ctx)
	// Real 1s quotes drive everything. We deliberately do NOT run the 0.1s
	// synthetic tick loop (tickLoop) nor the 5s generateKLines rebroadcast:
	// when the market is closed or the live source is down, fetchQuote returns
	// nil and the feed simply freezes — no fake data, no phantom ticks.
	go s.persistLoop(ctx)
	go s.historyRollover(ctx)
	log.Println("Market service started")
}

// loadKLineHistory restores accumulated real K-line history from a local JSON file
func (s *MarketService) loadKLineHistory() {
	data, err := os.ReadFile(s.historyFilePath)
	if err != nil {
		log.Printf("No K-line history file yet (%v) — will accumulate from live quotes", err)
		return
	}
	var store map[string][]common.KLine
	if err := json.Unmarshal(data, &store); err != nil {
		log.Printf("Failed to parse K-line history file: %v", err)
		return
	}
	s.klineHistoryMu.Lock()
	for key, klines := range store {
		if len(klines) == 0 {
			continue
		}
		// Only restore if newer than what's in memory (avoid overwriting live data)
		if len(s.klineHistory[key]) == 0 {
			// De-duplicate by timestamp and sort ascending — stale files may
			// contain a duplicated boundary candle (forming + completed) which
			// would make the chart library reject the whole series.
			dedup := make(map[int64]common.KLine, len(klines))
			for _, k := range klines {
				dedup[k.Timestamp] = k
			}
			clean := make([]common.KLine, 0, len(dedup))
			for _, k := range dedup {
				clean = append(clean, k)
			}
			sort.Slice(clean, func(i, j int) bool { return clean[i].Timestamp < clean[j].Timestamp })
			s.klineHistory[key] = clean
		}
	}
	// Repair any candles corrupted by past bad ticks (e.g. a ~basePrice spike
	// that left a phantom lower/upper wick). Runs after restore so a freshly
	// loaded file is sanitized before it reaches the chart.
	s.healCandles()
	s.klineHistoryMu.Unlock()
	log.Printf("Restored %d K-line history series from %s", len(store), s.historyFilePath)
}

// healCandles clamps any candle whose wick extends more than candleHealMaxWick
// beyond its body back to the body edge. This removes phantom wicks produced
// by transient bad ticks (e.g. a fallback-to-fake-price spike) without
// touching legitimate candles.
func (s *MarketService) healCandles() {
	const candleHealMaxWick = 0.03 // wick longer than 3% of price = clamp
	for key, klines := range s.klineHistory {
		fixed := false
		for i := range klines {
			k := &klines[i]
			bodyBottom := math.Min(k.Open, k.Close)
			bodyTop := math.Max(k.Open, k.Close)
			if k.Low < bodyBottom*(1-candleHealMaxWick) {
				k.Low = bodyBottom
				fixed = true
			}
			if k.High > bodyTop*(1+candleHealMaxWick) {
				k.High = bodyTop
				fixed = true
			}
		}
		if fixed {
			s.klineHistory[key] = klines
			s.historyDirty = true
		}
	}
}

// persistLoop periodically saves accumulated K-line history to disk
func (s *MarketService) persistLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.persistKLineHistory()
			return
		case <-ticker.C:
			s.persistKLineHistory()
		}
	}
}

// persistKLineHistory writes the rolling K-line history to a local JSON file
func (s *MarketService) persistKLineHistory() {
	s.klineHistoryMu.Lock()
	store := make(map[string][]common.KLine, len(s.klineHistory))
	for key, klines := range s.klineHistory {
		if len(klines) > 0 {
			store[key] = klines
		}
	}
	s.klineHistoryMu.Unlock()

	if len(store) == 0 {
		return
	}
	data, err := json.Marshal(store)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.historyFilePath), 0755); err != nil {
		return
	}
	if err := os.WriteFile(s.historyFilePath, data, 0644); err != nil {
		log.Printf("Failed to persist K-line history: %v", err)
		return
	}
	log.Printf("Persisted K-line history: %d series → %s", len(store), s.historyFilePath)
}

func (s *MarketService) pollQuotes(ctx context.Context) {
	// 1s polling: the user wants live ticks once per second (not 0.1s). Each
	// poll fetches a real quote and broadcasts it; if no real quote is
	// available (market closed / sources down) the feed freezes — no synthetic
	// fallback, no simulated data.
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Fetch immediately on startup (so users see real price right away)
	s.fetchAndBroadcast()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.fetchAndBroadcast()
		}
	}
}

// fetchAndBroadcast fetches quotes and broadcasts via WebSocket
func (s *MarketService) fetchAndBroadcast() {
	for _, symbol := range s.symbols {
		quote := s.fetchQuote(symbol, "")
		if quote == nil {
			continue
		}

		// Track price tick for K-line generation
		now := time.Now()
		s.priceHistoryMu.Lock()
		s.priceHistory = append(s.priceHistory, priceTick{Price: quote.Price, Timestamp: now})
		// Keep last 5000 ticks (about 4 hours at 5s intervals)
		if len(s.priceHistory) > 5000 {
			s.priceHistory = s.priceHistory[len(s.priceHistory)-5000:]
		}
		s.priceHistoryMu.Unlock()

		// Update intraday high/low from real price
		s.quoteCacheMu.Lock()
		if cached, ok := s.cachedQuote[symbol]; ok && cached.Open > 0 {
			if quote.Price > cached.High || cached.High == 0 {
				cached.High = quote.Price
			}
			if quote.Price < cached.Low || cached.Low == 0 {
				cached.Low = quote.Price
			}
			// Preserve accumulated high/low
			if cached.High > quote.High {
				quote.High = cached.High
			}
			if cached.Low > 0 && (quote.Low == 0 || cached.Low < quote.Low) {
				quote.Low = cached.Low
			}
		}
		s.quoteCacheMu.Unlock()

		// Update forming K-line candles with real price
		s.updateCandles(symbol, "SPOT", quote.Price, now)

		// Broadcast the updated forming candle(s) immediately so the chart
		// "ticks" in lockstep with every quote poll — no separate slow ticker.
		s.broadcastCurrentCandles()

		// Store in Redis (if available)
		if s.rdb != nil {
			key := fmt.Sprintf("market:quote:%s:%s", symbol, "SPOT")
			priceData, _ := json.Marshal(quote)
			s.rdb.CacheSet(context.Background(), key, string(priceData), 10*time.Second)

			s.rdb.CacheSet(context.Background(),
				fmt.Sprintf("market:price:%s:SPOT", symbol),
				fmt.Sprintf("%.2f", quote.Price), 10*time.Second)
		}

		// Broadcast via WebSocket
		wsMsg, _ := json.Marshal(gin.H{
			"type": "quote",
			"data": quote,
		})
		ch := fmt.Sprintf("quote:%s", symbol)
		s.hub.BroadcastToChannel(ch, wsMsg)

		// Run order matching engine on real quotes too
		if s.tradeSvc != nil {
			s.tradeSvc.CheckPendingOrders(quote)
			s.tradeSvc.CheckStopTriggers(quote)
		}
	}
}

// ========== High-Frequency Synthetic Tick (0.1s live-feel) ==========

// tickLoop drives 0.1s synthetic micro-moves so the chart and quote bar update
// at 10fps even though the real market price only refreshes ~1s from Sina.
// Each synthetic tick is a mean-reverting random walk anchored to the last
// *real* accepted price, so the displayed price never drifts far from the
// actual market. The 1s pollQuotes loop re-anchors to the true price.
func (s *MarketService) tickLoop(ctx context.Context) {
	ticker := time.NewTicker(s.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syntheticTick()
		}
	}
}

// syntheticTick nudges each symbol's displayed price by a tiny mean-reverting
// step and rebroadcasts quote + forming candle over WebSocket.
func (s *MarketService) syntheticTick() {
	now := time.Now()
	for _, symbol := range s.symbols {
		// anchor = last real accepted price (set by acceptPrice on every poll)
		s.lastValidMu.Lock()
		anchor := s.lastValidPrice[symbol]
		if anchor <= 0 && s.simulateTicks {
			// No real quote yet (cold start / source outage). Seed the anchor
			// from the best available reference so the chart keeps ticking.
			anchor = s.seedAnchor(symbol)
			if anchor > 0 {
				s.lastValidPrice[symbol] = anchor
				s.lastValidIsReal[symbol] = false
			}
		}
		s.lastValidMu.Unlock()
		if anchor <= 0 {
			continue // simulation disabled and no real price yet — wait for a poll
		}

		s.quoteCacheMu.Lock()
		cached, ok := s.cachedQuote[symbol]
		if !ok || cached == nil {
			s.quoteCacheMu.Unlock()
			continue
		}
		current := cached.Price
		if current <= 0 {
			current = anchor
		}
		// mean-reverting random walk: pull back toward real price + small noise
		pull := (anchor - current) * 0.15
		noise := anchor * s.tickVolatility * (rand.Float64()*2 - 1)
		newPrice := current + pull + noise
		// hard clamp: never wander more than ±0.2% from the real price
		maxDev := anchor * 0.002
		if newPrice > anchor+maxDev {
			newPrice = anchor + maxDev
		} else if newPrice < anchor-maxDev {
			newPrice = anchor - maxDev
		}
		cached.Price = newPrice
		if newPrice > cached.High || cached.High == 0 {
			cached.High = newPrice
		}
		if newPrice < cached.Low || cached.Low == 0 {
			cached.Low = newPrice
		}
		cached.Timestamp = now.UnixMilli()
		s.quoteCacheMu.Unlock()

		// nudge the live forming candle (close/high/low) without inflating volume
		s.updateFormingCandlePrice(symbol, "SPOT", newPrice, now)

		wsMsg, _ := json.Marshal(gin.H{"type": "quote", "data": cached})
		s.hub.BroadcastToChannel(fmt.Sprintf("quote:%s", symbol), wsMsg)

		// Run order matching engine on each symbol's tick
		if s.tradeSvc != nil && cached != nil {
			s.tradeSvc.CheckPendingOrders(cached)
			s.tradeSvc.CheckStopTriggers(cached)
		}
	}
	// rebroadcast all forming candles once (chart) — cheap, 7 periods only
	s.broadcastCurrentCandles()
}

// updateFormingCandlePrice nudges the forming candle's close/high/low to the
// synthetic price without incrementing Volume (called at high frequency).
func (s *MarketService) updateFormingCandlePrice(symbol, contractMonth string, price float64, t time.Time) {
	periods := []string{"1m", "5m", "15m", "30m", "1h", "4h", "1d"}
	s.currentCandleMu.Lock()
	defer s.currentCandleMu.Unlock()
	for _, period := range periods {
		key := fmt.Sprintf("%s:%s:%s", symbol, contractMonth, period)
		candle, ok := s.currentCandle[key]
		if !ok || candle == nil {
			continue
		}
		if price > candle.High {
			candle.High = price
		}
		if price < candle.Low {
			candle.Low = price
		}
		candle.Close = price
	}
}

// ========== K-Line Candle Management (Real Price Accumulation) ==========

// periodDuration maps period strings to time durations
var periodDurations = map[string]time.Duration{
	"1m":  time.Minute,
	"5m":  5 * time.Minute,
	"15m": 15 * time.Minute,
	"30m": 30 * time.Minute,
	"1h":  time.Hour,
	"4h":  4 * time.Hour,
	"1d":  24 * time.Hour,
}

// periodStart returns the start time of the period containing t
func periodStart(t time.Time, period string) time.Time {
	d, ok := periodDurations[period]
	if !ok {
		d = time.Minute
	}
	return t.Truncate(d)
}

// updateCandles updates the forming candle for each period with the latest price
func (s *MarketService) updateCandles(symbol, contractMonth string, price float64, t time.Time) {
	periods := []string{"1m", "5m", "15m", "30m", "1h", "4h", "1d"}

	s.currentCandleMu.Lock()
	defer s.currentCandleMu.Unlock()

	for _, period := range periods {
		key := fmt.Sprintf("%s:%s:%s", symbol, contractMonth, period)
		ps := periodStart(t, period)

		candle, ok := s.currentCandle[key]
		if !ok || candle.Timestamp < ps.UnixMilli() {
			// Previous candle completed (if any), push to history
			if ok && candle != nil {
				s.pushKLineToHistory(key, *candle)
			}
			// Start new candle
			s.currentCandle[key] = &common.KLine{
				Symbol:        symbol,
				ContractMonth: contractMonth,
				Period:        period,
				Open:          price,
				High:          price,
				Low:           price,
				Close:         price,
				Volume:        1,
				Timestamp:     ps.UnixMilli(),
			}
		} else {
			// Update existing candle
			if price > candle.High {
				candle.High = price
			}
			if price < candle.Low {
				candle.Low = price
			}
			candle.Close = price
			candle.Volume++
		}
	}
}

// pushKLineToHistory adds a completed K-line to the rolling history
func (s *MarketService) pushKLineToHistory(key string, kline common.KLine) {
	s.klineHistoryMu.Lock()
	defer s.klineHistoryMu.Unlock()

	history := s.klineHistory[key]
	// Replace an existing candle with the same timestamp instead of appending a
	// duplicate (the forming candle and a completed candle can share a boundary).
	for i := range history {
		if history[i].Timestamp == kline.Timestamp {
			history[i] = kline
			s.klineHistory[key] = history
			s.broadcastCompletedKLine(kline)
			return
		}
	}
	history = append(history, kline)
	// Keep last klineMaxBars candles per period (≈ a full trading day at 1m)
	if len(history) > klineMaxBars {
		history = history[len(history)-klineMaxBars:]
	}
	s.klineHistory[key] = history

	// Broadcast the completed K-line
	s.broadcastCompletedKLine(kline)
}

func (s *MarketService) broadcastCompletedKLine(kline common.KLine) {
	wsMsg, _ := json.Marshal(gin.H{
		"type": "kline_complete",
		"data": kline,
	})
	ch := fmt.Sprintf("kline:%s:%s", kline.Symbol, kline.Period)
	s.hub.BroadcastToChannel(ch, wsMsg)
}

// getKLinesFromHistory returns accumulated K-lines from real price data
func (s *MarketService) getKLinesFromHistory(symbol, contractMonth, period string, count int) []common.KLine {
	key := fmt.Sprintf("%s:%s:%s", symbol, contractMonth, period)

	// Merge history with the live forming candle, keyed by timestamp so the
	// forming candle overrides any history candle that shares its boundary
	// (otherwise a duplicate timestamp makes the chart library reject the
	// whole series and the chart renders blank).
	merged := make(map[int64]common.KLine)
	s.klineHistoryMu.Lock()
	for _, k := range s.klineHistory[key] {
		merged[k.Timestamp] = k
	}
	s.klineHistoryMu.Unlock()

	s.currentCandleMu.Lock()
	if candle, ok := s.currentCandle[key]; ok && candle != nil {
		merged[candle.Timestamp] = *candle
	}
	s.currentCandleMu.Unlock()

	history := make([]common.KLine, 0, len(merged))
	for _, k := range merged {
		history = append(history, k)
	}
	sort.Slice(history, func(i, j int) bool { return history[i].Timestamp < history[j].Timestamp })

	// Return last 'count' candles
	if len(history) > count {
		history = history[len(history)-count:]
	}

	if len(history) == 0 {
		return nil
	}
	return history
}

// ========== Multi-Source Quote Pipeline ==========
//
// fetchQuote tries data sources in order:
//   0. External push (MT4/local broker bridge, POST /api/v1/market/push) —
//      highest-priority REAL source when fresh (< externalQuoteMaxAge).
//   1. gold-api.io (XAU) — real-time, key-less, CN-cloud reachable (primary cloud)
//   2. Sina Finance (hf_XAU) — free but IP-blocked on Tencent Cloud
//   3. Twelve Data (XAU/USD) — free tier, daily credit limits
//   4. No data — feed freezes (no simulation, no fake prices)
//
// Successful real quotes are cached for quoteCacheDuration.

func (s *MarketService) fetchQuote(symbol, contractMonth string) *common.Quote {
	// Source 0: externally pushed quote (MT4/local broker bridge) — the highest
	// priority REAL source when fresh. Checked BEFORE the cache shortcut so a
	// fresh push always wins over a slightly-stale cloud fetch.
	if q := s.getFreshExternalQuote(symbol); q != nil {
		s.cacheQuote(symbol, q)
		return q
	}

	// Return cached quote if still fresh
	s.quoteCacheMu.Lock()
	if q, ok := s.cachedQuote[symbol]; ok && time.Since(s.cachedQuoteAt[symbol]) < quoteCacheDuration {
		s.quoteCacheMu.Unlock()
		return q
	}
	s.quoteCacheMu.Unlock()

	// Source 1: gold-api.io — real-time London Gold Spot (XAU/USD), free,
	// key-less, and reachable from Tencent Cloud (Sina hf_XAU is IP-blocked
	// there, Twelve Data hits daily credit limits). Primary live source.
	if q, err := s.fetchFromGoldApi(symbol); err == nil && q != nil && s.acceptPrice(symbol, q.Price) {
		log.Printf("[quote] gold-api.io: %s=%.2f", symbol, q.Price)
		s.cacheQuote(symbol, q)
		return q
	}
	log.Printf("[quote] gold-api.io failed for %s, trying Sina...", symbol)

	// Source 2: Sina Finance (hf_XAU London gold spot) — IP-blocked on CN
	// cloud, kept as a fallback in case the block is lifted.
	if q, err := s.fetchFromSina(symbol); err == nil && q != nil && s.acceptPrice(symbol, q.Price) {
		log.Printf("[quote] Sina Finance: %s=%.2f", symbol, q.Price)
		s.cacheQuote(symbol, q)
		return q
	}
	log.Printf("[quote] Sina Finance failed/unstable for %s, trying Twelve Data...", symbol)

	// Source 3: Twelve Data (throttled — free tier is ~800 credits/day, so we
	// only probe at most once per minute; the simulation below covers the gaps).
	s.twelveCooldownMu.Lock()
	probe := time.Since(s.twelveDataTriedAt[symbol]) >= twelveDataCooldown
	if probe {
		s.twelveDataTriedAt[symbol] = time.Now()
	}
	s.twelveCooldownMu.Unlock()
	if probe {
		if q, err := s.fetchFromTwelveData(symbol); err == nil && q != nil && s.acceptPrice(symbol, q.Price) {
			log.Printf("[quote] Twelve Data fallback: %s=%.2f", symbol, q.Price)
			s.cacheQuote(symbol, q)
			return q
		}
	} else {
		log.Printf("[quote] %s Twelve Data in cooldown (simulation covers feed)", symbol)
	}

	// No real source returned a fresh quote. Per design we do NOT synthesize
	// fake prices and we do NOT hold a stale quote as "live" — the feed simply
	// stops (freezes) until the market reopens or the live source recovers.
	log.Printf("[quote] %s no fresh real quote (market closed or sources down) — feed frozen", symbol)
	return nil
}

// acceptPrice validates a fresh price against the last accepted price. A single
// tick moving more than badTickMaxDeviation (3%) from the last sane price is
// treated as a bad tick (exchange glitch) and rejected — it never reaches the
// candle pipeline. The first price after a cold start is always accepted.
func (s *MarketService) acceptPrice(symbol string, price float64) bool {
	if price <= 0 {
		return false
	}
	s.lastValidMu.Lock()
	defer s.lastValidMu.Unlock()
	last := s.lastValidPrice[symbol]
	// Reject a single tick that deviates more than badTickMaxDeviation from the
	// last sane price — that's an exchange glitch, never reaching the candles.
	// Exception: after a long gap (market was closed) the reopen price may
	// legitimately differ, so skip the filter and re-anchor to the live market.
	if last > 0 && s.lastValidIsReal[symbol] && time.Since(s.lastValidAt[symbol]) < longGapResetThreshold {
		dev := math.Abs(price-last) / last
		if dev > badTickMaxDeviation {
			log.Printf("[quote] rejected bad tick for %s: %.2f (last sane %.2f, dev %.2f%%)",
				symbol, price, last, dev*100)
			return false
		}
	}
	s.lastValidPrice[symbol] = price
	s.lastValidIsReal[symbol] = true
	s.lastValidAt[symbol] = time.Now()
	return true
}

func (s *MarketService) cacheQuote(symbol string, q *common.Quote) {
	s.quoteCacheMu.Lock()
	s.cachedQuote[symbol] = q
	s.cachedQuoteAt[symbol] = time.Now()
	s.quoteCacheMu.Unlock()
}

// getFreshExternalQuote returns the latest externally-pushed quote for a symbol
// if it arrived within externalQuoteMaxAge (i.e. the local bridge is alive).
// Returns nil when there's no push yet or the bridge has gone silent, so the
// caller falls back to the autonomous cloud sources.
func (s *MarketService) getFreshExternalQuote(symbol string) *common.Quote {
	s.externalQuoteMu.Lock()
	eq, ok := s.externalQuote[symbol]
	s.externalQuoteMu.Unlock()
	if !ok || eq == nil {
		return nil
	}
	if time.Since(eq.At) > externalQuoteMaxAge {
		return nil
	}
	return eq.Quote
}

// getPushToken resolves the shared secret for the external push endpoint,
// preferring config > env > built-in default.
func getPushToken() string {
	if t := viper.GetString("market.push.token"); t != "" {
		return t
	}
	if t := os.Getenv("MARKET_PUSH_TOKEN"); t != "" {
		return t
	}
	return marketPushTokenDefault
}

// PushQuote accepts a real-time quote pushed from an external source (e.g. the
// user's MT4 broker feed via the local bridge). It is token-protected and
// public (no user auth) so the bridge can reach it over the internet.
//
// Expected JSON body:
//
//	{"symbol":"XAU","price":4605.0,"bid":4604.8,"ask":4605.2,"high":...,"low":...}
//
// `symbol` defaults to XAU; `price` falls back to the bid/ask mid if omitted.
func (s *MarketService) PushQuote(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
			token = strings.TrimPrefix(h, "Bearer ")
		}
	}
	if token != getPushToken() {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}

	var body struct {
		Symbol        string  `json:"symbol"`
		ContractMonth string  `json:"contract_month"`
		Price         float64 `json:"price"`
		Bid           float64 `json:"bid"`
		Ask           float64 `json:"ask"`
		High          float64 `json:"high"`
		Low           float64 `json:"low"`
		Open          float64 `json:"open"`
		Volume        int64   `json:"volume"`
		Timestamp     int64   `json:"timestamp"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad json: " + err.Error()})
		return
	}
	if body.Symbol == "" {
		body.Symbol = "XAU"
	}
	price := body.Price
	if price <= 0 && body.Bid > 0 && body.Ask > 0 {
		price = (body.Bid + body.Ask) / 2
	}
	if price <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "price missing or invalid"})
		return
	}

	now := time.Now()
	q := &common.Quote{
		Symbol:        body.Symbol,
		ContractMonth: "SPOT",
		Bid:           body.Bid,
		Ask:           body.Ask,
		Price:         price,
		Open:          body.Open,
		High:          body.High,
		Low:           body.Low,
		Volume:        body.Volume,
		Timestamp:     now.UnixMilli(),
	}

	s.externalQuoteMu.Lock()
	s.externalQuote[body.Symbol] = &externalQuoteData{Quote: q, At: now}
	s.externalQuoteMu.Unlock()

	// Keep lastValid in sync. The MT4 broker feed is the most authoritative
	// price we have, so we bypass the bad-tick deviation filter for it.
	s.lastValidMu.Lock()
	s.lastValidPrice[body.Symbol] = price
	s.lastValidIsReal[body.Symbol] = true
	s.lastValidAt[body.Symbol] = now
	s.lastValidMu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"symbol": body.Symbol,
		"price":  price,
		"at":     now.UnixMilli(),
	})
}

// lastRealQuote returns the most recently cached real quote for a symbol
// (if any). Used to serve a frozen-but-real price during 休市 instead of an
// error when the live feed is unavailable.
func (s *MarketService) lastRealQuote(symbol string) *common.Quote {
	s.quoteCacheMu.Lock()
	defer s.quoteCacheMu.Unlock()
	if q, ok := s.cachedQuote[symbol]; ok && q != nil && q.Price > 0 {
		return q
	}
	return nil
}

// seedAnchor returns the best available reference price to anchor synthetic
// ticks when no live quote is reachable. Priority: cached quote > last known
// K-line close (most recent real close) > configured base price.
func (s *MarketService) seedAnchor(symbol string) float64 {
	s.quoteCacheMu.Lock()
	if q, ok := s.cachedQuote[symbol]; ok && q != nil && q.Price > 0 {
		s.quoteCacheMu.Unlock()
		return q.Price
	}
	s.quoteCacheMu.Unlock()

	s.klineHistoryMu.Lock()
	if hist, ok := s.klineHistory[symbol+":SPOT:1m"]; ok && len(hist) > 0 {
		if last := hist[len(hist)-1].Close; last > 0 {
			s.klineHistoryMu.Unlock()
			return last
		}
	}
	s.klineHistoryMu.Unlock()

	return s.basePrice
}

// ========== Sina Finance Integration (Primary) ==========
//
// Sina Finance provides free, real-time London Gold Spot (XAU/USD) via hf_XAU.
// No API key required; works well from Chinese servers/IPs.

// fetchFromSina fetches London Gold Spot (XAU/USD) from Sina Finance.
// URL: https://hq.sinajs.cn/list=hf_XAU
func (s *MarketService) fetchFromSina(symbol string) (*common.Quote, error) {
	url := fmt.Sprintf("https://hq.sinajs.cn/list=hf_XAU")

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	raw := string(body)

	// Handle empty response (symbol not available)
	if strings.TrimSpace(raw) == "" || strings.Contains(raw, `""`) {
		return nil, fmt.Errorf("sina returned empty data for hf_XAU")
	}

	// Extract the quoted portion after "hf_XX="
	idx := strings.Index(raw, "\"")
	if idx < 0 {
		return nil, fmt.Errorf("unexpected sina response format: %s", raw)
	}
	endIdx := strings.LastIndex(raw, "\"")
	if endIdx <= idx {
		return nil, fmt.Errorf("unexpected sina response format")
	}

	data := raw[idx+1 : endIdx]
	fields := strings.Split(data, ",")
	if len(fields) < 9 {
		return nil, fmt.Errorf("sina hf_XAU response has only %d fields, expected >= 9", len(fields))
	}

	// Sina hf_XAU field mapping (London Gold Spot):
	// [0]=current price, [1]=prev_settle, [2]=bid, [3]=ask,
	// [4]=open, [5]=prev_close, [6]=time,
	// [7]=reference price (NOT today's high — same as prev_settle for futures),
	// [8]=reference price (NOT today's low)
	price := parseFloatSafe(fields[0])
	if price == 0 {
		return nil, fmt.Errorf("sina hf_XAU returned zero price")
	}

	bid := parseFloatSafe(fields[2])
	ask := parseFloatSafe(fields[3])
	open := parseFloatSafe(fields[4])
	prevClose := parseFloatSafe(fields[5])

	// hf_XAU fields[7]/[8] are NOT intraday high/low — they're reference prices
	// (often matching prev_settle). Calculate high/low from price and open instead.
	high := price
	low := price
	if open > 0 {
		if open > high {
			high = open
		}
		if open < low {
			low = open
		}
	}

	change := price - prevClose
	changePct := 0.0
	if prevClose > 0 {
		changePct = (change / prevClose) * 100
	}

	now := time.Now()

	return &common.Quote{
		Symbol:         "XAU",
		ContractMonth:  "SPOT",
		Bid:            bid,
		Ask:            ask,
		Price:          price,
		Open:           open,
		High:           high,
		Low:            low,
		PreviousSettle: prevClose,
		Volume:         0,
		OpenInterest:   450000,
		Change:         change,
		ChangePercent:  changePct,
		Timestamp:      now.UnixMilli(),
	}, nil
}

// parseFloatSafe parses a string to float64, returning 0 on any error.
func parseFloatSafe(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

// ========== K-Line Generation ==========

func (s *MarketService) generateKLines(ctx context.Context) {
	// Broadcast current forming candles every 5 seconds for real-time chart updates
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.broadcastCurrentCandles()
		}
	}
}

// broadcastCurrentCandles sends the latest forming candle to WebSocket clients
func (s *MarketService) broadcastCurrentCandles() {
	s.currentCandleMu.Lock()
	defer s.currentCandleMu.Unlock()

	periods := []string{"1m", "5m", "15m", "30m", "1h", "4h", "1d"}
	for _, period := range periods {
		key := fmt.Sprintf("XAU:SPOT:%s", period)
		if candle, ok := s.currentCandle[key]; ok && candle != nil {
			wsMsg, _ := json.Marshal(gin.H{
				"type": "kline",
				"data": candle,
			})
			ch := fmt.Sprintf("kline:XAU:%s", period)
			s.hub.BroadcastToChannel(ch, wsMsg)
		}
	}
}

func (s *MarketService) createKLine(period string) {
	quote := s.fetchQuote("XAU", "SPOT")
	if quote == nil {
		return
	}

	kline := common.KLine{
		Symbol:        "XAU",
		ContractMonth: "SPOT",
		Period:        period,
		Open:          quote.Open,
		High:          quote.High,
		Low:           quote.Low,
		Close:         quote.Price,
		Volume:        float64(quote.Volume),
		Timestamp:     quote.Timestamp,
	}

	key := fmt.Sprintf("market:kline:XAU:SPOT:%s:latest", period)
	data, _ := json.Marshal(kline)
	if s.rdb != nil {
		s.rdb.CacheSet(context.Background(), key, string(data), 24*time.Hour)
	}
	s.cachedKLines.Store(key, string(data))

	wsMsg, _ := json.Marshal(gin.H{
		"type": "kline",
		"data": kline,
	})
	ch := fmt.Sprintf("kline:XAU:%s", period)
	s.hub.BroadcastToChannel(ch, wsMsg)
}

// GetCachedQuote returns the most recently cached quote for a symbol.
// This allows other services (e.g. TradeService) to access real-time prices
// without needing Redis.
func (s *MarketService) GetCachedQuote(symbol string) *common.Quote {
	s.quoteCacheMu.Lock()
	defer s.quoteCacheMu.Unlock()
	if q, ok := s.cachedQuote[symbol]; ok {
		return q
	}
	return nil
}

// ========== HTTP Handlers ==========

func (s *MarketService) GetQuote(c *gin.Context) {
	symbol := c.DefaultQuery("symbol", "XAU")
	contractMonth := c.DefaultQuery("contract_month", "SPOT")

	quote := s.fetchQuote(symbol, contractMonth)
	if quote == nil {
		// 休市期或所有行情源不可用：返回最后一次真实报价（冻结展示），
		// 不编造模拟数据，也不报错导致前端空白。
		if last := s.lastRealQuote(symbol); last != nil {
			common.Success(c, last)
			return
		}
		common.Error(c, errs.InvalidSymbol, "休市中或行情源不可用")
		return
	}
	common.Success(c, quote)
}

func (s *MarketService) GetKLines(c *gin.Context) {
	symbol := c.DefaultQuery("symbol", "XAU")
	contractMonth := c.DefaultQuery("contract_month", "SPOT")
	period := c.DefaultQuery("period", "1m")

	// 只返回真实累积的 K 线（从真实价格 tick 生成），不掺入任何模拟数据。
	// 上限 klineMaxBars，足够铺满一整天 1m 分时。
	realKLines := s.getKLinesFromHistory(symbol, contractMonth, period, klineMaxBars)
	if len(realKLines) == 0 {
		// 服务刚启动、尚无真实数据时返回空数组（前端显示空图表）
		common.Success(c, []common.KLine{})
		return
	}
	common.Success(c, realKLines)
}

func (s *MarketService) GetSymbols(c *gin.Context) {
	symbols := []gin.H{
		{
			"symbol":           "XAU",
			"name":             "伦敦金现货 XAU/USD",
			"exchange":         "London Bullion Market (LBM)",
			"contract_size":    100,
			"contract_month":   "SPOT",
			"tick_size":        0.01,
			"tick_value":       1.0,
			"min_volume":       0.01,
			"max_volume":       100,
			"max_leverage":     1000,
			"spread":           0.35,
			"trading_hours":    "周一至周五 全天 (XAU/USD 近乎24小时连续交易, 周末休市)",
			"available_months": []string{"SPOT"},
		},
	}
	common.Success(c, symbols)
}

func (s *MarketService) WebSocketHandler(c *gin.Context) {
	userID := c.GetInt64("user_id")
	ws.ServeWs(s.hub, userID, c.Writer, c.Request)
}

func (s *MarketService) HealthCheck(c *gin.Context) {
	dataSource := "no-data"
	// External (MT4/local bridge) push is the preferred real source.
	s.externalQuoteMu.Lock()
	for _, eq := range s.externalQuote {
		if eq != nil && time.Since(eq.At) <= externalQuoteMaxAge {
			dataSource = "mt4-push"
			break
		}
	}
	s.externalQuoteMu.Unlock()
	// Fall back to checking the cloud-sourced cached quote.
	if dataSource == "no-data" {
		s.quoteCacheMu.Lock()
		if len(s.cachedQuote) > 0 {
			for sym := range s.cachedQuote {
				if time.Since(s.cachedQuoteAt[sym]) < quoteCacheDuration*2 {
					dataSource = "real"
					break
				}
			}
		}
		s.quoteCacheMu.Unlock()
	}

	c.JSON(http.StatusOK, gin.H{
		"status":            "ok",
		"symbols":           s.symbols,
		"websocket_clients": len(s.hub.Clients),
		"data_source":       dataSource,
	})
}

// ========== Twelve Data Integration ==========

type TwelveDataQuote struct {
	Symbol        string  `json:"symbol"`
	Price         float64 `json:"price,string"`
	Bid           float64 `json:"bid,string"`
	Ask           float64 `json:"ask,string"`
	Change        float64 `json:"change,string"`
	ChangePercent float64 `json:"percent_change,string"`
	High          float64 `json:"high,string"`
	Low           float64 `json:"low,string"`
	Open          float64 `json:"open,string"`
	PreviousClose float64 `json:"previous_close,string"`
	Close         float64 `json:"close,string"`
	Timestamp     int64   `json:"timestamp"`
	Volume        int64   `json:"volume,string"`
}

// TwelveDataErrorResponse captures Twelve Data API error responses.
type TwelveDataErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// getTwelveDataAPIKey returns the Twelve Data API key.
// Priority: TWELVEDATA_API_KEY env var > market.twelvedata.api_key in config.yaml.
// Returns empty string if neither is configured (caller should skip Twelve Data).
func getTwelveDataAPIKey() string {
	if key := os.Getenv("TWELVEDATA_API_KEY"); key != "" {
		return key
	}
	return viper.GetString("market.twelvedata.api_key")
}

// fetchFromGoldApi fetches real-time London Gold Spot (XAU/USD) from
// gold-api.io. It is a free, key-less endpoint and — critically — is
// reachable from Tencent Cloud datacenters, unlike Sina hf_XAU (IP-blocked)
// and Twelve Data (daily credit caps). This makes it the primary live source.
func (s *MarketService) fetchFromGoldApi(symbol string) (*common.Quote, error) {
	url := "https://api.gold-api.com/price/XAU"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("gold-api create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gold-api http: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gold-api status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gold-api read: %w", err)
	}

	var ga struct {
		Price     float64 `json:"price"`
		UpdatedAt string  `json:"updatedAt"`
	}
	if err := json.Unmarshal(body, &ga); err != nil {
		return nil, fmt.Errorf("gold-api parse: %w", err)
	}
	if ga.Price <= 0 {
		return nil, fmt.Errorf("gold-api returned zero price")
	}

	// gold-api.io keeps returning the last price during market closure, but
	// stops advancing updatedAt. Treat a stale update as "no live data" so the
	// feed freezes during 休市 instead of displaying a frozen fake price.
	if ga.UpdatedAt != "" {
		if updatedAt, perr := time.Parse(time.RFC3339, ga.UpdatedAt); perr == nil {
			if age := time.Since(updatedAt); age > goldApiStaleThreshold {
				return nil, fmt.Errorf("gold-api quote stale (updated %s, %s ago — market likely closed)",
					ga.UpdatedAt, age.Round(time.Second))
			}
		}
	}

	now := time.Now()
	return &common.Quote{
		Symbol:         symbol,
		ContractMonth:  "SPOT",
		Bid:            ga.Price - 0.05,
		Ask:            ga.Price + 0.05,
		Price:          ga.Price,
		Open:           ga.Price,
		High:           ga.Price,
		Low:            ga.Price,
		PreviousSettle: ga.Price,
		Volume:         0,
		OpenInterest:   450000,
		Timestamp:      now.UnixMilli(),
	}, nil
}

// fetchFromTwelveData fetches real quotes from Twelve Data API.
// Requires a valid API key (free tier available at https://twelvedata.com/pricing).
// Set via TWELVEDATA_API_KEY env var or market.twelvedata.api_key in config.yaml.
// Without a valid key, this source is skipped gracefully.
func (s *MarketService) fetchFromTwelveData(symbol string) (*common.Quote, error) {
	apiKey := getTwelveDataAPIKey()
	if apiKey == "" || apiKey == "demo" {
		return nil, fmt.Errorf("twelvedata: no valid API key configured (set TWELVEDATA_API_KEY env var)")
	}

	// Always fetch XAU/USD from Twelve Data
	tdSymbol := "XAU/USD"

	baseURL := viper.GetString("market.twelvedata.base_url")
	if baseURL == "" {
		baseURL = "https://api.twelvedata.com"
	}
	url := fmt.Sprintf("%s/quote?symbol=%s&apikey=%s", baseURL, tdSymbol, apiKey)

	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("twelvedata http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("twelvedata read: %w", err)
	}

	// Check for error response first
	var errResp TwelveDataErrorResponse
	if json.Unmarshal(body, &errResp) == nil && errResp.Status == "error" {
		return nil, fmt.Errorf("twelvedata error [%d]: %s", errResp.Code, errResp.Message)
	}

	// Parse quote response
	var td TwelveDataQuote
	if err := json.Unmarshal(body, &td); err != nil {
		return nil, fmt.Errorf("twelvedata parse: %w", err)
	}

	// Twelve Data /quote returns "close" (not "price"); use it as the price.
	price := td.Price
	if price == 0 {
		price = td.Close
	}
	// Validate: real quotes always have non-zero price
	if price == 0 {
		return nil, fmt.Errorf("twelvedata returned zero price")
	}
	// Forex quotes often omit bid/ask — derive a tiny spread so the order
	// ticket isn't shown with a zero spread.
	if td.Bid == 0 || td.Ask == 0 {
		td.Bid = price - 0.05
		td.Ask = price + 0.05
	}

	now := time.Now()
	return &common.Quote{
		Symbol:         symbol,
		ContractMonth:  "SPOT",
		Bid:            td.Bid,
		Ask:            td.Ask,
		Price:          price,
		Open:           td.Open,
		High:           td.High,
		Low:            td.Low,
		PreviousSettle: td.PreviousClose,
		Volume:         td.Volume,
		OpenInterest:   450000,
		Change:         td.Change,
		ChangePercent:  td.ChangePercent,
		Timestamp:      now.UnixMilli(),
	}, nil
}
