package service

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	"github.com/wnjoon/go-yfinance/pkg/models"
	"github.com/wnjoon/go-yfinance/pkg/ticker"
)

type TrackerCacheEntry struct {
	Returns   []float64 `json:"returns"`
	UpdatedAt time.Time `json:"updated_at"`
}

type MarketDataService struct {
	cacheRepo *repository.CacheRepository
	dataDir   string
}

func NewMarketDataService(cache *repository.CacheRepository, dataDir string) *MarketDataService {
	return &MarketDataService{
		cacheRepo: cache,
		dataDir:   dataDir,
	}
}

func (s *MarketDataService) GetCachedReturns(ticker string) ([]float64, bool) {
	// Try the new file-based cache first
	cache, err := s.loadTrackerCache()
	if err == nil {
		if entry, ok := cache[ticker]; ok {
			// Check if cache is still valid (1 week)
			if time.Since(entry.UpdatedAt) < 7*24*time.Hour {
				return entry.Returns, true
			}
		}
	}

	// Fallback to the old DB-based cache (might be useful for transition)
	cacheKey := fmt.Sprintf("returns:%s", ticker)
	if data, ok, _ := s.cacheRepo.Get(cacheKey); ok {
		var returns []float64
		if err := json.Unmarshal([]byte(data), &returns); err == nil {
			return returns, true
		}
	}

	return nil, false
}

func (s *MarketDataService) loadTrackerCache() (map[string]TrackerCacheEntry, error) {
	cachePath := filepath.Join(s.dataDir, "caches", "tracker_history.json")
	f, err := os.Open(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]TrackerCacheEntry), nil
		}
		return nil, err
	}
	defer f.Close()

	var cache map[string]TrackerCacheEntry
	if err := json.NewDecoder(f).Decode(&cache); err != nil {
		return nil, err
	}
	return cache, nil
}

func (s *MarketDataService) saveTrackerCache(cache map[string]TrackerCacheEntry) error {
	cachePath := filepath.Join(s.dataDir, "caches", "tracker_history.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return err
	}

	f, err := os.Create(cachePath)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewEncoder(f).Encode(cache)
}

func (s *MarketDataService) GetHistoricalWeeklyReturns(tSymbol string, conversionSymbol string) ([]float64, error) {
	log.Printf("[MARKET_DATA] Fetching historical weekly returns for ticker %s (conversion: %s)", tSymbol, conversionSymbol)

	cacheKey := tSymbol
	if conversionSymbol != "" {
		cacheKey = fmt.Sprintf("%s_%s", tSymbol, conversionSymbol)
	}

	// 1. Try Cache
	cache, _ := s.loadTrackerCache()
	if entry, ok := cache[cacheKey]; ok {
		if time.Since(entry.UpdatedAt) < 7*24*time.Hour {
			log.Printf("[MARKET_DATA] Using cached returns for %s (age: %v)", cacheKey, time.Since(entry.UpdatedAt))
			return entry.Returns, nil
		}
	}

	// 2. Fetch from Yahoo Finance
	t, err := ticker.New(tSymbol)
	if err != nil {
		return nil, fmt.Errorf("failed to create yfinance ticker: %w", err)
	}
	defer t.Close()

	params := models.HistoryParams{
		Interval: "1wk",
		Period:   "max",
	}

	bars, err := t.History(params)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch history from yfinance: %w", err)
	}

	if len(bars) < 2 {
		return nil, fmt.Errorf("not enough data points for ticker %s", tSymbol)
	}

	// Handle Conversion
	if conversionSymbol != "" {
		ct, err := ticker.New(conversionSymbol)
		if err != nil {
			return nil, fmt.Errorf("failed to create conversion ticker: %w", err)
		}
		defer ct.Close()

		cBars, err := ct.History(params)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch conversion history: %w", err)
		}

		// Align bars by date
		cMap := make(map[string]float64)
		for _, b := range cBars {
			cMap[b.Date.Format("2006-01-02")] = b.AdjClose
		}

		var alignedBars []models.Bar
		for _, b := range bars {
			dateStr := b.Date.Format("2006-01-02")
			if cPrice, ok := cMap[dateStr]; ok && cPrice > 0 {
				// We assume conversion is Division (e.g. Index is USD, Conversion is EURUSD=X)
				// Price_EUR = Price_USD / Rate_EURUSD
				b.AdjClose = b.AdjClose / cPrice
				alignedBars = append(alignedBars, b)
			}
		}
		bars = alignedBars
	}

	if len(bars) < 2 {
		return nil, fmt.Errorf("not enough aligned data points for ticker %s after conversion", tSymbol)
	}

	// Calculate returns (Newest First)
	var returns []float64
	for i := len(bars) - 1; i > 0; i-- {
		newer := bars[i].AdjClose
		older := bars[i-1].AdjClose

		if older > 0 {
			ret := (newer - older) / older
			returns = append(returns, ret)
		}
	}

	// 3. Update Cache
	if cache == nil {
		cache = make(map[string]TrackerCacheEntry)
	}
	cache[cacheKey] = TrackerCacheEntry{
		Returns:   returns,
		UpdatedAt: time.Now(),
	}
	if err := s.saveTrackerCache(cache); err != nil {
		log.Printf("[MARKET_DATA] Failed to save cache: %v", err)
	}

	// Also update DB-based cache for compatibility
	dbCacheKey := fmt.Sprintf("returns:%s", cacheKey)
	jsonBytes, _ := json.Marshal(returns)
	s.cacheRepo.Set(dbCacheKey, string(jsonBytes))

	return returns, nil
}
