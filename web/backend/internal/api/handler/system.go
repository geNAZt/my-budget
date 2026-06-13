package handler

import (
	"net/http"
	"time"

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

	// Send initial buffer in one batch for performance
	if len(initialBuffer) > 0 {
		if s.IsClosed() {
			return
		}
		if err := sys.handler.SendResponse(s, reqID, &apiproto.SystemLogChunk{Lines: initialBuffer}, false); err != nil {
			return
		}
	}

	// Stream new logs with heartbeat
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

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
			if err := sys.handler.SendResponse(s, reqID, &apiproto.SystemLogChunk{Line: line}, false); err != nil {
				return
			}
		case <-ticker.C:
			// Heartbeat to keep connection alive and detect dead sessions
			if s.IsClosed() {
				return
			}
			if err := sys.handler.SendResponse(s, reqID, &apiproto.SystemLogChunk{}, false); err != nil {
				return
			}
		}
	}
}
