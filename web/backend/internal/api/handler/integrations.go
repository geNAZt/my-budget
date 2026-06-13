package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/web/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/web/backend/internal/crypto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	"github.com/genazt/my-budget-script/web/backend/internal/service"
	"github.com/google/uuid"
)

// Integrations isolates everything relating to the "integrations::" namespace.
// The reflection registry maps methods on this struct to "integrations::[method_name]".
type Integrations struct {
	handler      *api.WebSocketHandler
	integrations *repository.IntegrationRepository
	syncService  *service.SyncService
	gcService    *service.GoCardlessService
	ebService    *service.EnableBankingService
	crypto       *crypto.CryptoService
	userRepo     *repository.UserRepository

	// Sub-namespaces
	Transactions  *IntegrationsTransactions
	Enablebanking *IntegrationsEnableBanking
	Gocardless    *IntegrationsGoCardless
	Accounts      *IntegrationsAccounts
}

// NewIntegrations instantiates the isolated Integrations handler namespace.
func NewIntegrations(
	handler *api.WebSocketHandler,
	integrations *repository.IntegrationRepository,
	syncService *service.SyncService,
	gcService *service.GoCardlessService,
	ebService *service.EnableBankingService,
	crypto *crypto.CryptoService,
	userRepo *repository.UserRepository,
	txRepo *repository.TransactionRepository,
) *Integrations {
	return &Integrations{
		handler:      handler,
		integrations: integrations,
		syncService:  syncService,
		gcService:    gcService,
		ebService:    ebService,
		crypto:       crypto,
		userRepo:     userRepo,

		Transactions: &IntegrationsTransactions{
			handler:         handler,
			repo:            txRepo,
			integrationRepo: integrations,
			syncService:     syncService,
			crypto:          crypto,
		},
		Enablebanking: &IntegrationsEnableBanking{
			handler:         handler,
			integrationRepo: integrations,
			syncService:     syncService,
			ebService:       ebService,
			crypto:          crypto,
		},
		Gocardless: &IntegrationsGoCardless{
			handler:         handler,
			integrationRepo: integrations,
			syncService:     syncService,
			gcService:       gcService,
			crypto:          crypto,
		},
		Accounts: &IntegrationsAccounts{
			handler:         handler,
			integrationRepo: integrations,
			syncService:     syncService,
			crypto:          crypto,
		},
	}
}

// List automatically registers as "integrations::list"
func (i *Integrations) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		i.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	entities, err := i.integrations.List(userID)
	if err != nil {
		i.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.IntegrationList{}
	for _, e := range entities {
		p := mapIntegrationToProto(e)
		log.Printf("[WS] Mapping integration: %s -> %s (ID: %s)", e.Name, p.IntegrationName, p.IntegrationId)
		resp.Integrations = append(resp.Integrations, p)
	}

	i.handler.SendResponse(s, reqID, resp, true)
}

// Delete automatically registers as "integrations::delete"
func (i *Integrations) Delete(s *api.WebsocketSession, reqID string, reqIDObj *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		i.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := i.integrations.Delete(userID, reqIDObj.Id); err != nil {
		i.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	i.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: reqIDObj.Id}, true)
}

