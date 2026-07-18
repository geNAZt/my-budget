package handler

import (
	"net/http"
	"time"

	"github.com/genazt/my-budget-script/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/backend/internal/domain"
	"github.com/genazt/my-budget-script/backend/internal/repository"
	"github.com/genazt/my-budget-script/backend/internal/service"
)

// Automations acts as the root namespace -> "automations"
type Automations struct {
	handler *api.WebSocketHandler

	// Nested structs establish sub-namespaces -> "automations::plans" and "automations::connections"
	Plans       *AutomationPlans
	Connections *AutomationConnections
}

// NewAutomations instantiates the multi-level Automations namespace module.
func NewAutomations(handler *api.WebSocketHandler, executions *repository.ExecutionRepository, connections *repository.ConnectionRepository, executionService *service.ExecutionService) *Automations {
	return &Automations{
		handler:     handler,
		Plans:       &AutomationPlans{handler: handler, executions: executions, executionService: executionService},
		Connections: &AutomationConnections{handler: handler, connections: connections},
	}
}

// =========================================================================
// SUB-NAMESPACE: automations::plans
// =========================================================================
type AutomationPlans struct {
	handler          *api.WebSocketHandler
	executions       *repository.ExecutionRepository
	executionService *service.ExecutionService
}

// List automatically registers as "automations::plans::list"
func (p *AutomationPlans) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		p.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entities, err := p.executions.List(userID)
	if err != nil {
		p.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.ExecutionPlanList{}
	for _, e := range entities {
		resp.Plans = append(resp.Plans, mapExecutionPlanToProto(e))
	}

	p.handler.SendResponse(s, reqID, resp, true)
}

// Save automatically registers as "automations::plans::save"
func (p *AutomationPlans) Save(s *api.WebsocketSession, reqID string, reqPlan *apiproto.ExecutionPlan) {
	userID, _ := s.GetAuth()
	if userID == "" {
		p.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	domainPlan := domain.ExecutionPlan{
		ID:           reqPlan.Id,
		UserID:       userID,
		Name:         reqPlan.Name,
		Code:         reqPlan.Code,
		TriggerType:  reqPlan.TriggerType,
		TriggerValue: reqPlan.TriggerValue,
		IsEnabled:    reqPlan.IsEnabled,
	}

	if err := p.executions.Save(userID, &domainPlan); err != nil {
		p.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	p.handler.SendResponse(s, reqID, mapExecutionPlanToProto(domainPlan), true)
}

// Delete automatically registers as "automations::plans::delete"
func (p *AutomationPlans) Delete(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		p.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := p.executions.Delete(userID, reqIDObj.Id); err != nil {
		p.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	p.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

// Run automatically registers as "automations::plans::run"
func (p *AutomationPlans) Run(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		p.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := p.executionService.ExecutePlan(userID, reqIDObj.Id, nil); err != nil {
		p.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	p.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

// Logs automatically registers as "automations::plans::logs"
func (p *AutomationPlans) Logs(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		p.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	logs, err := p.executions.GetLogsForPlan(userID, reqIDObj.Id)
	if err != nil {
		p.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.ExecutionLogList{}
	for _, l := range logs {
		logProto := &apiproto.ExecutionLog{
			Id:        l.ID,
			UserId:    l.UserID,
			PlanId:    l.PlanID,
			Status:    l.Status,
			Stdout:    l.Stdout,
			Stderr:    l.Stderr,
			ExitCode:  int32(l.ExitCode),
			StartedAt: l.StartedAt.Format(time.RFC3339),
		}
		if l.FinishedAt != nil {
			logProto.FinishedAt = l.FinishedAt.Format(time.RFC3339)
		}
		resp.Logs = append(resp.Logs, logProto)
	}

	p.handler.SendResponse(s, reqID, resp, true)
}

// =========================================================================
// SUB-NAMESPACE: automations::connections
// =========================================================================
type AutomationConnections struct {
	handler     *api.WebSocketHandler
	connections *repository.ConnectionRepository
}

// List automatically registers as "automations::connections::list"
func (c *AutomationConnections) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		c.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entities, err := c.connections.List(userID)
	if err != nil {
		c.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.ExecutionConnectionList{}
	for _, e := range entities {
		resp.Connections = append(resp.Connections, mapExecutionConnectionToProto(e))
	}

	c.handler.SendResponse(s, reqID, resp, true)
}

// Save automatically registers as "automations::connections::save"
func (c *AutomationConnections) Save(s *api.WebsocketSession, reqID string, reqConn *apiproto.ExecutionConnection) {
	userID, _ := s.GetAuth()
	if userID == "" {
		c.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	domainConn := domain.Connection{
		ID:     reqConn.Id,
		UserID: userID,
		Name:   reqConn.Name,
		Value:  reqConn.Value,
	}

	if err := c.connections.Save(userID, &domainConn); err != nil {
		c.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	c.handler.SendResponse(s, reqID, mapExecutionConnectionToProto(domainConn), true)
}

// Delete automatically registers as "automations::connections::delete"
func (c *AutomationConnections) Delete(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		c.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := c.connections.Delete(userID, reqIDObj.Id); err != nil {
		c.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	c.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

func mapExecutionPlanToProto(p domain.ExecutionPlan) *apiproto.ExecutionPlan {
	return &apiproto.ExecutionPlan{
		Id:           p.ID,
		UserId:       p.UserID,
		Name:         p.Name,
		Code:         p.Code,
		TriggerType:  p.TriggerType,
		TriggerValue: p.TriggerValue,
		IsEnabled:    p.IsEnabled,
		CreatedAt:    p.CreatedAt.Format(time.RFC3339),
	}
}

func mapExecutionConnectionToProto(c domain.Connection) *apiproto.ExecutionConnection {
	return &apiproto.ExecutionConnection{
		Id:        c.ID,
		UserId:    c.UserID,
		Name:      c.Name,
		Value:     c.Value,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
	}
}
