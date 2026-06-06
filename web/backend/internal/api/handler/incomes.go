package handler

import (
	"net/http"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/web/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
)

// Incomes isolates everything relating to the "incomes::" namespace.
// The reflection registry maps methods on this struct to "incomes::[method_name]".
type Incomes struct {
	handler   *api.WebSocketHandler
	incomes   *repository.IncomeRepository
	scenarios *repository.ScenarioRepository
}

// NewIncomes instantiates the isolated Incomes handler namespace.
func NewIncomes(handler *api.WebSocketHandler, incomes *repository.IncomeRepository, scenarios *repository.ScenarioRepository) *Incomes {
	return &Incomes{
		handler:   handler,
		incomes:   incomes,
		scenarios: scenarios,
	}
}

// List automatically registers as "incomes::list"
func (i *Incomes) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		i.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	incomes, err := i.incomes.List(userID)
	if err != nil {
		i.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.IncomeList{}
	for _, income := range incomes {
		resp.Incomes = append(resp.Incomes, mapIncomeToProto(income))
	}

	i.handler.SendResponse(s, reqID, resp, true)
}

// Save automatically registers as "incomes::save"
func (i *Incomes) Save(s *api.WebsocketSession, reqID string, reqIncome *apiproto.Income) {
	userID, _ := s.GetAuth()
	if userID == "" {
		i.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	domainIncome := domain.Income{
		ID:              reqIncome.Id,
		UserID:          userID,
		Name:            reqIncome.Name,
		AccountIDs:      reqIncome.AccountIds,
		LinkToScenarios: reqIncome.LinkToScenarios,
		ActiveVersion:   mapProtoToIncomeVersion(reqIncome.ActiveVersion),
	}
	if reqIncome.PoolId != "" {
		domainIncome.PoolID = &reqIncome.PoolId
	}

	if err := i.incomes.Save(userID, &domainIncome); err != nil {
		i.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	if len(domainIncome.LinkToScenarios) > 0 {
		i.scenarios.LinkEntityToScenarios(userID, domainIncome.ID, "INCOME", domainIncome.LinkToScenarios)
	}

	i.handler.SendResponse(s, reqID, mapIncomeToProto(domainIncome), true)
}

// Delete automatically registers as "incomes::delete"
func (i *Incomes) Delete(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		i.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := i.incomes.ArchiveFull(userID, reqIDObj.Id); err != nil {
		i.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	i.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

func mapIncomeToProto(i domain.Income) *apiproto.Income {
	pi := &apiproto.Income{
		Id:              i.ID,
		UserId:          i.UserID,
		Name:            i.Name,
		IsDeleted:       i.IsDeleted,
		AccountIds:      i.AccountIDs,
		CreatedAt:       i.CreatedAt.Format(time.RFC3339),
		LinkToScenarios: i.LinkToScenarios,
	}

	if i.PoolID != nil {
		pi.PoolId = *i.PoolID
	}

	if i.ActiveVersion != nil {
		pi.ActiveVersion = &apiproto.IncomeVersion{
			Id:                         i.ActiveVersion.ID,
			IncomeId:                   i.ActiveVersion.IncomeID,
			Amount:                     i.ActiveVersion.Amount,
			StopModificationId:         "",
			StartDate:                  i.ActiveVersion.StartDate.Format(time.RFC3339),
			EndDate:                    "",
			IntervalMonths:             int32(i.ActiveVersion.IntervalMonths),
			CreatedAt:                  i.ActiveVersion.CreatedAt.Format(time.RFC3339),
			Slices:                     mapTimeSlicesToProto(i.ActiveVersion.Slices),
			IntervalIncreasePercentage: i.ActiveVersion.IntervalIncreasePercentage,
			IntervalIncreaseMonths:     int32(i.ActiveVersion.IntervalIncreaseMonths),
			IntervalIncreaseStartDate:  "",
		}
		if i.ActiveVersion.StopModificationID != nil {
			pi.ActiveVersion.StopModificationId = *i.ActiveVersion.StopModificationID
		}
		if i.ActiveVersion.EndDate != nil {
			pi.ActiveVersion.EndDate = i.ActiveVersion.EndDate.Format(time.RFC3339)
		}
		if i.ActiveVersion.IntervalIncreaseStartDate != nil {
			pi.ActiveVersion.IntervalIncreaseStartDate = i.ActiveVersion.IntervalIncreaseStartDate.Format(time.RFC3339)
		}
	}

	return pi
}

func mapProtoToIncomeVersion(pi *apiproto.IncomeVersion) *domain.IncomeVersion {
	if pi == nil {
		return nil
	}
	div := &domain.IncomeVersion{
		ID:                         pi.Id,
		IncomeID:                   pi.IncomeId,
		Amount:                     pi.Amount,
		IntervalMonths:             int(pi.IntervalMonths),
		Slices:                     mapProtoToTimeSlices(pi.Slices),
		IntervalIncreasePercentage: pi.IntervalIncreasePercentage,
		IntervalIncreaseMonths:     int(pi.IntervalIncreaseMonths),
	}
	if pi.StopModificationId != "" {
		div.StopModificationID = &pi.StopModificationId
	}
	if pi.StartDate != "" {
		if t, err := time.Parse(time.RFC3339, pi.StartDate); err == nil {
			div.StartDate = t
		}
	}
	if pi.EndDate != "" {
		if t, err := time.Parse(time.RFC3339, pi.EndDate); err == nil {
			div.EndDate = &t
		}
	}
	if pi.IntervalIncreaseStartDate != "" {
		if t, err := time.Parse(time.RFC3339, pi.IntervalIncreaseStartDate); err == nil {
			div.IntervalIncreaseStartDate = &t
		}
	}
	return div
}
