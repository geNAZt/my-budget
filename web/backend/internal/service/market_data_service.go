package service

import (
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	"github.com/wnjoon/go-yfinance/pkg/models"
)

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
	return nil, false
}

func isoWeekToDate(year, week int) time.Time {
	// Start with Jan 4 of the year (which is always in ISO week 1)
	t := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
	// Find Monday of that week
	daysToMonday := int(t.Weekday()) - 1
	if daysToMonday < 0 {
		daysToMonday = 6
	}
	monday := t.AddDate(0, 0, -daysToMonday)
	// Add weeks
	return monday.AddDate(0, 0, (week-1)*7)
}

func (s *MarketDataService) GetHistoricalWeeklyReturns(t domain.ETFTracker) (map[string]float64, error) {
	historicalTicker := t.HistoricalTracker
	if historicalTicker == "" {
		historicalTicker = t.Tracker
	}

	log.Printf("[MARKET_DATA] Fetching historical weekly returns for %s (provider: %s, conv: %s, anchor: %s)", historicalTicker, t.HistoryProvider, t.ConversionTracker, t.Tracker)

	// 2. Select Provider
	var provider HistoryProvider
	switch strings.ToLower(t.HistoryProvider) {
	case "solactive":
		provider = NewSolactiveHistoryProvider(s.cacheRepo)
	case "msci":
		provider = NewMSCIHistoryProvider(s.cacheRepo)
	default:
		provider = NewYahooHistoryProvider(s.cacheRepo)
	}

	// 3. Fetch History
	historyBars, err := provider.GetHistory(t)
	if err != nil {
		return nil, err
	}

	if len(historyBars) < 2 {
		return nil, fmt.Errorf("not enough data points from provider %s", t.HistoryProvider)
	}

	log.Printf("[MARKET_DATA] Processing %d chronological bars from provider", len(historyBars))

	// 4. Calculate returns (Newest First) and detect gaps
	return calculateReturnsFromBars(historyBars, t.Tracker), nil
}

func calculateReturnsFromBars(historyBars []models.Bar, trackerName string) map[string]float64 {
	returns := make(map[string]float64)
	for i := len(historyBars) - 1; i > 0; i-- {
		newerBar := historyBars[i]
		olderBar := historyBars[i-1]
		
		newerYear, newerWeek := newerBar.Date.ISOWeek()
		olderYear, olderWeek := olderBar.Date.ISOWeek()

		newerISOStr := fmt.Sprintf("%04d-W%02d", newerYear, newerWeek)
		
		if olderBar.AdjClose <= 0 {
			continue
		}

		// Calculate total return over the period
		totalReturn := (newerBar.AdjClose - olderBar.AdjClose) / olderBar.AdjClose
		
		// Gap detection: Calculate how many weeks passed
		// A simple way is to divide duration by 7 days.
		// Since we standardized dates to Monday in the providers, we can use exact duration.
		duration := newerBar.Date.Sub(olderBar.Date)
		weeksPassed := int(math.Round(duration.Hours() / 24.0 / 7.0))
		
		if weeksPassed > 1 {
			log.Printf("[MARKET_DATA] Data gap detected for %s. %d weeks between %d/%d and %d/%d", 
				trackerName, weeksPassed, olderWeek, olderYear, newerWeek, newerYear)
			
			// Geometric interpolation
			// (1 + r)^weeksPassed = 1 + totalReturn
			// r = (1 + totalReturn)^(1/weeksPassed) - 1
			weeklyReturn := math.Pow(1.0+totalReturn, 1.0/float64(weeksPassed)) - 1.0
			
			// Fill the gap
			currDate := newerBar.Date
			for w := 0; w < weeksPassed; w++ {
				y, wk := currDate.ISOWeek()
				isoStr := fmt.Sprintf("%04d-W%02d", y, wk)
				returns[isoStr] = weeklyReturn
				currDate = currDate.AddDate(0, 0, -7)
			}
		} else if weeksPassed == 1 {
			returns[newerISOStr] = totalReturn
		} else {
			// Zero weeks passed, shouldn't happen with proper weekly data, but just in case
			log.Printf("[MARKET_DATA] WARNING: 0 weeks passed between consecutive data points for %s", trackerName)
		}
	}

	return returns
}
