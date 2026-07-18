package api

import (
	"context"
	"encoding/hex"
	"log"
	"net/http"
	"reflect"
	"sync"

	apiproto "github.com/genazt/my-budget-script/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/backend/internal/bus"
	"github.com/genazt/my-budget-script/backend/internal/service"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
	"google.golang.org/protobuf/proto"
)

type activeReq struct {
	ctx    context.Context
	cancel context.CancelFunc
}

type WebsocketSession struct {
	ws         *websocket.Conn
	writeMutex sync.Mutex
	stateMu    sync.RWMutex
	userID     string
	tokenStr   string
	closed     bool
	remoteIP   string
	activeReqs map[string]activeReq
}

func (s *WebsocketSession) StartRequest(reqID string) context.Context {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.activeReqs == nil {
		s.activeReqs = make(map[string]activeReq)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.activeReqs[reqID] = activeReq{ctx: ctx, cancel: cancel}
	return ctx
}

func (s *WebsocketSession) FinishRequest(reqID string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.activeReqs != nil {
		if req, ok := s.activeReqs[reqID]; ok {
			req.cancel()
			delete(s.activeReqs, reqID)
		}
	}
}

func (s *WebsocketSession) CancelRequest(reqID string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.activeReqs != nil {
		if req, ok := s.activeReqs[reqID]; ok {
			req.cancel()
			delete(s.activeReqs, reqID)
		}
	}
}

func (s *WebsocketSession) IsRequestCancelled(reqID string) bool {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	if s.closed {
		return true
	}
	if s.activeReqs == nil {
		return false
	}
	req, ok := s.activeReqs[reqID]
	if !ok {
		return false
	}
	select {
	case <-req.ctx.Done():
		return true
	default:
		return false
	}
}

func (s *WebsocketSession) SetAuth(userID string, tokenStr string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()

	s.userID = userID
	s.tokenStr = tokenStr
}

func (s *WebsocketSession) GetAuth() (string, string) {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()

	return s.userID, s.tokenStr
}

func (s *WebsocketSession) RemoteAddr() string {
	return s.remoteIP
}

func (s *WebsocketSession) IsClosed() bool {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()

	return s.closed
}

type WebSocketHandler struct {
	eventBus   *bus.Bus
	Projection *service.ProjectionService
	registry   *WSRegistry
	mu         sync.RWMutex
	sessions   map[string]*WebsocketSession // sessionID -> session
}

func NewWebSocketHandler(eventBus *bus.Bus, projection *service.ProjectionService) *WebSocketHandler {
	h := &WebSocketHandler{
		eventBus:   eventBus,
		Projection: projection,
		registry:   NewWSRegistry(),
		sessions:   make(map[string]*WebsocketSession),
	}

	return h
}

func (w *WebSocketHandler) Register(apis ...interface{}) {
	for _, api := range apis {
		w.registry.Register(api) // calling your reflection engine
	}
}

func (h *WebSocketHandler) WebSocketGateway(c echo.Context) error {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)

	if err != nil {
		log.Printf("[WS] Upgrade error: %v", err)
		return err
	}

	session := &WebsocketSession{
		ws:         ws,
		closed:     false,
		remoteIP:   c.RealIP(),
		activeReqs: make(map[string]activeReq),
	}

	sessionID := uuid.New().String()
	h.mu.Lock()
	h.sessions[sessionID] = session
	h.mu.Unlock()
	log.Printf("[WS] Registered unauthenticated session %s", sessionID)

	defer func() {
		ws.Close()
		h.mu.Lock()

		if _, exists := h.sessions[sessionID]; exists {
			delete(h.sessions, sessionID)

			session.stateMu.Lock()
			session.closed = true
			if session.activeReqs != nil {
				for _, req := range session.activeReqs {
					req.cancel()
				}
				session.activeReqs = nil
			}
			session.stateMu.Unlock()

			log.Printf("[WS] Unregistered session %s", sessionID)
		}

		h.mu.Unlock()
	}()

	for {
		_, message, err := ws.ReadMessage()
		if err != nil {
			break
		}

		var req apiproto.WSRequest
		if err := proto.Unmarshal(message, &req); err != nil {
			log.Printf("[WS] Unmarshal message error: %v", err)
			continue
		}

		log.Printf("[WS] Incoming Request: %s (ID: %s) bytes: %d, data(hex): %s", req.Path, req.Id, len(message), hex.EncodeToString(message))

		if req.Path == "cancel" {
			log.Printf("[WS] Cancel Request received for ID: %s", req.Id)
			session.CancelRequest(req.Id)
			continue
		}

		session.StartRequest(req.Id)

		go func(req apiproto.WSRequest) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[WS] PANIC recovered in path %s: %v", req.Path, r)
					h.SendError(session, req.Id, http.StatusInternalServerError, "Internal Server Error: Panic occurred")
				}
			}()

			// 1. Check logical namespaced registry first
			if registration, ok := h.registry.Get(req.Path); ok {
				var reqBodyVal reflect.Value

				// If a request type is expected and we received a body
				if registration.RequestType != nil && len(req.Body) > 0 {
					var newReqStruct reflect.Value
					if registration.RequestType.Kind() == reflect.Ptr {
						// registration.RequestType is a pointer type (e.g., *apiproto.BillList), so Elem() gets the actual struct
						newReqStruct = reflect.New(registration.RequestType.Elem())
					} else {
						// It's likely an interface like proto.Message. We can't use reflect.New on an interface to get a concrete struct
						// unless we know what concrete type to instantiate.
						log.Printf("[WS] Error: RequestType for %s is not a pointer, it is %v. Handlers must use concrete proto pointer types.", req.Path, registration.RequestType)
						h.SendError(session, req.Id, http.StatusInternalServerError, "Internal Server Error: Invalid handler signature")
						return
					}

					reqBody := newReqStruct.Interface().(proto.Message)

					if err := proto.Unmarshal(req.Body, reqBody); err != nil {
						log.Printf("[WS] Body unmarshal error for %s: %v", req.Path, err)
						h.SendError(session, req.Id, http.StatusBadRequest, "Invalid request body")
						return
					}
					reqBodyVal = newReqStruct
					log.Printf("[WS] Handled with body: %T", reqBody)
				} else {
					// If no body is provided, ensure we pass a non-nil pointer to an empty struct
					// if the handler expects a pointer type (which all currently do).
					if registration.RequestType != nil && registration.RequestType.Kind() == reflect.Ptr {
						reqBodyVal = reflect.New(registration.RequestType.Elem())
					} else {
						reqBodyVal = reflect.Zero(registration.RequestType)
					}
					log.Printf("[WS] Handled without body (RequestType: %v)", registration.RequestType)
				}

				// 2. Invoke the method dynamically via Reflection
				// Go methods require the Receiver instance as the VERY first element in the arguments slice.
				args := []reflect.Value{
					registration.Receiver,    // The saved *Bills instance (In(0))
					reflect.ValueOf(session), // The websocket session     (In(1))
					reflect.ValueOf(req.Id),  // The request ID string     (In(2))
					reqBodyVal,               // The unmarshaled proto body (In(3))
				}

				// This calls your method (e.g., func (b *Bills) List(...))
				registration.HandlerFunc.Call(args)
				return
			}

			// Path not registered and fallback removed
			h.SendError(session, req.Id, http.StatusNotFound, "Handler not found for path: "+req.Path)
		}(req)
	}

	return nil
}

