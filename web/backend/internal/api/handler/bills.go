package handler

import (
	"net/http"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/web/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
)

// Bills isolates everything relating to the "bills::" namespace.
// The reflection registry maps methods on this struct to "bills::[method_name]".
type Bills struct {
	handler   *api.WebSocketHandler
	bills     *repository.BillRepository
	scenarios *repository.ScenarioRepository
}

// NewBills instantiates the isolated Bills handler namespace.
func NewBills(handler *api.WebSocketHandler, bills *repository.BillRepository, scenarios *repository.ScenarioRepository) *Bills {
	return &Bills{
		handler:   handler,
		bills:     bills,
		scenarios: scenarios,
	}
}

// List automatically registers as "bills::list"
func (b *Bills) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		b.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entities, err := b.bills.List(userID)
	if err != nil {
		b.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.BillList{}
	for _, e := range entities {
		resp.Bills = append(resp.Bills, mapBillToProto(e))
	}

	b.handler.SendResponse(s, reqID, resp, true)
}

// Save automatically registers as "bills::save"
func (b *Bills) Save(s *api.WebsocketSession, reqID string, reqObj *apiproto.Bill) {
	userID, _ := s.GetAuth()
	if userID == "" {
		b.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	domainObj := domain.Bill{
		ID:              reqObj.Id,
		UserID:          userID,
		Name:            reqObj.Name,
		AccountIDs:      reqObj.AccountIds,
		LinkToScenarios: reqObj.LinkToScenarios,
		ActiveVersion:   mapProtoToBillVersion(reqObj.ActiveVersion),
	}
	if reqObj.PoolId != "" {
		domainObj.PoolID = &reqObj.PoolId
	}

	if err := b.bills.Save(userID, &domainObj); err != nil {
		b.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	if len(domainObj.LinkToScenarios) > 0 {
		b.scenarios.LinkEntityToScenarios(userID, domainObj.ID, "BILL", domainObj.LinkToScenarios)
	}

	b.handler.SendResponse(s, reqID, mapBillToProto(domainObj), true)
}

// SaveBulk automatically registers as "bills::save_bulk"
func (b *Bills) SaveBulk(s *api.WebsocketSession, reqID string, reqList *apiproto.BillList) {
	userID, _ := s.GetAuth()
	if userID == "" {
		b.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var domainObjs []domain.Bill

	for _, reqObj := range reqList.Bills {
		domainObj := domain.Bill{
			ID:              reqObj.Id,
			UserID:          userID,
			Name:            reqObj.Name,
			AccountIDs:      reqObj.AccountIds,
			LinkToScenarios: reqObj.LinkToScenarios,
			ActiveVersion:   mapProtoToBillVersion(reqObj.ActiveVersion),
		}
		if reqObj.PoolId != "" {
			domainObj.PoolID = &reqObj.PoolId
		}
		domainObjs = append(domainObjs, domainObj)
	}

	if err := b.bills.SaveBulk(userID, domainObjs); err != nil {
		b.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.BillList{}
	for _, d := range domainObjs {
		if len(d.LinkToScenarios) > 0 {
			b.scenarios.LinkEntityToScenarios(userID, d.ID, "BILL", d.LinkToScenarios)
		}
		resp.Bills = append(resp.Bills, mapBillToProto(d))
	}

	b.handler.SendResponse(s, reqID, resp, true)
}

// Delete automatically registers as "bills::delete"
func (b *Bills) Delete(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		b.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := b.bills.ArchiveFull(userID, reqIDObj.Id); err != nil {
		b.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	b.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

func mapBillToProto(b domain.Bill) *apiproto.Bill {
	pb := &apiproto.Bill{
		Id:              b.ID,
		UserId:          b.UserID,
		Name:            b.Name,
		IsDeleted:       b.IsDeleted,
		AccountIds:      b.AccountIDs,
		CreatedAt:       b.CreatedAt.Format(time.RFC3339),
		LinkToScenarios: b.LinkToScenarios,
	}
	if b.PoolID != nil {
		pb.PoolId = *b.PoolID
	}
	if b.ActiveVersion != nil {
		pb.ActiveVersion = &apiproto.BillVersion{
			Id:             b.ActiveVersion.ID,
			BillId:         b.ActiveVersion.BillID,
			Amount:         b.ActiveVersion.Amount,
			StartDate:      b.ActiveVersion.StartDate.Format(time.RFC3339),
			IntervalMonths: int32(b.ActiveVersion.IntervalMonths),
			CreatedAt:      b.ActiveVersion.CreatedAt.Format(time.RFC3339),
		}
		if b.ActiveVersion.EndDate != nil {
			pb.ActiveVersion.EndDate = b.ActiveVersion.EndDate.Format(time.RFC3339)
		}
	}
	return pb
}

func mapProtoToBillVersion(pv *apiproto.BillVersion) *domain.BillVersion {
	if pv == nil {
		return nil
	}
	dbv := &domain.BillVersion{
		ID:             pv.Id,
		BillID:         pv.BillId,
		Amount:         pv.Amount,
		IntervalMonths: int(pv.IntervalMonths),
	}
	if pv.StartDate != "" {
		if t, err := time.Parse(time.RFC3339, pv.StartDate); err == nil {
			dbv.StartDate = t
		}
	}
	if pv.EndDate != "" {
		if t, err := time.Parse(time.RFC3339, pv.EndDate); err == nil {
			dbv.EndDate = &t
		}
	}
	return dbv
}
