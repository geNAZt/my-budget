package service

import (
	"fmt"
	"log"
	"math"
	"sort"
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
	if len(t.StitchingSegments) > 0 {
		return s.getStitchedWeeklyReturns(t)
	}

	historicalTicker := t.HistoricalTracker
	if historicalTicker == "" {
		historicalTicker = t.Tracker
	}

	log.Printf("[MARKET_DATA] Fetching historical weekly returns for %s (provider: %s, conv: %s, anchor: %s)", 
		historicalTicker, t.HistoryProvider, t.ConversionTracker, t.Tracker)

	// 2. Select Provider
	var provider HistoryProvider
	switch strings.ToLower(t.HistoryProvider) {
	case "solactive":
		provider = NewSolactiveHistoryProvider(s.cacheRepo)
	case "msci":
		provider = NewMSCIHistoryProvider(s.cacheRepo)
	case "justetf":
		provider = NewJustETFHistoryProvider(s.cacheRepo)
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

func (s *MarketDataService) getStitchedWeeklyReturns(t domain.ETFTracker) (map[string]float64, error) {
	log.Printf("[MARKET_DATA] Stitching history for %s with %d segments", t.Tracker, len(t.StitchingSegments))

	// Prepend the main historical tracker as the first segment if it exists
	var allSegments []domain.HistoryStitchingSegment
	seenTickers := make(map[string]bool)

	if t.HistoricalTracker != "" {
		allSegments = append(allSegments, domain.HistoryStitchingSegment{
			Provider:          t.HistoryProvider,
			LookupTicker:      t.HistoricalTracker,
			ConversionTracker: t.ConversionTracker,
		})
		seenTickers[t.HistoricalTracker] = true
	}

	for _, seg := range t.StitchingSegments {
		if !seenTickers[seg.LookupTicker] {
			allSegments = append(allSegments, seg)
			seenTickers[seg.LookupTicker] = true
		}
	}

	var barsList [][]models.Bar
	var fetchedSegments []domain.HistoryStitchingSegment
	for _, seg := range allSegments {
		// Construct a temporary ETFTracker for this segment
		conversionTracker := seg.ConversionTracker
		if conversionTracker == "" {
			conversionTracker = t.ConversionTracker
		}

		segTracker := domain.ETFTracker{
			Tracker:           t.Tracker, // Keep the same base ETF
			HistoricalTracker: seg.LookupTicker,
			ConversionTracker: conversionTracker,
			HistoryProvider:   seg.Provider,
		}

		var provider HistoryProvider
		switch strings.ToLower(seg.Provider) {
		case "solactive":
			provider = NewSolactiveHistoryProvider(s.cacheRepo)
		case "msci":
			provider = NewMSCIHistoryProvider(s.cacheRepo)
		case "justetf":
			provider = NewJustETFHistoryProvider(s.cacheRepo)
		default:
			provider = NewYahooHistoryProvider(s.cacheRepo)
		}

		bars, err := provider.GetHistory(segTracker)
		if err != nil {
			log.Printf("[MARKET_DATA] WARNING: Failed to fetch history for segment (%s, %s): %v", seg.Provider, seg.LookupTicker, err)
			continue
		}
		if len(bars) > 0 {
			barsList = append(barsList, bars)
			fetchedSegments = append(fetchedSegments, seg)
		}
	}

	if len(barsList) == 0 {
		return nil, fmt.Errorf("no history data available for any stitching segments of %s", t.Tracker)
	}

	// Stitch bars list chronologically
	// barsList[0] is the primary (newest/active ETF)
	stitchedBars := barsList[0]

	for segmentIdx := 1; segmentIdx < len(barsList); segmentIdx++ {
		nextBars := barsList[segmentIdx]
		if len(stitchedBars) == 0 {
			stitchedBars = nextBars
			continue
		}
		if len(nextBars) == 0 {
			continue
		}

		// Find earliest date in stitchedBars
		earliestStitched := stitchedBars[0].Date
		for _, b := range stitchedBars {
			if b.Date.Before(earliestStitched) {
				earliestStitched = b.Date
			}
		}

		// Find the closest overlapping date to calculate scaling multiplier
		// We want to anchor nextBars to stitchedBars at the overlap.
		var overlapStitched models.Bar
		var overlapNext models.Bar
		foundOverlap := false

		// Map stitched dates for fast lookup
		stitchedMap := make(map[time.Time]models.Bar)
		for _, b := range stitchedBars {
			stitchedMap[b.Date] = b
		}

		// Look for the oldest date in nextBars that overlaps with stitchedBars
		// We sort nextBars ascending to find the oldest overlapping point first
		sort.Slice(nextBars, func(i, j int) bool {
			return nextBars[i].Date.Before(nextBars[j].Date)
		})

		for _, b := range nextBars {
			if sb, ok := stitchedMap[b.Date]; ok && sb.AdjClose > 0 && b.AdjClose > 0 {
				overlapStitched = sb
				overlapNext = b
				foundOverlap = true
				break
			}
		}

		scaleMultiplier := 1.0
		if foundOverlap {
			scaleMultiplier = overlapStitched.AdjClose / overlapNext.AdjClose
			log.Printf("[MARKET_DATA] Anchoring segment %d (%s) to stitched history. Overlap date: %s. Stitched price: %.4f, Segment price: %.4f, Scale: %e",
				segmentIdx, fetchedSegments[segmentIdx].LookupTicker, overlapNext.Date.Format("2006-01-02"), overlapStitched.AdjClose, overlapNext.AdjClose, scaleMultiplier)
		} else {
			// No overlap (gap between series). Use the last price of nextBars and first price of stitchedBars
			var oldestStitched models.Bar
			hasOldest := false
			for _, b := range stitchedBars {
				if !hasOldest || b.Date.Before(oldestStitched.Date) {
					oldestStitched = b
					hasOldest = true
				}
			}

			if hasOldest && len(nextBars) > 0 && nextBars[0].AdjClose > 0 && oldestStitched.AdjClose > 0 {
				scaleMultiplier = oldestStitched.AdjClose / nextBars[0].AdjClose
				log.Printf("[MARKET_DATA] WARNING: No overlapping dates found for segment %d (%s). Anchoring via gap boundary. Oldest stitched: %s (price %.4f), Newest segment: %s (price %.4f), Scale: %e",
					segmentIdx, fetchedSegments[segmentIdx].LookupTicker, oldestStitched.Date.Format("2006-01-02"), oldestStitched.AdjClose, nextBars[0].Date.Format("2006-01-02"), nextBars[0].AdjClose, scaleMultiplier)
			}
		}

		// Keep only bars from nextBars that are strictly before earliestStitched
		var backfillBars []models.Bar
		for _, b := range nextBars {
			if b.Date.Before(earliestStitched) {
				// Scale the bar AdjClose
				b.AdjClose = b.AdjClose * scaleMultiplier
				backfillBars = append(backfillBars, b)
			}
		}

		// Sort backfillBars chronologically (ascending)
		sort.Slice(backfillBars, func(i, j int) bool {
			return backfillBars[i].Date.Before(backfillBars[j].Date)
		})

		// Prepend backfillBars to stitchedBars
		stitchedBars = append(backfillBars, stitchedBars...)
	}

	// Sort final stitched history chronologically
	sort.Slice(stitchedBars, func(i, j int) bool {
		return stitchedBars[i].Date.Before(stitchedBars[j].Date)
	})

	if len(stitchedBars) < 2 {
		return nil, fmt.Errorf("not enough stitched data points for %s", t.Tracker)
	}

	log.Printf("[MARKET_DATA] Successfully stitched %d segments into %d weekly bars for %s", len(fetchedSegments), len(stitchedBars), t.Tracker)

	// Calculate returns
	return calculateReturnsFromBars(stitchedBars, t.Tracker), nil
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
