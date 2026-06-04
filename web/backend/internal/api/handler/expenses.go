package handler

import (
	"net/http"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/web/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
)

// Expenses isolates everything relating to the "expenses::" namespace.
// The reflection registry maps methods on this struct to "expenses::[method_name]".
type Expenses struct {
	handler   *api.WebSocketHandler
	expenses  *repository.ExpenseRepository
	scenarios *repository.ScenarioRepository
}

// NewExpenses instantiates the isolated Expenses handler namespace.
func NewExpenses(handler *api.WebSocketHandler, expenses *repository.ExpenseRepository, scenarios *repository.ScenarioRepository) *Expenses {
	return &Expenses{
		handler:  handler,
		expenses: expenses,
	}
}

// List automatically registers as "expenses::list"
func (e *Expenses) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		e.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entities, err := e.expenses.List(userID)
	if err != nil {
		e.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.ExpenseList{}
	for _, ent := range entities {
		resp.Expenses = append(resp.Expenses, mapExpenseToProto(ent))
	}

	e.handler.SendResponse(s, reqID, resp, true)
}

// Save automatically registers as "expenses::save"
func (e *Expenses) Save(s *api.WebsocketSession, reqID string, reqObj *apiproto.Expense) {
	userID, _ := s.GetAuth()
	if userID == "" {
		e.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	domainObj := domain.Expense{
		ID:              reqObj.Id,
		UserID:          userID,
		Name:            reqObj.Name,
		AccountIDs:      reqObj.AccountIds,
		LinkToScenarios: reqObj.LinkToScenarios,
		ActiveVersion:   mapProtoToExpenseVersion(reqObj.ActiveVersion),
	}
	if reqObj.PoolId != "" {
		domainObj.PoolID = &reqObj.PoolId
	}

	if err := e.expenses.Save(userID, &domainObj); err != nil {
		e.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	if len(domainObj.LinkToScenarios) > 0 {
		e.scenarios.LinkEntityToScenarios(userID, domainObj.ID, "EXPENSE", domainObj.LinkToScenarios)
	}

	e.handler.SendResponse(s, reqID, mapExpenseToProto(domainObj), true)
}

// Delete automatically registers as "expenses::delete"
func (e *Expenses) Delete(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		e.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := e.expenses.ArchiveFull(userID, reqIDObj.Id); err != nil {
		e.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	e.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

func mapExpenseToProto(e domain.Expense) *apiproto.Expense {
	pe := &apiproto.Expense{
		Id:              e.ID,
		UserId:          e.UserID,
		Name:            e.Name,
		IsDeleted:       e.IsDeleted,
		AccountIds:      e.AccountIDs,
		CreatedAt:       e.CreatedAt.Format(time.RFC3339),
		LinkToScenarios: e.LinkToScenarios,
	}
	if e.PoolID != nil {
		pe.PoolId = *e.PoolID
	}
	if e.ActiveVersion != nil {
		pe.ActiveVersion = &apiproto.ExpenseVersion{
			Id:        e.ActiveVersion.ID,
			ExpenseId: e.ActiveVersion.ExpenseID,
			Amount:    e.ActiveVersion.Amount,
			DueDate:   e.ActiveVersion.DueDate.Format(time.RFC3339),
			CreatedAt: e.ActiveVersion.CreatedAt.Format(time.RFC3339),
			Slices:    mapTimeSlicesToProto(e.ActiveVersion.Slices),
		}
	}
	return pe
}

func mapProtoToExpenseVersion(pe *apiproto.ExpenseVersion) *domain.ExpenseVersion {
	if pe == nil {
		return nil
	}
	dev := &domain.ExpenseVersion{
		ID:        pe.Id,
		ExpenseID: pe.ExpenseId,
		Amount:    pe.Amount,
		Slices:    mapProtoToTimeSlices(pe.Slices),
	}
	if pe.DueDate != "" {
		if t, err := time.Parse(time.RFC3339, pe.DueDate); err == nil {
			dev.DueDate = t
		}
	}
	return dev
}