func (h *WebSocketHandler) SendError(session *WebsocketSession, reqID string, status int, message string) {
	log.Printf("[WS] Sending Error for %s: %d %s", reqID, status, message)
	resp := &apiproto.Error{
		Code:    int32(status),
		Message: message,
	}

	h.SendResponse(session, reqID, resp, true)
}

func (h *WebSocketHandler) SendResponse(session *WebsocketSession, reqID string, body proto.Message, done bool) error {
	if done {
		session.FinishRequest(reqID)
	}

	var bodyBytes []byte
	if body != nil {
		bodyBytes, _ = proto.Marshal(body)
	}

	if done {
		log.Printf("[WS] Sending Final Response for %s (Done: %v)", reqID, done)
	}

	resp := &apiproto.WSResponse{
		Id:   reqID,
		Data: bodyBytes,
		Done: done,
	}

	bytes, _ := proto.Marshal(resp)
	log.Printf("[WS] SendResponse: ID: %s, payload: %T, bytes: %d, done: %v, data(hex): %s", reqID, body, len(bytes), done, hex.EncodeToString(bytes))
	session.writeMutex.Lock()
	defer session.writeMutex.Unlock()

	if session.closed {
		return http.ErrHandlerTimeout
	}

	return session.ws.WriteMessage(websocket.BinaryMessage, bytes)
}

func (h *WebSocketHandler) BroadcastEvent(event string, body proto.Message) {
	var bodyBytes []byte
	if body != nil {
		bodyBytes, _ = proto.Marshal(body)
	}

	eventWrapper := &apiproto.EventWrapper{
		Event: event,
		Data:  bodyBytes,
	}

	bytes, _ := proto.Marshal(eventWrapper)

	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, session := range h.sessions {
		go func(s *WebsocketSession) {
			s.writeMutex.Lock()
			defer s.writeMutex.Unlock()

			s.stateMu.RLock()
			closed := s.closed
			s.stateMu.RUnlock()

			if closed {
				return
			}

			s.ws.WriteMessage(websocket.BinaryMessage, bytes)
		}(session)
	}
}
