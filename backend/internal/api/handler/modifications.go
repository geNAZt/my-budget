package handler

import (
	"net/http"
	"time"

	"github.com/genazt/my-budget-script/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/backend/internal/domain"
	"github.com/genazt/my-budget-script/backend/internal/repository"
)

// Modifications isolates everything relating to the "modifications::" namespace.
// The reflection registry maps methods on this struct to "modifications::[method_name]".
type Modifications struct {
	handler       *api.WebSocketHandler
	modifications *repository.ModificationRepository
}

// NewModifications instantiates the isolated Modifications handler namespace.
func NewModifications(handler *api.WebSocketHandler, modifications *repository.ModificationRepository) *Modifications {
	return &Modifications{
		handler:       handler,
		modifications: modifications,
	}
}

// List automatically registers as "modifications::list"
func (m *Modifications) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		m.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entities, err := m.modifications.List(userID)
	if err != nil {
		m.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.ModificationList{}
	for _, e := range entities {
		resp.Modifications = append(resp.Modifications, mapModificationToProto(e))
	}

	m.handler.SendResponse(s, reqID, resp, true)
}

// Save automatically registers as "modifications::save"
func (m *Modifications) Save(s *api.WebsocketSession, reqID string, reqObj *apiproto.Modification) {
	userID, _ := s.GetAuth()
	if userID == "" {
		m.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	domainObj := domain.Modification{
		ID:            reqObj.Id,
		UserID:        userID,
		TargetID:      reqObj.TargetId,
		TargetIDs:     reqObj.TargetIds,
		TargetType:    reqObj.TargetType,
		Description:   reqObj.Description,
		ActiveVersion: mapProtoToModificationVersion(reqObj.ActiveVersion),
	}

	if err := m.modifications.Save(userID, &domainObj); err != nil {
		m.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	m.handler.SendResponse(s, reqID, mapModificationToProto(domainObj), true)
}

// Delete automatically registers as "modifications::delete"
func (m *Modifications) Delete(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		m.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := m.modifications.ArchiveFull(userID, reqIDObj.Id); err != nil {
		m.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	m.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

func mapModificationToProto(m domain.Modification) *apiproto.Modification {
	pm := &apiproto.Modification{
		Id:          m.ID,
		UserId:      m.UserID,
		TargetId:    m.TargetID,
		TargetIds:   m.TargetIDs,
		TargetType:  m.TargetType,
		Description: m.Description,
		IsDeleted:   m.IsDeleted,
		CreatedAt:   m.CreatedAt.Format(time.RFC3339),
	}
	if m.ActiveVersion != nil {
		pm.ActiveVersion = &apiproto.ModificationVersion{
			Id:                   m.ActiveVersion.ID,
			ModificationId:       m.ActiveVersion.ModificationID,
			Amount:               m.ActiveVersion.Amount,
			WithdrawalPercentage: m.ActiveVersion.WithdrawalPercentage,
			StartDate:            m.ActiveVersion.StartDate.Format(time.RFC3339),
			IntervalMonths:       int32(m.ActiveVersion.IntervalMonths),
			CreatedAt:            m.ActiveVersion.CreatedAt.Format(time.RFC3339),
		}
		if m.ActiveVersion.EndDate != nil {
			pm.ActiveVersion.EndDate = m.ActiveVersion.EndDate.Format(time.RFC3339)
		}
	}
	return pm
}

func mapProtoToModificationVersion(pm *apiproto.ModificationVersion) *domain.ModificationVersion {
	if pm == nil {
		return nil
	}
	dmv := &domain.ModificationVersion{
		ID:                   pm.Id,
		ModificationID:       pm.ModificationId,
		Amount:               pm.Amount,
		WithdrawalPercentage: pm.WithdrawalPercentage,
		IntervalMonths:       int(pm.IntervalMonths),
	}
	if pm.StartDate != "" {
		if t, err := time.Parse(time.RFC3339, pm.StartDate); err == nil {
			dmv.StartDate = t
		}
	}
	if pm.EndDate != "" {
		if t, err := time.Parse(time.RFC3339, pm.EndDate); err == nil {
			dmv.EndDate = &t
		}
	}
	return dmv
}