// Save automatically registers as "integrations::save"
func (i *Integrations) Save(s *api.WebsocketSession, reqID string, reqObj *apiproto.IntegrationSaveRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		i.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var integrationID string
	if reqObj.Id != "" {
		integrationID = reqObj.Id
	} else {
		integrationID = uuid.New().String()
	}

	// Derive or create MasterKey
	var masterKey []byte
	var err error

	existing, _ := i.integrations.GetByID(userID, integrationID)
	if existing != nil {
		masterKey, err = i.syncService.GetMasterKey(userID, integrationID)
	} else {
		// Brand new integration: generate a fresh 32-byte MIK
		masterKey = make([]byte, 32)
		_, err = rand.Read(masterKey)
	}

	if err != nil || len(masterKey) == 0 {
		i.handler.SendError(s, reqID, http.StatusInternalServerError, "failed to manage encryption key")
		return
	}

	// 1. Wrap MIK for all user authenticators if new integration
	if existing == nil {
		user, err := i.userRepo.GetUserByID(userID)
		if err == nil && user != nil {
			for _, auth := range user.Authenticators {
				ik, _ := i.crypto.DeriveIdentityKey(auth.PublicKey)
				wrapped, err := i.crypto.WrapKey(ik, masterKey)
				if err == nil {
					i.integrations.SaveKeySlot(integrationID, auth.ID, base64.StdEncoding.EncodeToString(wrapped))
				}
			}
		}
	}

	// 2. Encrypt config based on service type
	configMap := make(map[string]interface{})

	// If updating, preserve existing config fields (like metadata)
	if existing != nil {
		decrypted, err := i.syncService.DecryptIntegrationConfig(userID, existing)
		if err == nil {
			json.Unmarshal(decrypted, &configMap)
		}
	}

	if reqObj.ServiceType == "GOCARDLESS" {
		configMap["secret_id"] = reqObj.SecretId
		configMap["secret_key"] = reqObj.SecretKey
	} else if reqObj.ServiceType == "TRADING212" {
		configMap["api_key"] = reqObj.ApiKey
		configMap["api_secret"] = reqObj.ApiSecret
	} else if reqObj.ServiceType == "ENABLEBANKING" {
		configMap["application_id"] = reqObj.ApplicationId
		configMap["private_key"] = reqObj.PrivateKey
	}

	configBytes, _ := json.Marshal(configMap)
	ciphertext, err := i.crypto.Encrypt(masterKey, configBytes)
	if err != nil {
		i.handler.SendError(s, reqID, http.StatusInternalServerError, "encryption failed")
		return
	}

	status := "LINKING"
	if reqObj.ServiceType == "TRADING212" {
		status = "ACTIVE"
	}

	domainObj := domain.Integration{
		ID:                  integrationID,
		UserID:              userID,
		ServiceType:         reqObj.ServiceType,
		Name:                reqObj.Name,
		EncryptedConfig:     base64.StdEncoding.EncodeToString(ciphertext),
		Status:              status,
		SyncIntervalSeconds: int(reqObj.SyncIntervalSeconds),
	}
	if domainObj.SyncIntervalSeconds <= 0 {
		domainObj.SyncIntervalSeconds = 3600
	}

	if err := i.integrations.Save(userID, &domainObj); err != nil {
		i.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	// Cache the derived key for enablebanking callback or immediate list use
	i.syncService.CacheMasterKey(userID, integrationID, masterKey)

	resp := mapIntegrationToProto(domainObj)
	i.handler.SendResponse(s, reqID, resp, true)
}

// Sync automatically registers as "integrations::sync"
func (i *Integrations) Sync(s *api.WebsocketSession, reqID string, req *apiproto.IntegrationSyncRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		i.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// Ensure PSU headers are populated
	if req.PsuHeaders == nil {
		req.PsuHeaders = make(map[string]string)
	}

	if _, ok := req.PsuHeaders["Psu-Ip-Address"]; !ok || req.PsuHeaders["Psu-Ip-Address"] == "127.0.0.1" {
		req.PsuHeaders["Psu-Ip-Address"] = s.RemoteAddr()
	}

	go func() {
		err := i.syncService.SyncIntegration(userID, req.Id, req.Force, req.PsuHeaders)
		if err != nil {
			i.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		} else {
			i.handler.SendResponse(s, reqID, &apiproto.Empty{}, true)
		}
	}()
}

// =========================================================================
// SUB-NAMESPACE: integrations::transactions
// =========================================================================
type IntegrationsTransactions struct {
	handler         *api.WebSocketHandler
	repo            *repository.TransactionRepository
	integrationRepo *repository.IntegrationRepository
	syncService     *service.SyncService
	crypto          *crypto.CryptoService
}

func (t *IntegrationsTransactions) List(s *api.WebsocketSession, reqID string, body *apiproto.GenericID) {
	userID, _ := s.GetAuth()
	if userID == "" {
		t.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	txs, err := t.repo.List(userID)
	if err != nil {
		t.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	integrations, err := t.integrationRepo.List(userID)
	if err != nil {
		t.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	mikCache := make(map[string][]byte)
	for _, i := range integrations {
		mk, err := t.syncService.GetMasterKey(userID, i.ID)
		if err == nil {
			mikCache[i.ID] = mk
		}
	}

	activeIntegrations, _ := t.syncService.GetAccountActiveIntegrations(userID, mikCache, integrations)

	resp := &apiproto.DiscoveredTransactionList{}

	for _, tx := range txs {
		if tx.IsDeleted {
			continue
		}

		data, err := t.syncService.DecryptTransaction(userID, &tx, mikCache, activeIntegrations)
		if err != nil {
			continue
		}

		var integrationObj *domain.Integration
		for _, i := range integrations {
			if i.ID == tx.IntegrationID {
				integrationObj = &i
				break
			}
		}
		if integrationObj == nil {
			continue
		}

		provider := t.syncService.GetProvider(integrationObj.ServiceType)
		if provider == nil {
			continue
		}

		meta, err := provider.ParseTransaction(data, tx.AccountID)
		if err != nil {
			continue
		}

		potentialLinkId := ""
		if tx.LinkedTransactionID != nil {
			potentialLinkId = *tx.LinkedTransactionID
		}

		p := &apiproto.Transaction{
			Id:                   tx.ID,
			IntegrationId:        tx.IntegrationID,
			AccountId:            tx.AccountID,
			PoolIds:              tx.PoolIDs,
			Amount:               meta.Amount,
			Receiver:             meta.Receiver,
			ReceiverIban:         meta.ReceiverIBAN,
			Description:          meta.Description,
			CreatedAt:            tx.CreatedAt.Format(time.RFC3339),
			Tags:                 tx.Tags,
			SourceAccountId:      tx.SourceAccountID,
			DestinationAccountId: tx.DestinationAccountID,
			IsLinkConfirmed:      tx.IsLinkConfirmed,
			PotentialLinkId:      potentialLinkId,
			DeniedDuplicateIds:   tx.DeniedDuplicateIDs,
			ExternalId:           tx.ExternalID,
			CorrelationId:        tx.CorrelationID,
			InternalStatus:       tx.InternalStatus,
		}

		// Create DuplicateKey for UI highlighting: date | abs_amount
		p.DuplicateKey = fmt.Sprintf("%s|%.2f", tx.CreatedAt.Format("2006-01-02"), math.Abs(meta.Amount))

		resp.Transactions = append(resp.Transactions, p)
	}

	t.handler.SendResponse(s, reqID, resp, true)
}

func (t *IntegrationsTransactions) Delete(s *api.WebsocketSession, reqID string, req *apiproto.TransactionDeleteRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		t.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	if err := t.repo.Delete(userID, req.Id); err != nil {
		t.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	t.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: req.Id}, true)
}

func (t *IntegrationsTransactions) Unlink(s *api.WebsocketSession, reqID string, req *apiproto.TransactionUnlinkRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		t.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	log.Printf("[LINK] User %s unlinking %s", userID, req.Id)
	if err := t.repo.UnlinkTransaction(userID, req.Id); err != nil {
		log.Printf("[LINK] Error unlinking: %v", err)
		t.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	t.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: req.Id}, true)
}

func (t *IntegrationsTransactions) Link(s *api.WebsocketSession, reqID string, req *apiproto.TransactionLinkRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		t.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	log.Printf("[LINK] User %s linking %s and %s", userID, req.Id, req.TargetId)
	if err := t.repo.LinkTransactions(userID, req.Id, req.TargetId); err != nil {
		log.Printf("[LINK] Error linking: %v", err)
		t.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	t.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: req.Id}, true)
}

func (t *IntegrationsTransactions) AllowDuplicate(s *api.WebsocketSession, reqID string, req *apiproto.TransactionDuplicateRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		t.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	tx, err := t.repo.GetByID(userID, req.Id)
	if err != nil || tx == nil {
		t.handler.SendError(s, reqID, http.StatusNotFound, "Transaction not found")
		return
	}

	denied := tx.DeniedDuplicateIDs
	if denied != "" {
		denied += "," + req.DeniedId
	} else {
		denied = req.DeniedId
	}

	if err := t.repo.UpdateDeniedDuplicateIDs(userID, req.Id, denied); err != nil {
		t.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	t.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: req.Id}, true)
}

