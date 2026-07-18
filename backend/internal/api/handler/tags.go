package handler

import (
	"net/http"

	"github.com/genazt/my-budget-script/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/backend/internal/repository"
)

type Tags struct {
	handler *api.WebSocketHandler
	repo    *repository.TagRepository
}

func NewTags(handler *api.WebSocketHandler, repo *repository.TagRepository) *Tags {
	return &Tags{
		handler: handler,
		repo:    repo,
	}
}

// List automatically registers as "tags::list"
func (t *Tags) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		t.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	tags, err := t.repo.List(userID)
	if err != nil {
		t.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.AvailableTagList{}
	for _, tag := range tags {
		resp.Tags = append(resp.Tags, &apiproto.AvailableTag{
			Name: tag,
		})
	}

	t.handler.SendResponse(s, reqID, resp, true)
}

// Create automatically registers as "tags::create"
func (t *Tags) Create(s *api.WebsocketSession, reqID string, req *apiproto.AvailableTagCreateRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		t.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if req.Name == "" {
		t.handler.SendError(s, reqID, http.StatusBadRequest, "Tag name cannot be empty")
		return
	}

	err := t.repo.Create(userID, req.Name)
	if err != nil {
		t.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	t.handler.SendResponse(s, reqID, &apiproto.AvailableTag{
		Name: req.Name,
	}, true)
}
