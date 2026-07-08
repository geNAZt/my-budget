package handler

import (
	"net/http"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/web/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
)

// VirtualAccounts isolates everything relating to the "virtual_accounts::" namespace.
// The reflection registry maps methods on this struct to "virtual_accounts::[method_name]".
type VirtualAccounts struct {
	handler         *api.WebSocketHandler
	virtualAccounts *repository.VirtualAccountRepository
}

// NewVirtualAccounts instantiates the isolated Virtual Accounts handler namespace.
func NewVirtualAccounts(handler *api.WebSocketHandler, virtualAccounts *repository.VirtualAccountRepository) *VirtualAccounts {
	return &VirtualAccounts{
		handler:         handler,
		virtualAccounts: virtualAccounts,
	}
}

// List automatically registers as "virtual_accounts::list"
func (v *VirtualAccounts) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		v.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entities, err := v.virtualAccounts.List(userID)
	if err != nil {
		v.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.VirtualAccountList{}
	for _, e := range entities {
		resp.VirtualAccounts = append(resp.VirtualAccounts, mapVirtualAccountToProto(e))
	}

	v.handler.SendResponse(s, reqID, resp, true)
}

// Delete automatically registers as "virtualaccounts::delete"
func (v *VirtualAccounts) Delete(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		v.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := v.virtualAccounts.ArchiveFull(userID, body.Id); err != nil {
		v.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	v.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: body.Id}, true)
}

// Save automatically registers as "virtualaccounts::save"
func (v *VirtualAccounts) Save(s *api.WebsocketSession, reqID string, body *apiproto.VirtualAccount) {
	userID, _ := s.GetAuth()
	if userID == "" {
		v.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	va := &domain.VirtualAccount{
		ID:     body.Id,
		UserID: userID,
		Name:   body.Name,
	}

	if body.ActiveVersion != nil {
		va.ActiveVersion = &domain.VirtualAccountVersion{
			Color:             body.ActiveVersion.Color,
			StartingBalance:   body.ActiveVersion.StartingBalance,
			Description:       body.ActiveVersion.Description,
			RealtimeAccountID: body.ActiveVersion.RealtimeAccountId,
		}
	}

	if err := v.virtualAccounts.Save(userID, va); err != nil {
		v.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	v.handler.SendResponse(s, reqID, mapVirtualAccountToProto(*va), true)
}

func mapVirtualAccountToProto(va domain.VirtualAccount) *apiproto.VirtualAccount {
	pva := &apiproto.VirtualAccount{
		Id:        va.ID,
		UserId:    va.UserID,
		Name:      va.Name,
		IsDeleted: va.IsDeleted,
		CreatedAt: va.CreatedAt.Format(time.RFC3339),
	}
	if va.ActiveVersion != nil {
		pva.ActiveVersion = &apiproto.VirtualAccountVersion{
			Id:                va.ActiveVersion.ID,
			VirtualAccountId:  va.ActiveVersion.VirtualAccountID,
			Color:             va.ActiveVersion.Color,
			StartingBalance:   va.ActiveVersion.StartingBalance,
			Description:       va.ActiveVersion.Description,
			RealtimeAccountId: va.ActiveVersion.RealtimeAccountID,
			CreatedAt:         va.ActiveVersion.CreatedAt.Format(time.RFC3339),
		}
	}
	return pva
}