func (t *IntegrationsTransactions) Update(s *api.WebsocketSession, reqID string, req *apiproto.Transaction) {
	userID, _ := s.GetAuth()
	if userID == "" {
		t.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	tx, err := t.repo.GetByID(userID, req.Id)
	if err != nil || tx == nil {
		t.handler.SendError(s, reqID, http.StatusNotFound, "Transaction not found")
		return
	}

	// 1. Update core fields in DB (accounts, tags)
	err = t.repo.Update(userID, req.Id, req.AccountId, req.SourceAccountId, req.DestinationAccountId, req.Tags)
	if err != nil {
		t.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	// 2. Update encrypted metadata (receiver, receiver_iban, description, amount)
	integration, _ := t.integrationRepo.GetByID(userID, tx.IntegrationID)
	if integration != nil {
		masterKey, err := t.syncService.GetMasterKey(userID, tx.IntegrationID)
		if err == nil {
			mikCache := map[string][]byte{tx.IntegrationID: masterKey}
			decrypted, err := t.syncService.DecryptTransaction(userID, tx, mikCache, nil)
			if err == nil {
				provider := t.syncService.GetProvider(integration.ServiceType)
				if provider != nil {
					meta, _ := provider.ParseTransaction(decrypted, tx.AccountID)

					// Update metadata fields from request
					meta.Amount = req.Amount
					meta.Receiver = req.Receiver
					meta.ReceiverIBAN = req.ReceiverIban
					meta.Description = req.Description

					genericTx := domain.GenericTransaction{
						Amount:      meta.Amount,
						Description: meta.Description,
						Peer:        meta.Receiver,
						PeerIBAN:    meta.ReceiverIBAN,
						CreatedAt:   meta.CreatedAt,
						ExternalID:  meta.ExternalID,
					}

					newJSON, _ := json.Marshal(genericTx)
					encrypted, err := t.crypto.Encrypt(masterKey, newJSON)
					if err == nil {
						encryptedB64 := base64.StdEncoding.EncodeToString(encrypted)
						t.repo.UpdateEncryptedData(userID, tx.ID, encryptedB64)

						// 3. Trigger rule re-evaluation
						accountTags := ""
						accountName := ""
						decryptedConfig, err := t.syncService.DecryptIntegrationConfig(userID, integration)
						if err == nil {
							var config struct {
								AccountsMetadata map[string]*domain.AccountMeta `json:"accounts_metadata"`
							}
							json.Unmarshal(decryptedConfig, &config)
							if config.AccountsMetadata != nil && config.AccountsMetadata[tx.AccountID] != nil {
								accountTags = config.AccountsMetadata[tx.AccountID].Tags
								accountName = config.AccountsMetadata[tx.AccountID].Alias
							}
						}

						newPoolIDs, _ := t.syncService.RuleService().ProcessTransaction(userID, tx.IntegrationID, meta.Receiver, meta.Description, req.Tags, accountTags, accountName, meta.Amount)
						t.repo.UpdatePools(userID, tx.ID, newPoolIDs)
					}
				}
			}
		}
	}

	t.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: req.Id}, true)
}

