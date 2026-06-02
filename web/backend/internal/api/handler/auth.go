package handler

import (
	"encoding/json"
	"log"
	mrand "math/rand"
	"net/http"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/web/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	"github.com/genazt/my-budget-script/web/backend/internal/service"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// Auth acts as the root namespace -> "auth"
type Auth struct {
	handler *api.WebSocketHandler

	users *repository.UserRepository

	// Needed objects for WebAuthn
	jwtKey []byte
	wauth  *webauthn.WebAuthn

	// Nested structs establish sub-namespaces: -> "auth::register" and "auth::login"
	Register *AuthRegister
	Login    *AuthLogin
	Recovery *AuthRecovery
}

func NewAuth(handler *api.WebSocketHandler, users *repository.UserRepository, wauth *webauthn.WebAuthn, sessions *repository.SessionRepository, sync *service.SyncService) *Auth {
	jwtKey := []byte("internal-secret-key")

	return &Auth{
		handler:  handler,
		users:    users,
		wauth:    wauth,
		jwtKey:   jwtKey,
		Register: &AuthRegister{handler: handler, users: users, sessions: sessions, wauth: wauth, jwtKey: jwtKey},
		Login:    &AuthLogin{handler: handler, users: users, sessions: sessions, wauth: wauth, jwtKey: jwtKey},
		Recovery: &AuthRecovery{handler: handler, users: users, sync: sync, jwtKey: jwtKey},
	}
}

// Handshake lives right on the root namespace -> "auth::handshake"
func (a *Auth) Handshake(s *api.WebsocketSession, reqID string, req *apiproto.AuthHandshakeRequest) {
	token, err := jwt.Parse(req.Token, func(token *jwt.Token) (interface{}, error) {
		return a.jwtKey, nil
	})

	if err != nil || !token.Valid {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "invalid token")
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "invalid claims")
		return
	}

	userID, ok := claims["sub"].(string)
	if !ok || userID == "" {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "invalid user id")
		return
	}

	user, err := a.users.GetUserByID(userID)
	if err != nil || user == nil {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "user not found")
		return
	}

	scope, _ := claims["scope"].(string)
	if scope == "" {
		scope = "FULL"
	}

	s.SetAuth(user.ID, req.Token)
	a.handler.SendResponse(s, reqID, mapAuthSuccessResponse(user, req.Token, scope), true)
}

// ==========================================
// Sub-Namespace: auth::register
// ==========================================
type AuthRegister struct {
	handler *api.WebSocketHandler
	wauth   *webauthn.WebAuthn
	jwtKey  []byte

	users    *repository.UserRepository
	sessions *repository.SessionRepository
}

// Begin maps to -> "auth::register::begin"
func (r *AuthRegister) Begin(s *api.WebsocketSession, reqID string, req *apiproto.AuthBeginRequest) {
	if req.Username == "" {
		r.handler.SendError(s, reqID, http.StatusBadRequest, "username is required")
		return
	}

	user, err := r.users.GetUser(req.Username)
	if err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}
	if user != nil {
		r.handler.SendError(s, reqID, http.StatusConflict, "username already taken")
		return
	}

	user = &domain.User{
		ID:       uuid.New().String(),
		Username: req.Username,
	}

	options, session, err := r.wauth.BeginRegistration(user)
	if err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	if err := r.sessions.SaveSession(req.Username, &repository.AuthSession{
		WebAuthnSession: session,
		User:            user,
	}); err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, "failed to save session")
		return
	}

	optionsJSON, _ := json.Marshal(options)
	resp := &apiproto.AuthBeginResponse{
		UserId:  user.ID,
		Options: optionsJSON,
	}
	r.handler.SendResponse(s, reqID, resp, true)
}

// BeginAdd maps to -> "auth::register::begin_add"
func (r *AuthRegister) BeginAdd(s *api.WebsocketSession, reqID string, req *apiproto.AuthBeginRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		r.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	user, err := r.users.GetUserByID(userID)
	if err != nil || user == nil {
		r.handler.SendError(s, reqID, http.StatusNotFound, "User not found")
		return
	}

	options, session, err := r.wauth.BeginRegistration(user)
	if err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	if err := r.sessions.SaveSession(user.Username, &repository.AuthSession{
		WebAuthnSession: session,
		User:            user,
	}); err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, "failed to save session")
		return
	}

	optionsJSON, _ := json.Marshal(options)
	resp := &apiproto.AuthBeginResponse{
		UserId:  user.ID,
		Options: optionsJSON,
	}
	r.handler.SendResponse(s, reqID, resp, true)
}

