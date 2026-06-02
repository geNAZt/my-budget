package handler

import (
	crand "crypto/rand"
	"encoding/base64"
	"log"
	"math/big"
	"net/http"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/web/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	"github.com/genazt/my-budget-script/web/backend/internal/service"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	handler     *api.WebSocketHandler
	repo        *repository.UserRepository
	syncService *service.SyncService

	Profile *UserProfile
}

func NewUser(handler *api.WebSocketHandler, repo *repository.UserRepository, sync *service.SyncService) *User {
	return &User{
		handler:     handler,
		repo:        repo,
		syncService: sync,
		Profile: &UserProfile{
			handler: handler,
			repo:    repo,
			sync:    sync,
		},
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

type UserProfile struct {
	handler *api.WebSocketHandler
	repo    *repository.UserRepository
	sync    *service.SyncService
}

func (p *UserProfile) Get(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		p.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	user, err := p.repo.GetUserByID(userID)
	if err != nil || user == nil {
		p.handler.SendError(s, reqID, http.StatusNotFound, "User not found")
		return
	}

	resp := &apiproto.UserProfile{
		Id:       user.ID,
		Username: user.Username,
		Timezone: user.Timezone,
	}

	for _, cred := range user.Authenticators {
		idB64 := base64.StdEncoding.EncodeToString(cred.ID)
		name := user.AuthenticatorNames[idB64]
		if name == "" {
			name = "Unnamed Passkey"
		}

		resp.Authenticators = append(resp.Authenticators, &apiproto.UserAuthenticator{
			Id:        idB64,
			Name:      name,
			CreatedAt: user.AuthenticatorCreatedAt[idB64].Format("2006-01-02 15:04:05"),
		})
	}

	p.handler.SendResponse(s, reqID, resp, true)
}

func (p *UserProfile) Update(s *api.WebsocketSession, reqID string, req *apiproto.UpdateProfileRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		p.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := p.repo.UpdateTimezone(userID, req.Timezone); err != nil {
		p.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	p.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: userID}, true)
}

func (p *UserProfile) RenameAuthenticator(s *api.WebsocketSession, reqID string, req *apiproto.RenameAuthenticatorRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		p.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idBytes, err := base64.StdEncoding.DecodeString(req.Id)
	if err != nil {
		p.handler.SendError(s, reqID, http.StatusBadRequest, "invalid authenticator id")
		return
	}

	if err := p.repo.RenameAuthenticator(userID, idBytes, req.Name); err != nil {
		p.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	p.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: req.Id}, true)
}

func (p *UserProfile) DeleteAuthenticator(s *api.WebsocketSession, reqID string, req *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		p.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	idBytes, err := base64.StdEncoding.DecodeString(req.Id)
	if err != nil {
		p.handler.SendError(s, reqID, http.StatusBadRequest, "invalid authenticator id")
		return
	}

	// Check if user has more than one authenticator to prevent lockout
	user, err := p.repo.GetUserByID(userID)
	if err == nil && len(user.Authenticators) <= 1 {
		p.handler.SendError(s, reqID, http.StatusForbidden, "Cannot delete last remaining passkey. Add a new one first.")
		return
	}

	if err := p.repo.DeleteAuthenticator(userID, idBytes); err != nil {
		p.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	p.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: req.Id}, true)
}

func (p *UserProfile) CycleRecovery(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		p.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	user, err := p.repo.GetUserByID(userID)
	if err != nil || user == nil {
		p.handler.SendError(s, reqID, http.StatusNotFound, "User not found")
		return
	}

	// Generate new token: MB-XXXX-XXXX-XXXX
	chars := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	newToken := "MB-"
	for i := 0; i < 3; i++ {
		for j := 0; j < 4; j++ {
			idx, _ := crand.Int(crand.Reader, big.NewInt(int64(len(chars))))
			newToken += string(chars[idx.Int64()])
		}
		if i < 2 {
			newToken += "-"
		}
	}

	log.Printf("!!! RECOVERY TOKEN ROTATED FOR %s: %s !!!", user.Username, newToken)

	newHash, _ := bcrypt.GenerateFromPassword([]byte(newToken), bcrypt.DefaultCost)
	if err := p.repo.UpdateRecoveryHash(user.ID, string(newHash)); err != nil {
		p.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	if err := p.sync.SetupRecoveryKey(user.ID, newToken); err != nil {
		p.handler.SendError(s, reqID, http.StatusInternalServerError, "failed to setup recovery key: "+err.Error())
		return
	}

	p.handler.SendResponse(s, reqID, &apiproto.CycleRecoveryResponse{NewRecoveryToken: newToken}, true)
}
