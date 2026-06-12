package handler

import (
	"net/http"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/web/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/web/backend/internal/service"
	"github.com/google/uuid"
)

type System struct {
	handler    *api.WebSocketHandler
	logService *service.LogService
}

func NewSystem(handler *api.WebSocketHandler, logService *service.LogService) *System {
	return &System{
		handler:    handler,
		logService: logService,
	}
}

// Logs streams backend logs to the client
func (sys *System) Logs(s *api.WebsocketSession, reqID string, _ *apiproto.Empty) {
	userID, _ := s.GetAuth()
	if userID == "" {
		sys.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	subID := uuid.New().String()
	ch, initialBuffer := sys.logService.Subscribe(subID)
	defer sys.logService.Unsubscribe(subID)

	// Send initial buffer
	for _, line := range initialBuffer {
		if s.IsClosed() {
			return
		}
		sys.handler.SendResponse(s, reqID, &apiproto.SystemLogChunk{Line: line}, false)
	}

	// Stream new logs
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				sys.handler.SendResponse(s, reqID, nil, true)
				return
			}
			if s.IsClosed() {
				return
			}
			sys.handler.SendResponse(s, reqID, &apiproto.SystemLogChunk{Line: line}, false)
		}
	}
}
