package handler

import (
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/genazt/my-budget-script/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/backend/internal/domain"
	"github.com/genazt/my-budget-script/backend/internal/repository"
	"github.com/genazt/my-budget-script/backend/internal/service"
)

// Assets handles websocket requests for the "assets" namespace.
// The reflection engine will register its methods automatically as "assets::method_name".
type Assets struct {
	handler      *api.WebSocketHandler
	repo         *repository.AssetRepository
	scenarioRepo *repository.ScenarioRepository
	marketData   *service.MarketDataService
}

// NewAssets creates a new instance of the Assets API handler namespace
func NewAssets(handler *api.WebSocketHandler, repo *repository.AssetRepository, scenarioRepo *repository.ScenarioRepository, marketData *service.MarketDataService) *Assets {
	return &Assets{
		handler:      handler,
		repo:         repo,
		scenarioRepo: scenarioRepo,
		marketData:   marketData,
	}
}

// List maps to route "assets::list"
func (a *Assets) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	assets, err := a.repo.List(userID)
	if err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.AssetList{}
	for _, asset := range assets {
		resp.Assets = append(resp.Assets, mapAssetToProto(asset))
	}

	a.handler.SendResponse(s, reqID, resp, true)
}

func mapProtoToAssetVersion(reqVersion *apiproto.AssetVersion) *domain.AssetVersion {
	if reqVersion == nil {
		return nil
	}

	av := &domain.AssetVersion{
		ID:               reqVersion.Id,
		AssetID:          reqVersion.AssetId,
		Type:             domain.AssetType(reqVersion.Type),
		TargetValue:      strconv.FormatFloat(reqVersion.TargetValue, 'f', -1, 64),
		InterestRate:     reqVersion.InterestRate,
		InterestInterval: reqVersion.InterestInterval,
		AmountPerMonth:   reqVersion.AmountPerMonth,
	}

	if av.Type == domain.AssetTypeETF {
		av.InterestRate = 0
	}

	if reqVersion.DumpingLoanId != "" {
		av.DumpingLoanID = &reqVersion.DumpingLoanId
	}

	if reqVersion.StopModificationId != "" {
		av.StopModificationID = &reqVersion.StopModificationId
	}

	av.UseForPassiveIncome = reqVersion.UseForPassiveIncome
	av.TaxAllowance = reqVersion.TaxAllowance

	if reqVersion.TaxAllowanceStartDate != "" {
		if t, err := time.Parse(time.RFC3339, reqVersion.TaxAllowanceStartDate); err == nil {
			av.TaxAllowanceStartDate = &t
		}
	}

	if reqVersion.TaxAllowanceEndDate != "" {
		if t, err := time.Parse(time.RFC3339, reqVersion.TaxAllowanceEndDate); err == nil {
			av.TaxAllowanceEndDate = &t
		}
	}

	if reqVersion.RemainderStartDate != "" {
		if t, err := time.Parse(time.RFC3339, reqVersion.RemainderStartDate); err == nil {
			av.RemainderStartDate = &t
		}
	}

	if reqVersion.StartDate != "" {
		if t, err := time.Parse(time.RFC3339, reqVersion.StartDate); err == nil {
			av.StartDate = t
		}
	}

	if reqVersion.EndDate != "" {
		if t, err := time.Parse(time.RFC3339, reqVersion.EndDate); err == nil {
			av.EndDate = &t
		}
	}

	for _, tracker := range reqVersion.EtfConfig {
		var segments []domain.HistoryStitchingSegment
		for _, seg := range tracker.StitchingSegments {
			segments = append(segments, domain.HistoryStitchingSegment{
				Provider:          seg.Provider,
				LookupTicker:      seg.LookupTicker,
				ConversionTracker: seg.ConversionTracker,
			})
		}
		av.ETFConfig = append(av.ETFConfig, domain.ETFTracker{
			Tracker:           tracker.Tracker,
			HistoricalTracker: tracker.HistoricalTracker,
			ConversionTracker: tracker.ConversionTracker,
			HistoryProvider:   tracker.HistoryProvider,
			Percentage:        tracker.Percentage,
			TER:               tracker.Ter,
			StitchingSegments: segments,
		})
	}

	for _, penalty := range reqVersion.Penalties {
		av.Penalties = append(av.Penalties, domain.AssetPenalty{
			Name:        penalty.Name,
			TriggerType: domain.PenaltyTriggerType(penalty.TriggerType),
			Percentage:  penalty.Percentage,
		})
	}

	for _, ta := range reqVersion.TaxAllowances {
		item := domain.AssetTaxAllowance{
			ID:     ta.Id,
			Amount: ta.Amount,
		}
		if ta.StartDate != "" {
			if t, err := time.Parse(time.RFC3339, ta.StartDate); err == nil {
				item.StartDate = &t
			}
		}
		if ta.EndDate != "" {
			if t, err := time.Parse(time.RFC3339, ta.EndDate); err == nil {
				item.EndDate = &t
			}
		}
		av.TaxAllowances = append(av.TaxAllowances, item)
	}

	for _, subAsset := range reqVersion.SubAssets {
		sa := domain.SubAsset{
			ID:                  subAsset.Id,
			Name:                subAsset.Name,
			TargetValue:         strconv.FormatFloat(subAsset.TargetValue, 'f', -1, 64),
			AmountPerMonth:      subAsset.AmountPerMonth,
			IsRemainderConsumer: subAsset.IsRemainderConsumer,
			RemainderPriority:   subAsset.RemainderPriority,
		}

		if subAsset.RemainderStartDate != "" {
			if t, err := time.Parse(time.RFC3339, subAsset.RemainderStartDate); err == nil {
				sa.RemainderStartDate = &t
			}
		}

		if subAsset.DumpingLoanId != "" {
			sa.DumpingLoanID = &subAsset.DumpingLoanId
		}

		if subAsset.ExpenseId != "" {
			sa.ExpenseID = &subAsset.ExpenseId
		}

		if subAsset.StartDate != "" {
			if t, err := time.Parse(time.RFC3339, subAsset.StartDate); err == nil {
				sa.StartDate = t
			}
		}

		if subAsset.EndDate != "" {
			if t, err := time.Parse(time.RFC3339, subAsset.EndDate); err == nil {
				sa.EndDate = &t
			}
		}

		if subAsset.EarliestDumpDate != "" {
			if t, err := time.Parse(time.RFC3339, subAsset.EarliestDumpDate); err == nil {
				sa.EarliestDumpDate = &t
			}
		}

		av.SubAssets = append(av.SubAssets, sa)
	}

	return av
}