// =========================================================================
// SUB-NAMESPACE: integrations::accounts
// =========================================================================
type IntegrationsAccounts struct {
	handler         *api.WebSocketHandler
	integrationRepo *repository.IntegrationRepository
	syncService     *service.SyncService
	crypto          *crypto.CryptoService
}

func (a *IntegrationsAccounts) List(s *api.WebsocketSession, reqID string, req *apiproto.IntegrationAccountsRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	var integrations []domain.Integration
	var err error

	if req.Id != "" {
		integration, err := a.integrationRepo.GetByID(userID, req.Id)
		if err != nil || integration == nil {
			a.handler.SendError(s, reqID, http.StatusNotFound, "Integration not found")
			return
		}
		integrations = append(integrations, *integration)
	} else {
		integrations, err = a.integrationRepo.List(userID)
		if err != nil {
			a.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
			return
		}
	}

	resp := &apiproto.IntegrationAccountList{}
	for _, intEntity := range integrations {
		integration := intEntity // Local copy for safety

		provider := a.syncService.GetRegistry().Get(string(integration.ServiceType))
		if provider == nil {
			log.Printf("[WS] No provider found for service type %s", integration.ServiceType)
			continue
		}

		accounts, err := provider.GetAccounts(userID, &integration)
		if err != nil {
			log.Printf("[WS] Failed to get accounts for integration %s: %v", integration.ID, err)
			continue
		}

		for _, acc := range accounts {
			backoff := ""
			if acc.BackoffUntil != nil {
				backoff = acc.BackoffUntil.Format(time.RFC3339)
			}

			log.Printf("[WS] Account %s: Name=%s, Integration=%s (ID in loop: %s), Balance=%v, Enabled=%v", acc.ID, acc.Name, integration.Name, integration.ID, acc.Balance, acc.Enabled)

			resp.Accounts = append(resp.Accounts, &apiproto.IntegrationAccount{
				Id:            acc.ID,
				Name:          acc.Name,
				Balance:       acc.Balance,
				IntegrationId: integration.ID,
				Enabled:       acc.Enabled,
				Iban:          acc.IBAN,
				BackoffUntil:  backoff,
				Tags:          acc.Tags,
			})
		}
	}

	log.Printf("[WS] IntegrationsAccounts.List returning %d accounts", len(resp.Accounts))
	a.handler.SendResponse(s, reqID, resp, true)
}

