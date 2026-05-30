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

// Rules isolates everything relating to the "rules::" namespace.
// The reflection registry maps methods on this struct to "rules::[method_name]".
type Rules struct {
	handler *api.WebSocketHandler
	rules   *repository.RuleRepository
	sync    *service.SyncService
}

// NewRules instantiates the isolated Rules handler namespace.
func NewRules(handler *api.WebSocketHandler, rules *repository.RuleRepository, sync *service.SyncService) *Rules {
	return &Rules{
		handler: handler,
		rules:   rules,
		sync:    sync,
	}
}

// List automatically registers as "rules::list"
func (r *Rules) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		r.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entities, err := r.rules.ListRules(userID)
	if err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.TransactionRuleList{}
	for _, e := range entities {
		resp.Rules = append(resp.Rules, mapRuleToProto(e))
	}

	r.handler.SendResponse(s, reqID, resp, true)
}

// Save automatically registers as "rules::save"
func (r *Rules) Save(s *api.WebsocketSession, reqID string, reqObj *apiproto.TransactionRule) {
	userID, _ := s.GetAuth()
	if userID == "" {
		r.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var mapProtoToRule func(p *apiproto.TransactionRule) domain.TransactionRule
	mapProtoToRule = func(p *apiproto.TransactionRule) domain.TransactionRule {
		rule := domain.TransactionRule{
			ID:             p.Id,
			UserID:         userID,
			Operator:       p.Operator,
			Field:          p.Field,
			Regex:          p.Regex,
			AmountOperator: p.AmountOperator,
			Priority:       int(p.Priority),
			Negate:         p.Negate,
		}
		if p.ParentId != "" {
			rule.ParentID = &p.ParentId
		}
		if p.IntegrationId != "" {
			rule.IntegrationID = &p.IntegrationId
		}
		if p.TargetPoolId != "" {
			rule.TargetPoolID = &p.TargetPoolId
		}
		if p.Field == "AMOUNT" {
			val := p.AmountValue
			rule.AmountValue = &val
		}
		for _, c := range p.Children {
			rule.Children = append(rule.Children, mapProtoToRule(c))
		}
		return rule
	}

	domainObj := mapProtoToRule(reqObj)
	if err := r.rules.SaveRule(userID, &domainObj); err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	// Trigger rule re-application to categorize existing transactions
	r.sync.ReapplyAllRules(userID)

	r.handler.SendResponse(s, reqID, mapRuleToProto(domainObj), true)
}

// Delete automatically registers as "rules::delete"
func (r *Rules) Delete(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		r.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := r.rules.DeleteRule(userID, reqIDObj.Id); err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	// Trigger rule re-application to categorize existing transactions
	r.sync.ReapplyAllRules(userID)

	r.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

func mapRuleToProto(r domain.TransactionRule) *apiproto.TransactionRule {
	pr := &apiproto.TransactionRule{
		Id:             r.ID,
		UserId:         r.UserID,
		Operator:       r.Operator,
		Field:          r.Field,
		Regex:          r.Regex,
		AmountOperator: r.AmountOperator,
		Priority:       int32(r.Priority),
		Negate:         r.Negate,
		CreatedAt:      r.CreatedAt.Format(time.RFC3339),
	}
	if r.IntegrationID != nil {
		pr.IntegrationId = *r.IntegrationID
	}
	if r.AmountValue != nil {
		pr.AmountValue = *r.AmountValue
	}
	if r.ParentID != nil {
		pr.ParentId = *r.ParentID
	}
	if r.TargetPoolID != nil {
		pr.TargetPoolId = *r.TargetPoolID
	}
	for _, c := range r.Children {
		pr.Children = append(pr.Children, mapRuleToProto(c))
	}
	return pr
}