// Save maps to route "assets::save"
func (a *Assets) Save(s *api.WebsocketSession, reqID string, reqAsset *apiproto.Asset) {
	userID, _ := s.GetAuth()
	if userID == "" {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	domainAsset := domain.Asset{
		ID:              reqAsset.Id,
		UserID:          userID,
		Name:            reqAsset.Name,
		AccountIDs:      reqAsset.AccountIds,
		LinkToScenarios: reqAsset.LinkToScenarios,
		ActiveVersion:   mapProtoToAssetVersion(reqAsset.ActiveVersion),
	}
	if reqAsset.PoolId != "" {
		domainAsset.PoolID = &reqAsset.PoolId
	}

	if err := a.repo.Save(userID, &domainAsset); err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	if len(domainAsset.LinkToScenarios) > 0 {
		a.scenarioRepo.LinkEntityToScenarios(userID, domainAsset.ID, "ASSET", domainAsset.LinkToScenarios)
	}

	a.handler.SendResponse(s, reqID, mapAssetToProto(domainAsset), true)
}

// Delete maps to route "assets::delete"
func (a *Assets) Delete(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := a.repo.ArchiveFull(userID, reqIDObj.Id); err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	a.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

// Helper mapper function kept identical
func mapAssetToProto(a domain.Asset) *apiproto.Asset {
	pa := &apiproto.Asset{
		Id:              a.ID,
		UserId:          a.UserID,
		Name:            a.Name,
		IsDeleted:       a.IsDeleted,
		AccountIds:      a.AccountIDs,
		CreatedAt:       a.CreatedAt.Format(time.RFC3339),
		LinkToScenarios: a.LinkToScenarios,
	}

	if a.PoolID != nil {
		pa.PoolId = *a.PoolID
	}

	if a.ActiveVersion != nil {
		targetValue, _ := strconv.ParseFloat(a.ActiveVersion.TargetValue, 64)
		pa.ActiveVersion = &apiproto.AssetVersion{
			Id:                 a.ActiveVersion.ID,
			AssetId:            a.ActiveVersion.AssetID,
			Type:               string(a.ActiveVersion.Type),
			TargetValue:        targetValue,
			DumpingLoanId:      "",
			StopModificationId:  "",
			InterestRate:        a.ActiveVersion.InterestRate,
			InterestInterval:    a.ActiveVersion.InterestInterval,
			AmountPerMonth:      a.ActiveVersion.AmountPerMonth,
			CreatedAt:           a.ActiveVersion.CreatedAt.Format(time.RFC3339),
			StartDate:           a.ActiveVersion.StartDate.Format(time.RFC3339),
			UseForPassiveIncome: a.ActiveVersion.UseForPassiveIncome,
			TaxAllowance:        a.ActiveVersion.TaxAllowance,
		}

		if a.ActiveVersion.DumpingLoanID != nil {
			pa.ActiveVersion.DumpingLoanId = *a.ActiveVersion.DumpingLoanID
		}

		if a.ActiveVersion.StopModificationID != nil {
			pa.ActiveVersion.StopModificationId = *a.ActiveVersion.StopModificationID
		}

		if a.ActiveVersion.RemainderStartDate != nil {
			pa.ActiveVersion.RemainderStartDate = a.ActiveVersion.RemainderStartDate.Format(time.RFC3339)
		}

		if a.ActiveVersion.EndDate != nil {
			pa.ActiveVersion.EndDate = a.ActiveVersion.EndDate.Format(time.RFC3339)
		}

		if a.ActiveVersion.TaxAllowanceStartDate != nil {
			pa.ActiveVersion.TaxAllowanceStartDate = a.ActiveVersion.TaxAllowanceStartDate.Format(time.RFC3339)
		}

		if a.ActiveVersion.TaxAllowanceEndDate != nil {
			pa.ActiveVersion.TaxAllowanceEndDate = a.ActiveVersion.TaxAllowanceEndDate.Format(time.RFC3339)
		}

		for _, tracker := range a.ActiveVersion.ETFConfig {
			var segments []*apiproto.HistoryStitchingSegment
			for _, seg := range tracker.StitchingSegments {
				segments = append(segments, &apiproto.HistoryStitchingSegment{
					Provider:          seg.Provider,
					LookupTicker:      seg.LookupTicker,
					ConversionTracker: seg.ConversionTracker,
				})
			}
			pa.ActiveVersion.EtfConfig = append(pa.ActiveVersion.EtfConfig, &apiproto.ETFTracker{
				Tracker:           tracker.Tracker,
				HistoricalTracker: tracker.HistoricalTracker,
				ConversionTracker: tracker.ConversionTracker,
				HistoryProvider:   tracker.HistoryProvider,
				Percentage:        tracker.Percentage,
				Ter:               tracker.TER,
				StitchingSegments: segments,
			})
		}

		for _, penalty := range a.ActiveVersion.Penalties {
			pa.ActiveVersion.Penalties = append(pa.ActiveVersion.Penalties, &apiproto.AssetPenalty{
				Name:        penalty.Name,
				TriggerType: string(penalty.TriggerType),
				Percentage:  penalty.Percentage,
			})
		}

		for _, ta := range a.ActiveVersion.TaxAllowances {
			item := &apiproto.TaxAllowance{
				Id:     ta.ID,
				Amount: ta.Amount,
			}
			if ta.StartDate != nil {
				item.StartDate = ta.StartDate.Format(time.RFC3339)
			}
			if ta.EndDate != nil {
				item.EndDate = ta.EndDate.Format(time.RFC3339)
			}
			pa.ActiveVersion.TaxAllowances = append(pa.ActiveVersion.TaxAllowances, item)
		}

		for _, subAsset := range a.ActiveVersion.SubAssets {
			saTargetValue, _ := strconv.ParseFloat(subAsset.TargetValue, 64)
			psa := &apiproto.SubAsset{
				Id:                  subAsset.ID,
				Name:                subAsset.Name,
				TargetValue:         saTargetValue,
				AmountPerMonth:      subAsset.AmountPerMonth,
				IsRemainderConsumer: subAsset.IsRemainderConsumer,
				RemainderPriority:   subAsset.RemainderPriority,
				DumpingLoanId:       "",
				StartDate:           subAsset.StartDate.Format(time.RFC3339),
				EndDate:             "",
				EarliestDumpDate:    "",
			}
			if subAsset.RemainderStartDate != nil {
				psa.RemainderStartDate = subAsset.RemainderStartDate.Format(time.RFC3339)
			}
			if subAsset.DumpingLoanID != nil {
				psa.DumpingLoanId = *subAsset.DumpingLoanID
			}
			if subAsset.ExpenseID != nil {
				psa.ExpenseId = *subAsset.ExpenseID
			}
			if subAsset.EndDate != nil {
				psa.EndDate = subAsset.EndDate.Format(time.RFC3339)
			}
			if subAsset.EarliestDumpDate != nil {
				psa.EarliestDumpDate = subAsset.EarliestDumpDate.Format(time.RFC3339)
			}
			pa.ActiveVersion.SubAssets = append(pa.ActiveVersion.SubAssets, psa)
		}
	}

	return pa
}

// GetTrackerCharts maps to route "assets::gettrackercharts"
func (a *Assets) GetTrackerCharts(s *api.WebsocketSession, reqID string, body *apiproto.GetTrackerChartsRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	assets, err := a.repo.List(userID)
	if err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	rangeStr := "max"
	if body != nil && body.Range != "" {
		rangeStr = body.Range
	}

	resp := &apiproto.TrackerChartsResponse{}

	// Avoid duplicate tracker fetching
	processedTrackers := make(map[string]bool)

	for _, asset := range assets {
		if asset.ActiveVersion == nil || asset.ActiveVersion.Type != domain.AssetTypeETF {
			continue
		}

		for _, t := range asset.ActiveVersion.ETFConfig {
			trackerKey := t.Tracker
			if processedTrackers[trackerKey] {
				continue
			}
			processedTrackers[trackerKey] = true

			if rangeStr == "max" {
				// Fetch history returns
				returns, err := a.marketData.GetHistoricalWeeklyReturns(t)
				if err != nil {
					log.Printf("[ASSETS] Failed to get historical weekly returns for tracker %s: %v", t.Tracker, err)
					continue
				}

				if len(returns) == 0 {
					continue
				}

				// Sort weeks chronologically
				var weeks []string
				for wk := range returns {
					weeks = append(weeks, wk)
				}
				sort.Strings(weeks)

				// Construct cumulative performance series starting at 100.0
				chartPoints := make([]*apiproto.TrackerChartPoint, 0, len(weeks)+1)

				// Base starting point
				// Let's find the first week and determine its preceding monday to represent the start
				var baseDate time.Time
				hasBaseDate := false
				if len(weeks) > 0 {
					firstWeek := weeks[0]
					var year, wkNum int
					_, err := fmt.Sscanf(firstWeek, "%d-W%d", &year, &wkNum)
					if err == nil {
						baseDate = isoWeekToDate(year, wkNum).AddDate(0, 0, -7)
						chartPoints = append(chartPoints, &apiproto.TrackerChartPoint{
							Date:  baseDate.Format("2006-01-02"),
							Value: 100.0,
						})
						hasBaseDate = true
					}
				}

				currVal := 100.0
				for _, wk := range weeks {
					var year, wkNum int
					_, err := fmt.Sscanf(wk, "%d-W%d", &year, &wkNum)
					if err != nil {
						continue
					}

					currVal = currVal * (1.0 + returns[wk])

					chartPoints = append(chartPoints, &apiproto.TrackerChartPoint{
						Date:  isoWeekToDate(year, wkNum).Format("2006-01-02"),
						Value: currVal,
					})
				}

				// Generate Monte Carlo points
				var mcPoints []*apiproto.TrackerChartPoint
				if len(weeks) > 0 {
					// Populate flat returns slice
					returnValues := make([]float64, 0, len(weeks))
					for _, wk := range weeks {
						returnValues = append(returnValues, returns[wk])
					}

					numSims := 1000
					numWeeks := len(weeks)

					// Initialize paths: numSims rows, numWeeks + 1 columns
					paths := make([][]float64, numSims)
					for i := 0; i < numSims; i++ {
						paths[i] = make([]float64, numWeeks+1)
						paths[i][0] = 100.0
					}

					r := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 42))

					for step := 1; step <= numWeeks; step++ {
						for sim := 0; sim < numSims; sim++ {
							randIdx := r.IntN(len(returnValues))
							sampledReturn := returnValues[randIdx]
							paths[sim][step] = paths[sim][step-1] * (1.0 + sampledReturn)
						}
					}

					mcPoints = make([]*apiproto.TrackerChartPoint, 0, numWeeks+1)
					if hasBaseDate {
						mcPoints = append(mcPoints, &apiproto.TrackerChartPoint{
							Date:  baseDate.Format("2006-01-02"),
							Value: 100.0,
						})
					}

					stepVals := make([]float64, numSims)
					for step := 1; step <= numWeeks; step++ {
						for sim := 0; sim < numSims; sim++ {
							stepVals[sim] = paths[sim][step]
						}
						sort.Float64s(stepVals)
						// Median (50th percentile)
						medianVal := stepVals[numSims/2]

						var dateStr string
						var year, wkNum int
						_, err := fmt.Sscanf(weeks[step-1], "%d-W%d", &year, &wkNum)
						if err == nil {
							dateStr = isoWeekToDate(year, wkNum).Format("2006-01-02")
						}

						mcPoints = append(mcPoints, &apiproto.TrackerChartPoint{
							Date:  dateStr,
							Value: medianVal,
						})
					}
				}

				resp.Charts = append(resp.Charts, &apiproto.TrackerChart{
					Tracker:  t.Tracker,
					Points:   chartPoints,
					McPoints: mcPoints,
				})
			} else {
				// For 1w and 1d, use GetTrackerHistory directly
				bars, err := a.marketData.GetTrackerHistory(t, rangeStr)
				if err != nil {
					log.Printf("[ASSETS] Failed to get tracker history for range %s: %v", rangeStr, err)
					continue
				}

				if len(bars) == 0 {
					continue
				}

				chartPoints := make([]*apiproto.TrackerChartPoint, 0, len(bars))
				firstPrice := bars[0].AdjClose
				if firstPrice <= 0 {
					continue
				}

				for _, b := range bars {
					chartPoints = append(chartPoints, &apiproto.TrackerChartPoint{
						Date:  b.Date.Format(time.RFC3339),
						Value: 100.0 * (b.AdjClose / firstPrice),
					})
				}

				resp.Charts = append(resp.Charts, &apiproto.TrackerChart{
					Tracker: t.Tracker,
					Points:  chartPoints,
				})
			}
		}
	}

	a.handler.SendResponse(s, reqID, resp, true)
}

// ClearCache maps to route "assets::clear_cache"
func (a *Assets) ClearCache(s *api.WebsocketSession, reqID string, body *apiproto.Empty) {
	userID, _ := s.GetAuth()
	if userID == "" {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	_, err := a.repo.ClearHistoryCache()
	if err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	a.handler.SendResponse(s, reqID, &apiproto.Empty{}, true)
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
