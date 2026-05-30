package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/web/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
)

// Assets handles websocket requests for the "assets" namespace.
// The reflection engine will register its methods automatically as "assets::method_name".
type Assets struct {
	handler      *api.WebSocketHandler
	repo         *repository.AssetRepository
	scenarioRepo *repository.ScenarioRepository
}

// NewAssets creates a new instance of the Assets API handler namespace
func NewAssets(handler *api.WebSocketHandler, repo *repository.AssetRepository, scenarioRepo *repository.ScenarioRepository) *Assets {
	return &Assets{
		handler:      handler,
		repo:         repo,
		scenarioRepo: scenarioRepo,
	}
}

// List maps to route "assets::list"
func (a *Assets) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	assets, err := a.repo.List(userID)
	if err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.AssetList{}
	for _, asset := range assets {
		resp.Assets = append(resp.Assets, mapAssetToProto(asset))
	}

	a.handler.SendResponse(s, reqID, resp, true)
}

func mapProtoToAssetVersion(reqVersion *apiproto.AssetVersion) *domain.AssetVersion {
	if reqVersion == nil {
		return nil
	}

	av := &domain.AssetVersion{
		ID:               reqVersion.Id,
		AssetID:          reqVersion.AssetId,
		Type:             domain.AssetType(reqVersion.Type),
		TargetValue:      strconv.FormatFloat(reqVersion.TargetValue, 'f', -1, 64),
		InterestRate:     reqVersion.InterestRate,
		InterestInterval: reqVersion.InterestInterval,
		AmountPerMonth:   reqVersion.AmountPerMonth,
	}

	if reqVersion.DumpingLoanId != "" {
		av.DumpingLoanID = &reqVersion.DumpingLoanId
	}

	if reqVersion.StopModificationId != "" {
		av.StopModificationID = &reqVersion.StopModificationId
	}

	if reqVersion.RemainderStartDate != "" {
		if t, err := time.Parse(time.RFC3339, reqVersion.RemainderStartDate); err == nil {
			av.RemainderStartDate = &t
		}
	}

	if reqVersion.StartDate != "" {
		if t, err := time.Parse(time.RFC3339, reqVersion.StartDate); err == nil {
			av.StartDate = t
		}
	}

	if reqVersion.EndDate != "" {
		if t, err := time.Parse(time.RFC3339, reqVersion.EndDate); err == nil {
			av.EndDate = &t
		}
	}

	for _, tracker := range reqVersion.EtfConfig {
		av.ETFConfig = append(av.ETFConfig, domain.ETFTracker{
			Tracker:    tracker.Tracker,
			Percentage: tracker.Percentage,
			TER:        tracker.Ter,
		})
	}

	for _, penalty := range reqVersion.Penalties {
		av.Penalties = append(av.Penalties, domain.AssetPenalty{
			Name:        penalty.Name,
			TriggerType: domain.PenaltyTriggerType(penalty.TriggerType),
			Percentage:  penalty.Percentage,
		})
	}

	for _, subAsset := range reqVersion.SubAssets {
		sa := domain.SubAsset{
			ID:                  subAsset.Id,
			Name:                subAsset.Name,
			TargetValue:         strconv.FormatFloat(subAsset.TargetValue, 'f', -1, 64),
			AmountPerMonth:      subAsset.AmountPerMonth,
			IsRemainderConsumer: subAsset.IsRemainderConsumer,
		}

		if subAsset.RemainderStartDate != "" {
			if t, err := time.Parse(time.RFC3339, subAsset.RemainderStartDate); err == nil {
				sa.RemainderStartDate = &t
			}
		}

		if subAsset.DumpingLoanId != "" {
			sa.DumpingLoanID = &subAsset.DumpingLoanId
		}

		if subAsset.StartDate != "" {
			if t, err := time.Parse(time.RFC3339, subAsset.StartDate); err == nil {
				sa.StartDate = t
			}
		}

		if subAsset.EndDate != "" {
			if t, err := time.Parse(time.RFC3339, subAsset.EndDate); err == nil {
				sa.EndDate = &t
			}
		}

		if subAsset.EarliestDumpDate != "" {
			if t, err := time.Parse(time.RFC3339, subAsset.EarliestDumpDate); err == nil {
				sa.EarliestDumpDate = &t
			}
		}

		av.SubAssets = append(av.SubAssets, sa)
	}

	return av
}

