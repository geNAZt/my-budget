package handler

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/web/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/web/backend/internal/service"
	"github.com/google/uuid"
)

type System struct {
	handler       *api.WebSocketHandler
	logService    *service.LogService
	dockerService *service.DockerService
}

func NewSystem(handler *api.WebSocketHandler, logService *service.LogService, dockerService *service.DockerService) *System {
	return &System{
		handler:       handler,
		logService:    logService,
		dockerService: dockerService,
	}
}

// Containers lists available containers in the system
func (sys *System) Containers(s *api.WebsocketSession, reqID string, _ *apiproto.Empty) {
	userID, _ := s.GetAuth()
	if userID == "" {
		sys.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	ctx := context.Background()
	var list apiproto.ContainerList

	if sys.dockerService.IsAvailable() {
		containers, err := sys.dockerService.ListContainers(ctx)
		if err == nil {
			for _, c := range containers {
				name := ""
				if len(c.Names) > 0 {
					name = c.Names[0]
				}
				list.Containers = append(list.Containers, &apiproto.ContainerInfo{
					Id:     c.ID,
					Name:   name,
					State:  c.State,
					Status: c.Status,
				})
			}
		}
	}

	sys.handler.SendResponse(s, reqID, &list, false)
}

// Logs streams logs to the client based on selected container
func (sys *System) Logs(s *api.WebsocketSession, reqID string, req *apiproto.SystemLogRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		sys.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	containerID := ""
	if req != nil {
		containerID = req.ContainerId
	}

	// Case 1: Stream standard backend logs using current process LogService
	if containerID == "" || containerID == "current" {
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

	// Case 2: Stream logs from a Docker container
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logChan := make(chan string, 1000)

	// Start streaming docker logs in a goroutine
	go func() {
		defer close(logChan)
		err := sys.dockerService.StreamLogs(ctx, containerID, logChan)
		if err != nil {
			select {
			case logChan <- fmt.Sprintf("[Docker Logs Error] %v", err):
			case <-ctx.Done():
			}
		}
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case line, ok := <-logChan:
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
			// Heartbeat
			if s.IsClosed() {
				return
			}
			if err := sys.handler.SendResponse(s, reqID, &apiproto.SystemLogChunk{}, false); err != nil {
				return
			}
		}
	}
}