func (a *IntegrationsAccounts) Update(s *api.WebsocketSession, reqID string, req *apiproto.IntegrationAccountUpdate) {
	userID, _ := s.GetAuth()
	if userID == "" {
		a.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	integration, err := a.integrationRepo.GetByID(userID, req.IntegrationId)
	if err != nil || integration == nil {
		a.handler.SendError(s, reqID, http.StatusNotFound, "Integration not found")
		return
	}

	decrypted, err := a.syncService.DecryptIntegrationConfig(userID, integration)
	if err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, "Failed to decrypt config")
		return
	}

	var config map[string]interface{}
	if err := json.Unmarshal(decrypted, &config); err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, "Failed to parse config")
		return
	}

	metadata, ok := config["accounts_metadata"].(map[string]interface{})
	if !ok {
		metadata = make(map[string]interface{})
		config["accounts_metadata"] = metadata
	}

	accMeta, ok := metadata[req.AccountId].(map[string]interface{})
	if !ok {
		accMeta = make(map[string]interface{})
		metadata[req.AccountId] = accMeta
	}

	accMeta["Alias"] = req.Alias
	accMeta["Enabled"] = req.Enabled
	accMeta["IBAN"] = req.Iban
	accMeta["Tags"] = req.Tags

	updatedConfig, err := json.Marshal(config)
	if err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, "Failed to marshal config")
		return
	}

	masterKey, err := a.syncService.GetMasterKey(userID, integration.ID)
	if err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, "Failed to get master key")
		return
	}

	encrypted, err := a.crypto.Encrypt(masterKey, updatedConfig)
	if err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, "Failed to encrypt config")
		return
	}

	integration.EncryptedConfig = base64.StdEncoding.EncodeToString(encrypted)
	if err := a.integrationRepo.Save(userID, integration); err != nil {
		a.handler.SendError(s, reqID, http.StatusInternalServerError, "Failed to update integration")
		return
	}

	a.handler.SendResponse(s, reqID, &apiproto.GenericID{Id: req.AccountId}, true)
}

// =========================================================================
// SUB-NAMESPACE: integrations::enablebanking
// =========================================================================
type IntegrationsEnableBanking struct {
	handler         *api.WebSocketHandler
	integrationRepo *repository.IntegrationRepository
	syncService     *service.SyncService
	ebService       *service.EnableBankingService
	crypto          *crypto.CryptoService
}

