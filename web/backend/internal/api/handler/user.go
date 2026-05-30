package handler

import (
	"net/http"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/web/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
)

type User struct {
	handler *api.WebSocketHandler
	repo    *repository.UserRepository
}

func NewUser(handler *api.WebSocketHandler, repo *repository.UserRepository) *User {
	return &User{
		handler: handler,
		repo:    repo,
	}
}

// Dashboard maps to route "user::dashboard"
func (u *User) Dashboard(s *api.WebsocketSession, reqID string, body *apiproto.Scenario) {
	userID, tokenStr := s.GetAuth()
	if userID == "" {
		u.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	scenarioID := body.Id

	user, err := u.repo.GetUserByID(userID)
	if err != nil || user == nil {
		u.handler.SendError(s, reqID, http.StatusNotFound, "User not found")
		return
	}

	// Keep month offset unchanged or default to current user value
	monthOffset := user.DashboardMonthOffset

	if err := u.repo.UpdateDashboardConfig(userID, scenarioID, monthOffset); err != nil {
		u.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	updatedUser, err := u.repo.GetUserByID(userID)
	if err != nil || updatedUser == nil {
		u.handler.SendError(s, reqID, http.StatusInternalServerError, "Failed to reload user")
		return
	}

	resp := &apiproto.AuthSuccessResponse{
		Status:               "success",
		Token:                tokenStr,
		Username:             updatedUser.Username,
		Id:                   updatedUser.ID,
		DashboardScenarioId:  updatedUser.DashboardScenarioID,
		DashboardMonthOffset: int32(updatedUser.DashboardMonthOffset),
		Scope:                "FULL",
	}

	u.handler.SendResponse(s, reqID, resp, true)
}