// Save maps to route "assets::save"
func (a *Assets) Save(s *api.WebsocketSession, reqID string, reqAsset *apiproto.Asset) {
	userID, _ := s.GetAuth()
	if userID == "" {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	domainAsset := domain.Asset{
		ID:              reqAsset.Id,
		UserID:          userID,
		Name:            reqAsset.Name,
		AccountIDs:      reqAsset.AccountIds,
		LinkToScenarios: reqAsset.LinkToScenarios,
		ActiveVersion:   mapProtoToAssetVersion(reqAsset.ActiveVersion),
	}
	if reqAsset.PoolId != "" {
		domainAsset.PoolID = &reqAsset.PoolId
	}

	if err := a.repo.Save(userID, &domainAsset); err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	if len(domainAsset.LinkToScenarios) > 0 {
		a.scenarioRepo.LinkEntityToScenarios(userID, domainAsset.ID, "ASSET", domainAsset.LinkToScenarios)
	}

	a.handler.SendResponse(s, reqID, mapAssetToProto(domainAsset), true)
}

// SaveBulk maps to route "assets::save_bulk"
func (a *Assets) SaveBulk(s *api.WebsocketSession, reqID string, reqList *apiproto.AssetList) {
	userID, _ := s.GetAuth()
	if userID == "" {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var domainAssets []domain.Asset

	for _, reqAsset := range reqList.Assets {
		domainAsset := domain.Asset{
			ID:              reqAsset.Id,
			UserID:          userID,
			Name:            reqAsset.Name,
			AccountIDs:      reqAsset.AccountIds,
			LinkToScenarios: reqAsset.LinkToScenarios,
			ActiveVersion:   mapProtoToAssetVersion(reqAsset.ActiveVersion),
		}
		if reqAsset.PoolId != "" {
			domainAsset.PoolID = &reqAsset.PoolId
		}
		domainAssets = append(domainAssets, domainAsset)
	}

	if err := a.repo.SaveBulk(userID, domainAssets); err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.AssetList{}
	for _, asset := range domainAssets {
		if len(asset.LinkToScenarios) > 0 {
			a.scenarioRepo.LinkEntityToScenarios(userID, asset.ID, "ASSET", asset.LinkToScenarios)
		}
		resp.Assets = append(resp.Assets, mapAssetToProto(asset))
	}
	a.handler.SendResponse(s, reqID, resp, true)
}

// Delete maps to route "assets::delete"
func (a *Assets) Delete(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := a.repo.ArchiveFull(userID, reqIDObj.Id); err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	a.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

// Helper mapper function kept identical
func mapAssetToProto(a domain.Asset) *apiproto.Asset {
	pa := &apiproto.Asset{
		Id:              a.ID,
		UserId:          a.UserID,
		Name:            a.Name,
		IsDeleted:       a.IsDeleted,
		AccountIds:      a.AccountIDs,
		CreatedAt:       a.CreatedAt.Format(time.RFC3339),
		LinkToScenarios: a.LinkToScenarios,
	}

	if a.PoolID != nil {
		pa.PoolId = *a.PoolID
	}

	if a.ActiveVersion != nil {
		targetValue, _ := strconv.ParseFloat(a.ActiveVersion.TargetValue, 64)
		pa.ActiveVersion = &apiproto.AssetVersion{
			Id:                 a.ActiveVersion.ID,
			AssetId:            a.ActiveVersion.AssetID,
			Type:               string(a.ActiveVersion.Type),
			TargetValue:        targetValue,
			DumpingLoanId:      "",
			StopModificationId: "",
			InterestRate:       a.ActiveVersion.InterestRate,
			InterestInterval:   a.ActiveVersion.InterestInterval,
			AmountPerMonth:     a.ActiveVersion.AmountPerMonth,
			CreatedAt:          a.ActiveVersion.CreatedAt.Format(time.RFC3339),
			StartDate:          a.ActiveVersion.StartDate.Format(time.RFC3339),
		}

		if a.ActiveVersion.DumpingLoanID != nil {
			pa.ActiveVersion.DumpingLoanId = *a.ActiveVersion.DumpingLoanID
		}

		if a.ActiveVersion.StopModificationID != nil {
			pa.ActiveVersion.StopModificationId = *a.ActiveVersion.StopModificationID
		}

		if a.ActiveVersion.RemainderStartDate != nil {
			pa.ActiveVersion.RemainderStartDate = a.ActiveVersion.RemainderStartDate.Format(time.RFC3339)
		}

		if a.ActiveVersion.EndDate != nil {
			pa.ActiveVersion.EndDate = a.ActiveVersion.EndDate.Format(time.RFC3339)
		}

		for _, tracker := range a.ActiveVersion.ETFConfig {
			pa.ActiveVersion.EtfConfig = append(pa.ActiveVersion.EtfConfig, &apiproto.ETFTracker{
				Tracker:    tracker.Tracker,
				Percentage: tracker.Percentage,
				Ter:        tracker.TER,
			})
		}

		for _, penalty := range a.ActiveVersion.Penalties {
			pa.ActiveVersion.Penalties = append(pa.ActiveVersion.Penalties, &apiproto.AssetPenalty{
				Name:        penalty.Name,
				TriggerType: string(penalty.TriggerType),
				Percentage:  penalty.Percentage,
			})
		}

		for _, subAsset := range a.ActiveVersion.SubAssets {
			saTargetValue, _ := strconv.ParseFloat(subAsset.TargetValue, 64)
			psa := &apiproto.SubAsset{
				Id:                  subAsset.ID,
				Name:                subAsset.Name,
				TargetValue:         saTargetValue,
				AmountPerMonth:      subAsset.AmountPerMonth,
				IsRemainderConsumer: subAsset.IsRemainderConsumer,
				DumpingLoanId:       "",
				StartDate:           subAsset.StartDate.Format(time.RFC3339),
				EndDate:             "",
				EarliestDumpDate:    "",
			}
			if subAsset.RemainderStartDate != nil {
				psa.RemainderStartDate = subAsset.RemainderStartDate.Format(time.RFC3339)
			}
			if subAsset.DumpingLoanID != nil {
				psa.DumpingLoanId = *subAsset.DumpingLoanID
			}
			if subAsset.EndDate != nil {
				psa.EndDate = subAsset.EndDate.Format(time.RFC3339)
			}
			if subAsset.EarliestDumpDate != nil {
				psa.EarliestDumpDate = subAsset.EarliestDumpDate.Format(time.RFC3339)
			}
			pa.ActiveVersion.SubAssets = append(pa.ActiveVersion.SubAssets, psa)
		}
	}

	return pa
}
