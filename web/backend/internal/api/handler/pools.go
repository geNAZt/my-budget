package handler

import (
	"net/http"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/web/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	"github.com/genazt/my-budget-script/web/backend/internal/service"
)

// Pools isolates everything relating to the "pools::" namespace.
// The reflection registry maps methods on this struct to "pools::[method_name]".
type Pools struct {
	handler *api.WebSocketHandler
	rules   *repository.RuleRepository
	sync    *service.SyncService
}

// NewPools instantiates the isolated Pools handler namespace.
func NewPools(handler *api.WebSocketHandler, rules *repository.RuleRepository, sync *service.SyncService) *Pools {
	return &Pools{
		handler: handler,
		rules:   rules,
		sync:    sync,
	}
}

// List automatically registers as "pools::list"
func (p *Pools) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		p.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entities, err := p.rules.ListPools(userID)
	if err != nil {
		p.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.TransactionPoolList{}
	for _, e := range entities {
		resp.Pools = append(resp.Pools, mapPoolToProto(e))
	}

	p.handler.SendResponse(s, reqID, resp, true)
}

// Save automatically registers as "pools::save"
func (p *Pools) Save(s *api.WebsocketSession, reqID string, reqObj *apiproto.TransactionPool) {
	userID, _ := s.GetAuth()
	if userID == "" {
		p.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	domainObj := domain.TransactionPool{
		ID:       reqObj.Id,
		UserID:   userID,
		Name:     reqObj.Name,
		Color:    reqObj.Color,
		IsHidden: reqObj.IsHidden,
	}
	if reqObj.ParentId != "" {
		domainObj.ParentID = &reqObj.ParentId
	}

	if err := p.rules.SavePool(userID, &domainObj); err != nil {
		p.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	// Trigger rule re-application to categorize existing transactions
	p.sync.ReapplyAllRules(userID)

	p.handler.SendResponse(s, reqID, mapPoolToProto(domainObj), true)
}

// Delete automatically registers as "pools::delete"
func (p *Pools) Delete(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		p.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := p.rules.DeletePool(userID, reqIDObj.Id); err != nil {
		p.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	// Trigger rule re-application to categorize existing transactions
	p.sync.ReapplyAllRules(userID)

	p.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

// Helper mapper function kept identical
func mapPoolToProto(p domain.TransactionPool) *apiproto.TransactionPool {
	pp := &apiproto.TransactionPool{
		Id:        p.ID,
		UserId:    p.UserID,
		Name:      p.Name,
		Color:     p.Color,
		IsHidden:  p.IsHidden,
		CreatedAt: p.CreatedAt.Format(time.RFC3339),
	}
	if p.ParentID != nil {
		pp.ParentId = *p.ParentID
	}
	return pp
}
