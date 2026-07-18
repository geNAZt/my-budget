package handler

import (
	"net/http"
	"time"

	"github.com/genazt/my-budget-script/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/backend/internal/domain"
	"github.com/genazt/my-budget-script/backend/internal/repository"
)

// Loans isolates everything relating to the "loans::" namespace.
// The reflection registry maps methods on this struct to "loans::[method_name]".
type Loans struct {
	handler   *api.WebSocketHandler
	loans     *repository.LoanRepository
	scenarios *repository.ScenarioRepository
}

// NewLoans instantiates the isolated Loans handler namespace.
func NewLoans(handler *api.WebSocketHandler, loans *repository.LoanRepository, scenarios *repository.ScenarioRepository) *Loans {
	return &Loans{
		handler:   handler,
		loans:     loans,
		scenarios: scenarios,
	}
}

// List automatically registers as "loans::list"
func (l *Loans) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		l.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entities, err := l.loans.List(userID)
	if err != nil {
		l.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.LoanList{}
	for _, e := range entities {
		resp.Loans = append(resp.Loans, mapLoanToProto(e))
	}

	l.handler.SendResponse(s, reqID, resp, true)
}

// Save automatically registers as "loans::save"
func (l *Loans) Save(s *api.WebsocketSession, reqID string, reqObj *apiproto.Loan) {
	userID, _ := s.GetAuth()
	if userID == "" {
		l.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	domainObj := domain.Loan{
		ID:              reqObj.Id,
		UserID:          userID,
		Name:            reqObj.Name,
		AccountIDs:      reqObj.AccountIds,
		LinkToScenarios: reqObj.LinkToScenarios,
		ActiveVersion:   mapProtoToLoanVersion(reqObj.ActiveVersion),
	}
	if reqObj.PoolId != "" {
		domainObj.PoolID = &reqObj.PoolId
	}

	if err := l.loans.Save(userID, &domainObj); err != nil {
		l.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	if len(domainObj.LinkToScenarios) > 0 {
		l.scenarios.LinkEntityToScenarios(userID, domainObj.ID, "LOAN", domainObj.LinkToScenarios)
	}

	l.handler.SendResponse(s, reqID, mapLoanToProto(domainObj), true)
}

// Delete automatically registers as "loans::delete"
func (l *Loans) Delete(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		l.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := l.loans.ArchiveFull(userID, reqIDObj.Id); err != nil {
		l.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	l.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

func mapLoanToProto(l domain.Loan) *apiproto.Loan {
	pl := &apiproto.Loan{
		Id:              l.ID,
		UserId:          l.UserID,
		Name:            l.Name,
		IsDeleted:       l.IsDeleted,
		AccountIds:      l.AccountIDs,
		CreatedAt:       l.CreatedAt.Format(time.RFC3339),
		LinkToScenarios: l.LinkToScenarios,
	}
	if l.PoolID != nil {
		pl.PoolId = *l.PoolID
	}
	if l.ActiveVersion != nil {
		pl.ActiveVersion = &apiproto.LoanVersion{
			Id:                 l.ActiveVersion.ID,
			LoanId:             l.ActiveVersion.LoanID,
			AmountLent:         l.ActiveVersion.AmountLent,
			InterestRate:       l.ActiveVersion.InterestRate,
			RuntimeMonths:      int32(l.ActiveVersion.RuntimeMonths),
			StartDate:          l.ActiveVersion.StartDate.Format(time.RFC3339),
			Priority:           int32(l.ActiveVersion.Priority), // Assuming Priority is mapped as int32 in proto based on common patterns
			BalloonLeftover:    l.ActiveVersion.BalloonLeftover,
			IsInterestOnly:     l.ActiveVersion.IsInterestOnly,
			EarlyPayoffPenalty: l.ActiveVersion.EarlyPayoffPenalty,
			CreatedAt:          l.ActiveVersion.CreatedAt.Format(time.RFC3339),
		}
		if l.ActiveVersion.RemainderStartDate != nil {
			pl.ActiveVersion.RemainderStartDate = l.ActiveVersion.RemainderStartDate.Format(time.RFC3339)
		}
		if l.ActiveVersion.NextLoanID != nil {
			pl.ActiveVersion.NextLoanId = *l.ActiveVersion.NextLoanID
		}
	}
	return pl
}

func mapProtoToLoanVersion(pl *apiproto.LoanVersion) *domain.LoanVersion {
	if pl == nil {
		return nil
	}
	dlv := &domain.LoanVersion{
		ID:                 pl.Id,
		LoanID:             pl.LoanId,
		AmountLent:         pl.AmountLent,
		InterestRate:       pl.InterestRate,
		RuntimeMonths:      int(pl.RuntimeMonths),
		Priority:           float64(pl.Priority),
		BalloonLeftover:    pl.BalloonLeftover,
		IsInterestOnly:     pl.IsInterestOnly,
		EarlyPayoffPenalty: pl.EarlyPayoffPenalty,
	}
	if pl.StartDate != "" {
		if t, err := time.Parse(time.RFC3339, pl.StartDate); err == nil {
			dlv.StartDate = t
		}
	}
	if pl.RemainderStartDate != "" {
		if t, err := time.Parse(time.RFC3339, pl.RemainderStartDate); err == nil {
			dlv.RemainderStartDate = &t
		}
	}
	if pl.NextLoanId != "" {
		dlv.NextLoanID = &pl.NextLoanId
	}
	return dlv
}