// Finish maps to -> "auth::register::finish"
func (r *AuthRegister) Finish(s *api.WebsocketSession, reqID string, req *apiproto.AuthFinishRequest) {
	authSession, err := r.sessions.GetSession(req.Username)
	if err != nil || authSession == nil {
		r.handler.SendError(s, reqID, http.StatusBadRequest, "no session found")
		return
	}

	user, err := r.users.GetUser(req.Username)
	if err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}
	if user != nil {
		r.handler.SendError(s, reqID, http.StatusConflict, "user already exists")
		return
	}

	user = authSession.User
	if user == nil || user.ID != req.UserId {
		r.handler.SendError(s, reqID, http.StatusForbidden, "user context lost or id mismatch")
		return
	}

	parsedCredential, err := protocol.ParseCredentialCreationResponseBytes(req.WebauthnPayload)
	if err != nil {
		r.handler.SendError(s, reqID, http.StatusBadRequest, err.Error())
		return
	}

	credential, err := r.wauth.CreateCredential(user, *authSession.WebAuthnSession, parsedCredential)
	if err != nil {
		r.handler.SendError(s, reqID, http.StatusBadRequest, err.Error())
		return
	}

	if err := r.users.CreateUser(user); err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	if err := r.users.AddCredential(user.ID, credential, "Initial Passkey"); err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	r.sessions.DeleteSession(req.Username)

	token, err := issueTokenForWS(r.jwtKey, user, "FULL")
	if err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	s.SetAuth(user.ID, token)
	r.handler.SendResponse(s, reqID, mapAuthSuccessResponse(user, token, "FULL"), true)
}

// FinishAdd maps to -> "auth::register::finish_add"
func (r *AuthRegister) FinishAdd(s *api.WebsocketSession, reqID string, req *apiproto.AuthFinishRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		r.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	authSession, err := r.sessions.GetSession(req.Username)
	if err != nil || authSession == nil {
		r.handler.SendError(s, reqID, http.StatusBadRequest, "no session found")
		return
	}

	user, err := r.users.GetUserByID(userID)
	if err != nil || user == nil {
		r.handler.SendError(s, reqID, http.StatusNotFound, "User not found")
		return
	}

	if user.Username != req.Username {
		r.handler.SendError(s, reqID, http.StatusForbidden, "username mismatch")
		return
	}

	parsedCredential, err := protocol.ParseCredentialCreationResponseBytes(req.WebauthnPayload)
	if err != nil {
		r.handler.SendError(s, reqID, http.StatusBadRequest, err.Error())
		return
	}

	credential, err := r.wauth.CreateCredential(user, *authSession.WebAuthnSession, parsedCredential)
	if err != nil {
		r.handler.SendError(s, reqID, http.StatusBadRequest, err.Error())
		return
	}

	if err := r.users.AddCredential(user.ID, credential, "New Passkey"); err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	r.sessions.DeleteSession(req.Username)

	// Refresh token in case of RECOVERY upgrade
	token, err := issueTokenForWS(r.jwtKey, user, "FULL")
	if err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	s.SetAuth(user.ID, token)
	r.handler.SendResponse(s, reqID, mapAuthSuccessResponse(user, token, "FULL"), true)
}

// ==========================================
// Sub-Namespace: auth::login
// ==========================================
type AuthLogin struct {
	handler *api.WebSocketHandler
	wauth   *webauthn.WebAuthn
	jwtKey  []byte

	users    *repository.UserRepository
	sessions *repository.SessionRepository
}

// Begin maps to -> "auth::login::begin"
func (l *AuthLogin) Begin(s *api.WebsocketSession, reqID string, req *apiproto.AuthBeginRequest) {
	user, err := l.users.GetUser(req.Username)
	if err != nil || user == nil {
		l.handler.SendError(s, reqID, http.StatusNotFound, "user not found")
		return
	}

	options, session, err := l.wauth.BeginLogin(user)
	if err != nil {
		l.handler.SendError(s, reqID, http.StatusInternalServerError, "failed to initiate authentication")
		return
	}

	if err := l.sessions.SaveSession(req.Username, &repository.AuthSession{
		WebAuthnSession: session,
		User:            user,
	}); err != nil {
		l.handler.SendError(s, reqID, http.StatusInternalServerError, "failed to save session")
		return
	}

	optionsJSON, _ := json.Marshal(options)
	resp := &apiproto.AuthBeginResponse{
		UserId:  user.ID,
		Options: optionsJSON,
	}
	l.handler.SendResponse(s, reqID, resp, true)
}

