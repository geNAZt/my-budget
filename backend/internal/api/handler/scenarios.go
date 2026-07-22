package handler

import (
	"log"
	"net/http"
	"time"

	"github.com/genazt/my-budget-script/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/backend/internal/domain"
	"github.com/genazt/my-budget-script/backend/internal/repository"
)

// Scenarios isolates everything relating to the "scenarios::" namespace.
// The reflection registry maps methods on this struct to "scenarios::[method_name]".
type Scenarios struct {
	handler   *api.WebSocketHandler
	scenarios *repository.ScenarioRepository
}

// NewScenarios instantiates the isolated Scenarios handler namespace.
func NewScenarios(handler *api.WebSocketHandler, scenarios *repository.ScenarioRepository) *Scenarios {
	return &Scenarios{
		handler:   handler,
		scenarios: scenarios,
	}
}

// List automatically registers as "scenarios::list"
func (sc *Scenarios) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		sc.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entities, err := sc.scenarios.List(userID)
	if err != nil {
		sc.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.ScenarioList{}
	for _, e := range entities {
		resp.Scenarios = append(resp.Scenarios, mapScenarioToProto(e))
	}

	sc.handler.SendResponse(s, reqID, resp, true)
}

// Save automatically registers as "scenarios::save"
func (sc *Scenarios) Save(s *api.WebsocketSession, reqID string, reqObj *apiproto.Scenario) {
	userID, _ := s.GetAuth()
	if userID == "" {
		sc.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	domainObj := domain.Scenario{
		ID:                       reqObj.Id,
		UserID:                   userID,
		Name:                     reqObj.Name,
		Description:              reqObj.Description,
		ProjectionMonths:         int(reqObj.ProjectionMonths),
		RemainderOrder:           reqObj.RemainderOrder,
		IsActive:                 reqObj.IsActive,
		MonthStartDay:            int(reqObj.MonthStartDay),
		Simulations:              int(reqObj.Simulations),
		SimYears:                 int(reqObj.SimYears),
		SimPercent:               reqObj.SimPercent,
		LookbackYears:            int(reqObj.LookbackYears),
		MonteCarloImplementation: reqObj.McImplementation,
		PassiveIncomePercentage:  reqObj.PassiveIncomePercentage,
	}

	domainObj.ETFParams = make(map[string]domain.ETFScenarioParams)
	for k, v := range reqObj.EtfParams {
		if v != nil {
			domainObj.ETFParams[k] = domain.ETFScenarioParams{
				Simulations:   int(v.Simulations),
				SimYears:      int(v.SimYears),
				SimPercent:    v.SimPercent,
				LookbackYears: int(v.LookbackYears),
			}
		}
	}

	if reqObj.StartDate != "" {
		if t, err := time.Parse(time.RFC3339, reqObj.StartDate); err == nil {
			domainObj.StartDate = &t
		}
	}

	for _, e := range reqObj.Entities {
		domainObj.Entities = append(domainObj.Entities, domain.ScenarioEntity{
			EntityID:   e.EntityId,
			EntityType: e.EntityType,
			VersionID:  e.VersionId,
		})
	}

	if err := sc.scenarios.Save(userID, &domainObj); err != nil {
		sc.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	sc.handler.SendResponse(s, reqID, mapScenarioToProto(domainObj), true)
}

// Delete automatically registers as "scenarios::delete"
func (sc *Scenarios) Delete(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		sc.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := sc.scenarios.Archive(userID, reqIDObj.Id); err != nil {
		sc.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	sc.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

// Projection automatically registers as "scenarios::projection"
func (sc *Scenarios) Projection(s *api.WebsocketSession, reqID string, reqObj *apiproto.Scenario) {
	userID, _ := s.GetAuth()
	if userID == "" {
		sc.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	scenarioID := reqObj.Id
	limit := int(reqObj.ProjectionMonths)

	// Save scenario configuration before running simulation to ensure Monte Carlo takes current settings into account
	if reqObj.Name != "" {
		domainObj := domain.Scenario{
			ID:                       reqObj.Id,
			UserID:                   userID,
			Name:                     reqObj.Name,
			Description:              reqObj.Description,
			ProjectionMonths:         int(reqObj.ProjectionMonths),
			RemainderOrder:           reqObj.RemainderOrder,
			IsActive:                 reqObj.IsActive,
			MonthStartDay:            int(reqObj.MonthStartDay),
			Simulations:              int(reqObj.Simulations),
			SimYears:                 int(reqObj.SimYears),
			SimPercent:               reqObj.SimPercent,
			LookbackYears:            int(reqObj.LookbackYears),
			MonteCarloImplementation: reqObj.McImplementation,
			PassiveIncomePercentage:  reqObj.PassiveIncomePercentage,
		}

		domainObj.ETFParams = make(map[string]domain.ETFScenarioParams)
		for k, v := range reqObj.EtfParams {
			if v != nil {
				domainObj.ETFParams[k] = domain.ETFScenarioParams{
					Simulations:   int(v.Simulations),
					SimYears:      int(v.SimYears),
					SimPercent:    v.SimPercent,
					LookbackYears: int(v.LookbackYears),
				}
			}
		}

		if reqObj.StartDate != "" {
			if t, err := time.Parse(time.RFC3339, reqObj.StartDate); err == nil {
				domainObj.StartDate = &t
			}
		}

		for _, e := range reqObj.Entities {
			domainObj.Entities = append(domainObj.Entities, domain.ScenarioEntity{
				EntityID:   e.EntityId,
				EntityType: e.EntityType,
				VersionID:  e.VersionId,
			})
		}

		if err := sc.scenarios.Save(userID, &domainObj); err != nil {
			sc.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
			return
		}
	}

	result, err := sc.handler.Projection.RunWithLimit(userID, scenarioID, limit, func(month domain.ProjectionMonth) {
		wsMonth := &apiproto.ProjectionMonth{
			Date:          month.Date.Format(time.RFC3339),
			PeriodStart:   month.PeriodStart.Format(time.RFC3339),
			PeriodEnd:     month.PeriodEnd.Format(time.RFC3339),
			Income:        month.Income,
			PassiveIncome: month.PassiveIncome,
			Bills:         month.Bills,
			Expenses:      month.Expenses,
			Assets:        month.Assets,
			Loans:         month.Loans,
			Remainder:     month.Remainder,
			Balance:       month.Balance,
			AssetWorth:    month.AssetWorth,
			LoanDebt:      month.LoanDebt,
		}

		for _, va := range month.VirtualAccounts {
			wsMonth.VirtualAccounts = append(wsMonth.VirtualAccounts, &apiproto.VirtualAccountMonthBalance{
				Id:                va.AccountID,
				Name:              va.Name,
				Balance:           va.Balance,
				AllocatedBills:    va.Inflow,
				AllocatedExpenses: va.Outflow,
				StartingBalance:   va.StartingBalance,
				RealtimeAccountId: va.RealtimeAccountID,
			})
		}

		derefStr := func(p *string) string {
			if p == nil {
				return ""
			}
			return *p
		}

		derefFloat := func(p *float64) float64 {
			if p == nil {
				return 0
			}
			return *p
		}

		wsMonth.Breakdown = &apiproto.MonthBreakdown{}
		for _, item := range month.Breakdown.Incomes {
			wsMonth.Breakdown.Incomes = append(wsMonth.Breakdown.Incomes, &apiproto.EntryBreakdown{
				Name:                item.Name,
				EntityName:          item.EntityName,
				Amount:              item.Amount,
				RealtimeBalance:     derefFloat(item.RealtimeBalance),
				PoolId:              derefStr(item.PoolID),
				AccountIds:          item.AccountIDs,
				Interest:            item.Interest,
				Penalty:             item.Penalty,
				Balance:             item.Balance,
				RealSplit:           item.RealSplit,
				TrackerFlows:        item.TrackerFlows,
				SubAssetFlows:       item.SubAssetFlows,
				PreviousBookingDate: item.PreviousBookingDate,
				BookingDate:         item.BookingDate,
			})
		}
		for _, item := range month.Breakdown.Bills {
			wsMonth.Breakdown.Bills = append(wsMonth.Breakdown.Bills, &apiproto.EntryBreakdown{
				Name:                item.Name,
				EntityName:          item.EntityName,
				Amount:              item.Amount,
				RealtimeBalance:     derefFloat(item.RealtimeBalance),
				PoolId:              derefStr(item.PoolID),
				AccountIds:          item.AccountIDs,
				Interest:            item.Interest,
				Penalty:             item.Penalty,
				Balance:             item.Balance,
				RealSplit:           item.RealSplit,
				TrackerFlows:        item.TrackerFlows,
				SubAssetFlows:       item.SubAssetFlows,
				PreviousBookingDate: item.PreviousBookingDate,
				BookingDate:         item.BookingDate,
			})
		}
		for _, item := range month.Breakdown.Expenses {
			wsMonth.Breakdown.Expenses = append(wsMonth.Breakdown.Expenses, &apiproto.EntryBreakdown{
				Name:                item.Name,
				EntityName:          item.EntityName,
				Amount:              item.Amount,
				RealtimeBalance:     derefFloat(item.RealtimeBalance),
				PoolId:              derefStr(item.PoolID),
				AccountIds:          item.AccountIDs,
				Interest:            item.Interest,
				Penalty:             item.Penalty,
				Balance:             item.Balance,
				RealSplit:           item.RealSplit,
				TrackerFlows:        item.TrackerFlows,
				SubAssetFlows:       item.SubAssetFlows,
				PreviousBookingDate: item.PreviousBookingDate,
				BookingDate:         item.BookingDate,
			})
		}
		for _, item := range month.Breakdown.Assets {
			wsMonth.Breakdown.Assets = append(wsMonth.Breakdown.Assets, &apiproto.EntryBreakdown{
				Name:                item.Name,
				EntityName:          item.EntityName,
				Amount:              item.Amount,
				RealtimeBalance:     derefFloat(item.RealtimeBalance),
				PoolId:              derefStr(item.PoolID),
				AccountIds:          item.AccountIDs,
				Interest:            item.Interest,
				Penalty:             item.Penalty,
				Balance:             item.Balance,
				RealSplit:           item.RealSplit,
				TrackerFlows:        item.TrackerFlows,
				SubAssetFlows:       item.SubAssetFlows,
				PreviousBookingDate: item.PreviousBookingDate,
				BookingDate:         item.BookingDate,
			})
		}
		for _, item := range month.Breakdown.Loans {
			wsMonth.Breakdown.Loans = append(wsMonth.Breakdown.Loans, &apiproto.EntryBreakdown{
				Name:                item.Name,
				EntityName:          item.EntityName,
				Amount:              item.Amount,
				RealtimeBalance:     derefFloat(item.RealtimeBalance),
				PoolId:              derefStr(item.PoolID),
				AccountIds:          item.AccountIDs,
				Interest:            item.Interest,
				Penalty:             item.Penalty,
				Balance:             item.Balance,
				RealSplit:           item.RealSplit,
				TrackerFlows:        item.TrackerFlows,
				SubAssetFlows:       item.SubAssetFlows,
				PreviousBookingDate: item.PreviousBookingDate,
				BookingDate:         item.BookingDate,
			})
		}

		sc.handler.SendResponse(s, reqID, wsMonth, false)
	})

	if err != nil {
		sc.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	yields := result.SimulatedYields
	if yields == nil {
		yields = make(map[string]float64)
	}

	log.Printf("[SCENARIOS] Projection finished. Simulated yields: %d entries", len(yields))
	for k, v := range yields {
		log.Printf("[SCENARIOS] Yield for %s: %.2f%%", k, v)
	}

	wsYields := &apiproto.YieldMap{
		Yields: yields,
	}
	sc.handler.SendResponse(s, reqID, wsYields, false)

	wsPenalty := &apiproto.PenaltyAnalysis{}
	for _, event := range result.PenaltyAnalysis {
		wsPenalty.Events = append(wsPenalty.Events, &apiproto.PenaltyEvent{
			Type:              event.Type,
			Date:              event.Date.Format(time.RFC3339),
			AssetName:         event.AssetName,
			LotId:             event.LotID,
			LotCreatedAt:      event.LotCreatedAt.Format(time.RFC3339),
			Amount:            event.Amount,
			PrincipalSold:     event.PrincipalSold,
			PenaltyPaid:       event.PenaltyPaid,
			MonthsHeld:        int32(event.MonthsHeld),
			InterestGenerated: event.InterestGenerated,
			Reason:            event.Reason,
			RemainingTaxAllowance: event.RemainingTaxAllowance,
		})
	}
	sc.handler.SendResponse(s, reqID, wsPenalty, false)

	wsMetrics := &apiproto.PerformanceMetrics{
		TotalDurationMs:      result.Metrics.TotalDurationMS,
		ResolutionDurationMs: result.Metrics.ResolutionDurationMS,
		MonteCarloDurationMs: result.Metrics.MonteCarloDurationMS,
		CatchupDurationMs:    result.Metrics.CatchupDurationMS,
		ProjectionDurationMs: result.Metrics.ProjectionDurationMS,
		PerAssetMcMs:         0,
	}
	sc.handler.SendResponse(s, reqID, wsMetrics, false)

	sc.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqObj.Id}, true)
}

// Pdf automatically registers as "scenarios::pdf"
func (sc *Scenarios) Pdf(s *api.WebsocketSession, reqID string, reqObj *apiproto.ScenarioPDFRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		sc.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	scenario, err := sc.scenarios.GetFull(userID, reqObj.ScenarioId)
	if err != nil {
		sc.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	result, err := sc.handler.Projection.RunWithLimit(userID, reqObj.ScenarioId, scenario.ProjectionMonths, nil)
	if err != nil {
		sc.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	pdfBytes, err := sc.handler.Projection.GenerateScenarioPDF(scenario.Name, result.Months)
	if err != nil {
		sc.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.ScenarioPDFResponse{
		PdfBytes: pdfBytes,
	}

	sc.handler.SendResponse(s, reqID, resp, true)
}

func mapScenarioToProto(sc domain.Scenario) *apiproto.Scenario {
	psc := &apiproto.Scenario{
		Id:                      sc.ID,
		UserId:                  sc.UserID,
		Name:                    sc.Name,
		Description:             sc.Description,
		ProjectionMonths:        int32(sc.ProjectionMonths),
		RemainderOrder:          sc.RemainderOrder,
		IsActive:                sc.IsActive,
		MonthStartDay:           int32(sc.MonthStartDay),
		CreatedAt:               sc.CreatedAt.Format(time.RFC3339),
		Simulations:             int32(sc.Simulations),
		SimYears:                int32(sc.SimYears),
		SimPercent:              sc.SimPercent,
		LookbackYears:           int32(sc.LookbackYears),
		McImplementation:        sc.MonteCarloImplementation,
		PassiveIncomePercentage: sc.PassiveIncomePercentage,
	}
	if sc.StartDate != nil {
		psc.StartDate = sc.StartDate.Format(time.RFC3339)
	}

	psc.EtfParams = make(map[string]*apiproto.ETFScenarioParams)
	for k, v := range sc.ETFParams {
		psc.EtfParams[k] = &apiproto.ETFScenarioParams{
			Simulations:   int32(v.Simulations),
			SimYears:      int32(v.SimYears),
			SimPercent:    v.SimPercent,
			LookbackYears: int32(v.LookbackYears),
		}
	}

	for _, e := range sc.Entities {
		psc.Entities = append(psc.Entities, &apiproto.ScenarioEntity{
			EntityId:   e.EntityID,
			EntityType: e.EntityType,
			VersionId:  e.VersionID,
		})
	}

	return psc
}