func (e *IntegrationsEnableBanking) Aspsps(s *api.WebsocketSession, reqID string, req *apiproto.EBAspspsRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		e.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	integration, err := e.integrationRepo.GetByID(userID, req.Id)
	if err != nil || integration == nil {
		e.handler.SendError(s, reqID, http.StatusNotFound, "Integration not found")
		return
	}

	decrypted, err := e.syncService.DecryptIntegrationConfig(userID, integration)
	if err != nil {
		e.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	var config struct {
		ApplicationID string `json:"application_id"`
		PrivateKey    string `json:"private_key"`
	}
	if err := json.Unmarshal(decrypted, &config); err != nil {
		e.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	token, err := e.ebService.CreateJWT(config.ApplicationID, config.PrivateKey)
	if err != nil {
		e.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	aspsps, err := e.ebService.GetASPSPs(context.Background(), token, req.Country)
	if err != nil {
		e.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.EBAspspsList{}
	for _, asp := range aspsps {
		name := ""
		if asp.Name != nil {
			name = *asp.Name
		}
		country := ""
		if asp.Country != nil {
			country = *asp.Country
		}
		resp.Aspsps = append(resp.Aspsps, &apiproto.EBAspsp{
			Name:    name,
			Country: country,
		})
	}

	e.handler.SendResponse(s, reqID, resp, true)
}

func (e *IntegrationsEnableBanking) Link(s *api.WebsocketSession, reqID string, req *apiproto.EBLinkRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		e.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	integration, err := e.integrationRepo.GetByID(userID, req.Id)
	if err != nil || integration == nil {
		e.handler.SendError(s, reqID, http.StatusNotFound, "Integration not found")
		return
	}

	decrypted, err := e.syncService.DecryptIntegrationConfig(userID, integration)
	if err != nil {
		e.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	var config struct {
		ApplicationID string `json:"application_id"`
		PrivateKey    string `json:"private_key"`
	}
	if err := json.Unmarshal(decrypted, &config); err != nil {
		e.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	token, err := e.ebService.CreateJWT(config.ApplicationID, config.PrivateKey)
	if err != nil {
		e.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	url, _, err := e.ebService.StartAuthorization(context.Background(), token, req.BankName, req.Country, req.RedirectUrl, req.State, "")
	if err != nil {
		e.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	masterKey, _ := e.syncService.GetMasterKey(userID, integration.ID)
	e.syncService.CacheMasterKey(userID, integration.ID, masterKey)

	resp := &apiproto.EBLinkResponse{
		Url: url,
	}
	e.handler.SendResponse(s, reqID, resp, true)
}

// =========================================================================
// SUB-NAMESPACE: integrations::gocardless
// =========================================================================
type IntegrationsGoCardless struct {
	handler         *api.WebSocketHandler
	integrationRepo *repository.IntegrationRepository
	syncService     *service.SyncService
	gcService       *service.GoCardlessService
	crypto          *crypto.CryptoService
}

func (g *IntegrationsGoCardless) Institutions(s *api.WebsocketSession, reqID string, req *apiproto.GCInstitutionsRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		g.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	integration, err := g.integrationRepo.GetByID(userID, req.Id)
	if err != nil || integration == nil {
		g.handler.SendError(s, reqID, http.StatusNotFound, "Integration not found")
		return
	}

	decrypted, err := g.syncService.DecryptIntegrationConfig(userID, integration)
	if err != nil {
		g.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	var config struct {
		SecretID  string `json:"secret_id"`
		SecretKey string `json:"secret_key"`
	}
	if err := json.Unmarshal(decrypted, &config); err != nil {
		g.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	ctx := context.Background()
	token, _, err := g.gcService.GetAccessToken(ctx, config.SecretID, config.SecretKey)
	if err != nil {
		g.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	institutions, err := g.gcService.GetInstitutions(ctx, req.Country, token)
	if err != nil {
		g.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	resp := &apiproto.GCInstitutionsList{}
	for _, inst := range institutions {
		resp.Institutions = append(resp.Institutions, &apiproto.GCInstitution{
			Id:   inst.Id,
			Name: inst.Name,
		})
	}

	g.handler.SendResponse(s, reqID, resp, true)
}

func (g *IntegrationsGoCardless) Link(s *api.WebsocketSession, reqID string, req *apiproto.GCLinkRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		g.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	integration, err := g.integrationRepo.GetByID(userID, req.Id)
	if err != nil || integration == nil {
		g.handler.SendError(s, reqID, http.StatusNotFound, "Integration not found")
		return
	}

	decrypted, err := g.syncService.DecryptIntegrationConfig(userID, integration)
	if err != nil {
		g.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	var config struct {
		SecretID  string `json:"secret_id"`
		SecretKey string `json:"secret_key"`
	}
	if err := json.Unmarshal(decrypted, &config); err != nil {
		g.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	ctx := context.Background()
	token, _, err := g.gcService.GetAccessToken(ctx, config.SecretID, config.SecretKey)
	if err != nil {
		g.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	reqResult, err := g.gcService.CreateRequisition(ctx, req.InstitutionId, req.RedirectUrl, token)
	if err != nil {
		g.handler.SendError(s, reqID, http.StatusInternalServerError, err.Error())
		return
	}

	var fullConfig map[string]interface{}
	json.Unmarshal(decrypted, &fullConfig)
	fullConfig["requisition_id"] = reqResult.Id

	newDecrypted, _ := json.Marshal(fullConfig)
	masterKey, _ := g.syncService.GetMasterKey(userID, integration.ID)
	ciphertext, _ := g.crypto.Encrypt(masterKey, newDecrypted)

	integration.EncryptedConfig = base64.StdEncoding.EncodeToString(ciphertext)
	g.integrationRepo.Save(userID, integration)

	resp := &apiproto.GCLinkResponse{
		Link: *reqResult.Link,
	}
	g.handler.SendResponse(s, reqID, resp, true)
}

// Helper mapper function kept identical
func mapIntegrationToProto(i domain.Integration) *apiproto.Integration {
	lastSync := ""
	if i.LastSyncAt != nil {
		lastSync = i.LastSyncAt.Format(time.RFC3339)
	}

	return &apiproto.Integration{
		IntegrationId:       i.ID,
		IntegrationName:     i.Name,
		ServiceType:         string(i.ServiceType),
		DiscoveredCount:     0,
		Status:              i.Status,
		SyncIntervalSeconds: int32(i.SyncIntervalSeconds),
		LastSyncAt:          lastSync,
		LastError:           i.LastError,
		CachedBalance:       i.CachedBalance,
	}
}