// Finish maps to -> "auth::login::finish"
func (l *AuthLogin) Finish(s *api.WebsocketSession, reqID string, req *apiproto.AuthFinishRequest) {
	authSession, err := l.sessions.GetSession(req.Username)
	if err != nil || authSession == nil {
		l.handler.SendError(s, reqID, http.StatusBadRequest, "no session found")
		return
	}

	user, err := l.users.GetUser(req.Username)
	if err != nil || user == nil {
		l.handler.SendError(s, reqID, http.StatusNotFound, "user not found")
		return
	}

	parsedCredential, err := protocol.ParseCredentialRequestResponseBytes(req.WebauthnPayload)
	if err != nil {
		l.handler.SendError(s, reqID, http.StatusBadRequest, err.Error())
		return
	}

	// Debug logging for WebAuthn failure
	log.Printf("[AUTH] Validating login for user: %s", req.Username)
	log.Printf("[AUTH] Received Origin: %s", parsedCredential.Response.CollectedClientData.Origin)
	log.Printf("[AUTH] User has %d registered authenticators", len(user.Authenticators))

	credential, err := l.wauth.ValidateLogin(user, *authSession.WebAuthnSession, parsedCredential)
	if err != nil {
		log.Printf("[AUTH] WebAuthn validation failed: %v", err)
		log.Printf("[AUTH] Server expected RPID: %s", l.wauth.Config.RPID)
		log.Printf("[AUTH] Server expected Origins: %v", l.wauth.Config.RPOrigins)
		l.handler.SendError(s, reqID, http.StatusBadRequest, err.Error())
		return
	}

	l.users.UpdateCredential(user.ID, credential)
	l.sessions.DeleteSession(req.Username)

	token, err := issueTokenForWS(l.jwtKey, user, "FULL")
	if err != nil {
		l.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	s.SetAuth(user.ID, token)
	l.handler.SendResponse(s, reqID, mapAuthSuccessResponse(user, token, "FULL"), true)
}

// ==========================================
// Sub-Namespace: auth::recovery
// ==========================================
type AuthRecovery struct {
	handler *api.WebSocketHandler
	jwtKey  []byte

	users *repository.UserRepository
	sync  *service.SyncService
}

// Login maps to -> "auth::recovery::login"
func (r *AuthRecovery) Login(s *api.WebsocketSession, reqID string, req *apiproto.AuthRecoveryRequest) {
	user, err := r.users.GetUser(req.Username)
	if err != nil || user == nil {
		r.handler.SendError(s, reqID, http.StatusNotFound, "user not found")
		return
	}

	if user.RecoveryHash == "" {
		r.handler.SendError(s, reqID, http.StatusForbidden, "recovery not configured")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.RecoveryHash), []byte(req.Token)); err != nil {
		r.handler.SendError(s, reqID, http.StatusUnauthorized, "invalid recovery token")
		return
	}

	chars := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	newToken := "MB-"
	for i := 0; i < 3; i++ {
		for j := 0; j < 4; j++ {
			newToken += string(chars[mrand.Intn(len(chars))])
		}
		if i < 2 {
			newToken += "-"
		}
	}

	log.Printf("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
	log.Printf("!!! RECOVERY TOKEN ROTATED FOR %s: %s !!!", user.Username, newToken)
	log.Printf("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")

	r.sync.RecoverMIKsToCache(user.ID, req.Token)

	newHash, _ := bcrypt.GenerateFromPassword([]byte(newToken), bcrypt.DefaultCost)
	r.users.UpdateRecoveryHash(user.ID, string(newHash))
	r.sync.SetupRecoveryKey(user.ID, newToken)

	token, err := issueTokenForWS(r.jwtKey, user, "RECOVERY")
	if err != nil {
		r.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	s.SetAuth(user.ID, token)
	r.handler.SendResponse(s, reqID, mapAuthSuccessResponse(user, token, "RECOVERY"), true)
}

// Global Mappers kept identical
func issueTokenForWS(jwtKey []byte, user *domain.User, scope string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   user.ID,
		"name":  user.Username,
		"scope": scope,
		"exp":   time.Now().Add(time.Hour * 72).Unix(),
	})
	return token.SignedString(jwtKey)
}

func mapAuthSuccessResponse(user *domain.User, token string, scope string) *apiproto.AuthSuccessResponse {
	return &apiproto.AuthSuccessResponse{
		Status:               "success",
		Token:                token,
		Username:             user.Username,
		Id:                   user.ID,
		DashboardScenarioId:  user.DashboardScenarioID,
		DashboardMonthOffset: int32(user.DashboardMonthOffset),
		Scope:                scope,
	}
}
