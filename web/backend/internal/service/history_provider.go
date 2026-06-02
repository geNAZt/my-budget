package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	"github.com/wnjoon/go-yfinance/pkg/models"
	"github.com/wnjoon/go-yfinance/pkg/ticker"
)

type HistoryProvider interface {
	GetHistory(t domain.ETFTracker) ([]models.Bar, error)
}

// Helper to get cache key
func getHistoryCacheKey(providerPrefix, historicalTicker, conversionTracker string) string {
	cacheKey := fmt.Sprintf("%s:%s", providerPrefix, historicalTicker)
	if conversionTracker != "" {
		cacheKey += "_" + conversionTracker
	}
	return cacheKey
}

type cachedHistory struct {
	Bars      []models.Bar `json:"bars"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// Helper to find data in a window
func findInWindow(m map[string]float64, target time.Time, windowDays int) (float64, bool) {
	for i := 0; i <= windowDays; i++ {
		// Check target - i days and target + i days
		d1 := target.AddDate(0, 0, -i).Format("2006-01-02")
		if v, ok := m[d1]; ok && v > 0 {
			return v, true
		}
		d2 := target.AddDate(0, 0, i).Format("2006-01-02")
		if v, ok := m[d2]; ok && v > 0 {
			return v, true
		}
	}
	return 0, false
}

// fetchDailyFX fetches daily FX data from Yahoo for a given ticker.
func fetchDailyFX(symbol string) (map[string]float64, error) {
	if symbol == "" {
		return make(map[string]float64), nil
	}

	params1d := models.HistoryParams{
		Interval: "1d",
		Period:   "max",
	}

	ct, err := ticker.New(symbol)
	if err != nil {
		return nil, err
	}
	defer ct.Close()

	cBars, err := ct.History(params1d)
	if err != nil {
		return nil, err
	}

	fxMap := make(map[string]float64)
	for _, b := range cBars {
		fxMap[b.Date.Format("2006-01-02")] = b.AdjClose
	}
	return fxMap, nil
}

type YahooHistoryProvider struct {
	cacheRepo *repository.CacheRepository
}

func NewYahooHistoryProvider(cache *repository.CacheRepository) *YahooHistoryProvider {
	return &YahooHistoryProvider{cacheRepo: cache}
}

func (p *YahooHistoryProvider) GetHistory(t domain.ETFTracker) ([]models.Bar, error) {
	historicalTicker := t.HistoricalTracker
	if historicalTicker == "" {
		historicalTicker = t.Tracker
	}

	cacheKey := getHistoryCacheKey("yahoo_history", historicalTicker, t.ConversionTracker)
	
	if data, ok, _ := p.cacheRepo.Get(cacheKey); ok {
		var cached cachedHistory
		if err := json.Unmarshal([]byte(data), &cached); err == nil {
			if time.Since(cached.UpdatedAt) < 7*24*time.Hour {
				log.Printf("[YAHOO_PROVIDER] Cache hit for %s", cacheKey)
				return cached.Bars, nil
			}
		}
	}

	log.Printf("[YAHOO_PROVIDER] Cache miss for %s", cacheKey)

	// Fetch daily data because yfinance '1wk' over 'max' period often drops weeks entirely.
	params := models.HistoryParams{
		Interval: "1d",
		Period:   "max",
	}

	tTicker, err := ticker.New(historicalTicker)
	if err != nil {
		return nil, fmt.Errorf("failed to create ticker for %s: %v", historicalTicker, err)
	}
	defer tTicker.Close()

	bars, err := tTicker.History(params)
	if err != nil || len(bars) < 2 {
		return nil, fmt.Errorf("failed to fetch history for %s: %v", historicalTicker, err)
	}

	fxMap, _ := fetchDailyFX(t.ConversionTracker)

	// Anchoring Logic
	scaleMultiplier := 1.0
	if t.Tracker != "" && t.Tracker != historicalTicker {
		at, _ := ticker.New(t.Tracker)
		if at != nil {
			defer at.Close()
			aBars, err := at.History(params)
			if err == nil && len(aBars) > 0 {
				anchorFound := false
				for i := len(aBars) - 1; i >= 0; i-- {
					etfClose := aBars[i].AdjClose
					if etfClose <= 0 {
						continue
					}

					indexClose := 0.0
					for _, b := range bars {
						if b.Date.Format("2006-01-02") == aBars[i].Date.Format("2006-01-02") {
							indexClose = b.AdjClose
							break
						}
					}

					if indexClose > 0 {
						fxRate := 1.0
						if t.ConversionTracker != "" {
							if rate, ok := findInWindow(fxMap, aBars[i].Date, 7); ok {
								fxRate = rate
							} else {
								continue
							}
						}

						indexInEUR := indexClose / fxRate
						scaleMultiplier = etfClose / indexInEUR
						log.Printf("[YAHOO_PROVIDER] Anchored scale for %s: %e (ETF: %.4f, IndexInEUR: %.4f)", t.Tracker, scaleMultiplier, etfClose, indexInEUR)
						anchorFound = true
						break
					}
				}
				if !anchorFound {
					log.Printf("[YAHOO_PROVIDER] WARNING: No anchor point found for %s", t.Tracker)
				}
			}
		}
	}

	// Build Weekly History Slice
	var history []models.Bar
	log.Printf("[YAHOO_PROVIDER] Building weekly history slice from %d daily bars. Scale: %e", len(bars), scaleMultiplier)

	if len(bars) == 0 {
		return history, nil
	}

	// Create a map of daily values by date string for easy lookup
	dailyValues := make(map[string]float64)
	for _, b := range bars {
		fxRate := 1.0
		if t.ConversionTracker != "" {
			if rate, ok := findInWindow(fxMap, b.Date, 7); ok {
				fxRate = rate
			} else {
				continue
			}
		}
		dailyValues[b.Date.Format("2006-01-02")] = (b.AdjClose / fxRate) * scaleMultiplier
	}

	minDate := bars[0].Date
	maxDate := bars[len(bars)-1].Date

	// Iterate through every single week from min to max to guarantee a contiguous sequence
	curr := minDate
	lastPrice := 0.0

	for !curr.After(maxDate) {
		isoYear, isoWeek := curr.ISOWeek()
		
		// Look for a price in the current week. Since we want the *latest* price of the week,
		// we check dates from Sunday back to Monday.
		weekStart := isoWeekToDate(isoYear, isoWeek)
		for i := 6; i >= 0; i-- {
			checkDate := weekStart.AddDate(0, 0, i)
			if price, ok := dailyValues[checkDate.Format("2006-01-02")]; ok {
				lastPrice = price
				break
			}
		}

		if lastPrice > 0 {
			history = append(history, models.Bar{
				Date:     weekStart, // Standardize to the Monday of the week
				AdjClose: lastPrice,
			})
		}

		curr = curr.AddDate(0, 0, 7)
	}

	cached := cachedHistory{
		Bars:      history,
		UpdatedAt: time.Now(),
	}
	cacheBytes, _ := json.Marshal(cached)
	p.cacheRepo.Set(cacheKey, string(cacheBytes))

	return history, nil
}

type SolactiveHistoryProvider struct {
	cacheRepo *repository.CacheRepository
}

func NewSolactiveHistoryProvider(cache *repository.CacheRepository) *SolactiveHistoryProvider {
	return &SolactiveHistoryProvider{cacheRepo: cache}
}

type solactiveRequest struct {
	ISIN                   string `json:"isin"`
	IndexCreatingTimeStamp int    `json:"indexCreatingTimeStamp"`
	DayDate                int64  `json:"dayDate"`
}

func (p *SolactiveHistoryProvider) GetHistory(t domain.ETFTracker) ([]models.Bar, error) {
	historicalTicker := t.HistoricalTracker
	if historicalTicker == "" {
		historicalTicker = t.Tracker
	}

	cacheKey := getHistoryCacheKey("solactive_history", historicalTicker, t.ConversionTracker)
	
	if data, ok, _ := p.cacheRepo.Get(cacheKey); ok {
		var cached cachedHistory
		if err := json.Unmarshal([]byte(data), &cached); err == nil {
			if time.Since(cached.UpdatedAt) < 7*24*time.Hour {
				log.Printf("[SOLACTIVE_PROVIDER] Cache hit for %s", cacheKey)
				return cached.Bars, nil
			}
		}
	}

	log.Printf("[SOLACTIVE_PROVIDER] Cache miss for %s", cacheKey)

	reqBody := solactiveRequest{
		ISIN:                   historicalTicker,
		IndexCreatingTimeStamp: 0,
		DayDate:                time.Now().UnixMilli(),
	}

	jsonBody, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", "https://www.solactive.com/_actions/getDayHistoryChartData/", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("solactive API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data []interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	// Skip first element if it's an array (indices)
	startIdx := 0
	if len(data) > 0 {
		if _, ok := data[0].([]interface{}); ok {
			startIdx = 1
		}
	}

	// Intermediate map to store daily values for anchoring and FX
	dailyValues := make(map[string]float64)
	var dates []time.Time
	var lastDate time.Time

	for i := startIdx; i+1 < len(data); i += 3 {
		tsFloat, ok1 := data[i].(float64)
		valStr, ok2 := data[i+1].(string)
		if ok1 && ok2 {
			ts := int64(tsFloat)
			val, _ := strconv.ParseFloat(valStr, 64)
			date := time.UnixMilli(ts).UTC()
			dateStr := date.Format("2006-01-02")
			
			// Only keep the first value for each day if there are multiples
			if _, exists := dailyValues[dateStr]; !exists {
				dailyValues[dateStr] = val
				dates = append(dates, date)
			}
			
			if date.After(lastDate) {
				lastDate = date
			}
		}
	}

	if len(dailyValues) == 0 {
		return nil, fmt.Errorf("no data returned from solactive for %s", historicalTicker)
	}

	fxMap, _ := fetchDailyFX(t.ConversionTracker)

	// Anchoring Logic
	scaleMultiplier := 1.0
	if t.Tracker != "" && t.Tracker != historicalTicker {
		params := models.HistoryParams{Interval: "1wk", Period: "max"}
		at, _ := ticker.New(t.Tracker)
		if at != nil {
			defer at.Close()
			aBars, err := at.History(params)
			if err == nil && len(aBars) > 0 {
				anchorFound := false
				for i := len(aBars) - 1; i >= 0; i-- {
					etfClose := aBars[i].AdjClose
					if etfClose <= 0 {
						continue
					}

					if indexClose, ok := findInWindow(dailyValues, aBars[i].Date, 7); ok && indexClose > 0 {
						fxRate := 1.0
						if t.ConversionTracker != "" {
							if rate, ok := findInWindow(fxMap, aBars[i].Date, 7); ok {
								fxRate = rate
							} else {
								continue
							}
						}

						indexInEUR := indexClose / fxRate
						scaleMultiplier = etfClose / indexInEUR
						log.Printf("[SOLACTIVE_PROVIDER] Anchored scale for %s: %e (ETF: %.4f, IndexInEUR: %.4f)", t.Tracker, scaleMultiplier, etfClose, indexInEUR)
						anchorFound = true
						break
					}
				}
				if !anchorFound {
					log.Printf("[SOLACTIVE_PROVIDER] WARNING: No anchor point found for %s", t.Tracker)
				}
			}
		}
	}

	// Build Weekly History Slice
	var history []models.Bar
	log.Printf("[SOLACTIVE_PROVIDER] Building weekly history slice from %d daily values. Scale: %e", len(dailyValues), scaleMultiplier)

	if len(dates) == 0 {
		return history, nil
	}

	sort.Slice(dates, func(i, j int) bool {
		return dates[i].Before(dates[j])
	})

	minDate := dates[0]
	maxDate := dates[len(dates)-1]

	curr := minDate
	lastPrice := 0.0

	for !curr.After(maxDate) {
		isoYear, isoWeek := curr.ISOWeek()
		weekStart := isoWeekToDate(isoYear, isoWeek)
		
		for i := 6; i >= 0; i-- {
			checkDate := weekStart.AddDate(0, 0, i)
			if val, ok := dailyValues[checkDate.Format("2006-01-02")]; ok {
				fxRate := 1.0
				if t.ConversionTracker != "" {
					if rate, ok := findInWindow(fxMap, checkDate, 7); ok {
						fxRate = rate
					} else {
						continue // If no FX rate, we don't update lastPrice, keep previous
					}
				}
				lastPrice = (val / fxRate) * scaleMultiplier
				break
			}
		}

		if lastPrice > 0 {
			history = append(history, models.Bar{
				Date:     weekStart,
				AdjClose: lastPrice,
			})
		}

		curr = curr.AddDate(0, 0, 7)
	}

	cached := cachedHistory{
		Bars:      history,
		UpdatedAt: time.Now(),
	}
	cacheBytes, _ := json.Marshal(cached)
	p.cacheRepo.Set(cacheKey, string(cacheBytes))

	return history, nil
}

type MSCIHistoryProvider struct {
	cacheRepo *repository.CacheRepository
}

func NewMSCIHistoryProvider(cache *repository.CacheRepository) *MSCIHistoryProvider {
	return &MSCIHistoryProvider{cacheRepo: cache}
}

type msciPerformanceItem struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

type msciIndexData struct {
	IndexCode          string                `json:"indexCode"`
	PerformanceHistory []msciPerformanceItem `json:"performanceHistory"`
}

type msciResponseData struct {
	Indexes []msciIndexData `json:"indexes"`
}

type msciResponse struct {
	Data msciResponseData `json:"data"`
}

func (p *MSCIHistoryProvider) GetHistory(t domain.ETFTracker) ([]models.Bar, error) {
	historicalTicker := t.HistoricalTracker
	if historicalTicker == "" {
		historicalTicker = t.Tracker
	}

	cacheKey := getHistoryCacheKey("msci_history", historicalTicker, t.ConversionTracker)
	
	if data, ok, _ := p.cacheRepo.Get(cacheKey); ok {
		var cached cachedHistory
		if err := json.Unmarshal([]byte(data), &cached); err == nil {
			if time.Since(cached.UpdatedAt) < 7*24*time.Hour {
				log.Printf("[MSCI_PROVIDER] Cache hit for %s", cacheKey)
				return cached.Bars, nil
			}
		}
	}

	log.Printf("[MSCI_PROVIDER] Cache miss for %s", cacheKey)

	url := fmt.Sprintf("https://www.msci.com/indexes/api/index/performance?indexCode=%s&currency=USD&variant=NETR&frequency=daily&baseValue100=false&startDate=1998-12-31&endDate=%s", historicalTicker, time.Now().Format("2006-01-02"))
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/148.0.0.0 Safari/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MSCI API returned status %d", resp.StatusCode)
	}

	var msciResp msciResponse
	if err := json.NewDecoder(resp.Body).Decode(&msciResp); err != nil {
		return nil, err
	}

	if len(msciResp.Data.Indexes) == 0 || len(msciResp.Data.Indexes[0].PerformanceHistory) == 0 {
		return nil, fmt.Errorf("no data returned from MSCI for %s", historicalTicker)
	}

	dailyValues := make(map[string]float64)
	var dates []time.Time
	var lastDate time.Time

	for _, item := range msciResp.Data.Indexes[0].PerformanceHistory {
		date, err := time.Parse("2006-01-02", item.Date)
		if err != nil {
			continue
		}
		dateStr := date.Format("2006-01-02")
		
		if _, exists := dailyValues[dateStr]; !exists {
			dailyValues[dateStr] = item.Value
			dates = append(dates, date)
		}
		
		if date.After(lastDate) {
			lastDate = date
		}
	}

	if len(dailyValues) == 0 {
		return nil, fmt.Errorf("no valid data parsed from MSCI for %s", historicalTicker)
	}

	fxMap, _ := fetchDailyFX(t.ConversionTracker)

	// Build Weekly History Slice
	var history []models.Bar
	log.Printf("[MSCI_PROVIDER] Building weekly history slice from %d daily values.", len(dailyValues))

	if len(dates) == 0 {
		return history, nil
	}

	sort.Slice(dates, func(i, j int) bool {
		return dates[i].Before(dates[j])
	})

	minDate := dates[0]
	maxDate := dates[len(dates)-1]

	curr := minDate
	lastPrice := 0.0

	for !curr.After(maxDate) {
		isoYear, isoWeek := curr.ISOWeek()
		weekStart := isoWeekToDate(isoYear, isoWeek)
		
		for i := 6; i >= 0; i-- {
			checkDate := weekStart.AddDate(0, 0, i)
			if val, ok := dailyValues[checkDate.Format("2006-01-02")]; ok {
				fxRate := 1.0
				if t.ConversionTracker != "" {
					if rate, ok := findInWindow(fxMap, checkDate, 7); ok {
						fxRate = rate
					} else {
						continue // If no FX rate, we don't update lastPrice, keep previous
					}
				}
				lastPrice = (val / fxRate)
				break
			}
		}

		if lastPrice > 0 {
			history = append(history, models.Bar{
				Date:     weekStart,
				AdjClose: lastPrice,
			})
		}

		curr = curr.AddDate(0, 0, 7)
	}

	cached2 := cachedHistory{
		Bars:      history,
		UpdatedAt: time.Now(),
	}
	cacheBytes2, _ := json.Marshal(cached2)
	p.cacheRepo.Set(cacheKey, string(cacheBytes2))

	return history, nil
}
