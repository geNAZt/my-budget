package service

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"

	"github.com/genazt/my-budget-script/web/backend/internal/repository"
)

type MarketDataService struct {
	cacheRepo *repository.CacheRepository
	apiKey    string
}

func NewMarketDataService(cache *repository.CacheRepository) *MarketDataService {
	return &MarketDataService{
		cacheRepo: cache,
		apiKey:    "PR7DAI0RVKGOQ3U2", // From GAS implementation
	}
}

func (s *MarketDataService) GetCachedReturns(ticker string) ([]float64, bool) {
	cacheKey := fmt.Sprintf("returns:%s", ticker)
	if data, ok, _ := s.cacheRepo.Get(cacheKey); ok {
		var returns []float64
		if err := json.Unmarshal([]byte(data), &returns); err == nil {
			return returns, true
		}
	}
	return nil, false
}

func (s *MarketDataService) tryLoadFromLocalIndex(ticker string) ([]float64, error) {
	path := fmt.Sprintf("index/%s.csv", ticker)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	// Skip header
	_, err = reader.Read()
	if err != nil {
		return nil, err
	}

	var returns []float64
	var prevValue float64

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if len(record) < 2 {
			continue
		}

		value, err := strconv.ParseFloat(record[1], 64)
		if err != nil {
			continue
		}

		if prevValue > 0 {
			// Calculate monthly return
			monthlyReturn := (value - prevValue) / prevValue

			// Convert monthly return to weekly return
			// (1 + wr)^4.33 = (1 + mr)  => 1 + wr = (1 + mr)^(1/4.33)
			weeklyReturn := math.Pow(1+monthlyReturn, 1.0/4.3333) - 1

			// Append 4.33 equivalent weekly returns to the pool
			// We can append 4 returns per month to keep it close to weekly frequency
			for i := 0; i < 4; i++ {
				returns = append(returns, weeklyReturn)
			}
		}
		prevValue = value
	}

	return returns, nil
}

func (s *MarketDataService) GetHistoricalWeeklyReturns(ticker string) ([]float64, error) {
	// 1. Try Local Index
	if returns, err := s.tryLoadFromLocalIndex(ticker); err == nil {
		return returns, nil
	}

	cacheKey := fmt.Sprintf("returns:%s", ticker)
	if data, ok, _ := s.cacheRepo.Get(cacheKey); ok {
		var returns []float64
		if err := json.Unmarshal([]byte(data), &returns); err == nil {
			return returns, nil
		}
	}

	// Fetch from Alpha Vantage
	url := fmt.Sprintf("https://www.alphavantage.co/query?function=TIME_SERIES_WEEKLY_ADJUSTED&symbol=%s&apikey=%s", ticker, s.apiKey)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var avResp map[string]interface{}
	json.Unmarshal(body, &avResp)

	// Rate limit check
	if _, ok := avResp["Note"]; ok {
		return nil, fmt.Errorf("alpha vantage rate limit exceeded")
	}

	weeklyData, ok := avResp["Weekly Adjusted Time Series"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no weekly data found for ticker %s", ticker)
	}

	// Extract prices and calculate returns
	var dates []string
	for date := range weeklyData {
		dates = append(dates, date)
	}
	sort.Strings(dates) // Oldest first

	var returns []float64
	var prevPrice float64
	for _, date := range dates {
		node := weeklyData[date].(map[string]interface{})
		priceStr := node["5. adjusted close"].(string)
		price, _ := strconv.ParseFloat(priceStr, 64)

		if prevPrice > 0 {
			ret := (price - prevPrice) / prevPrice
			returns = append(returns, ret)
		}
		prevPrice = price
	}

	// Cache result
	jsonBytes, _ := json.Marshal(returns)
	s.cacheRepo.Set(cacheKey, string(jsonBytes))

	return returns, nil
}
