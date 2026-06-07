package service

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/bus"
	"github.com/genazt/my-budget-script/web/backend/internal/crypto"
	"github.com/genazt/my-budget-script/web/backend/internal/db"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/integration"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type SyncService struct {
	integrationRepo     *repository.IntegrationRepository
	transactionRepo     *repository.TransactionRepository
	connRepo            *repository.ConnectionRepository
	assetRepo           *repository.AssetRepository
	userRepo            *repository.UserRepository
	cryptoService       *crypto.CryptoService
	integrationRegistry *integration.Registry
	ruleService         *RuleService
	eventBus            *bus.Bus

	// Temporary cache for recovered master keys (Recovery flow)
	recoveryCache map[string]map[string][]byte // userID -> integrationID -> MIK
	cacheMu       sync.RWMutex
}

func (s *SyncService) RuleService() *RuleService {
	return s.ruleService
}

func (s *SyncService) GetProvider(serviceType string) integration.Provider {

	return s.integrationRegistry.Get(serviceType)
}

func NewSyncService(
	ir *repository.IntegrationRepository,
	tr *repository.TransactionRepository,
	cr *repository.ConnectionRepository,
	ar *repository.AssetRepository,
	ur *repository.UserRepository,
	cs *crypto.CryptoService,
	ir_registry *integration.Registry,
	rs *RuleService,
	eventBus *bus.Bus,
) *SyncService {

	return &SyncService{
		integrationRepo:     ir,
		transactionRepo:     tr,
		connRepo:            cr,
		assetRepo:           ar,
		userRepo:            ur,
		cryptoService:       cs,
		integrationRegistry: ir_registry,
		ruleService:         rs,
		recoveryCache:       make(map[string]map[string][]byte),
		eventBus:            eventBus,
	}
}

func (s *SyncService) GetRegistry() *integration.Registry {
	return s.integrationRegistry
}

func (s *SyncService) StartBackgroundWorker() {

	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[SYNC] Panic in background worker: %v", r)
			}
		}()

		for range ticker.C {
			log.Printf("[SYNC] Running scheduled background tasks...")
			s.SyncAll()
			log.Printf("[SYNC] Background tasks completed.")
		}
	}()
}

func (s *SyncService) SyncAll() {
	all, err := s.integrationRepo.ListAll()
	if err == nil {
		log.Printf("[SYNC] Total integrations in DB: %d", len(all))
		for _, i := range all {
			log.Printf("[SYNC] - %s (ID: %s, User: %s, Status: %s, Service: %s, LastError: %s)", i.Name, i.ID, i.UserID, i.Status, i.ServiceType, i.LastError)
		}
	} else {
		log.Printf("[SYNC] Failed to list all integrations for debug: %v", err)
	}

	integrations, err := s.integrationRepo.ListAllActive()
	if err != nil {
		log.Printf("[SYNC] Failed to list integrations: %v", err)
		return
	}

	log.Printf("[SYNC] Syncing %d active integrations...", len(integrations))

	for _, i := range integrations {
		log.Printf("[SYNC] Starting scheduled sync for %s (User: %s)...", i.Name, i.UserID)
		s.SyncIntegration(i.UserID, i.ID, false)
	}
}

func (s *SyncService) SyncIntegration(userID string, integrationID string, force bool) error {

	integration, err := s.integrationRepo.GetByID(userID, integrationID)
	if err != nil || integration == nil {
		return fmt.Errorf("integration not found")
	}

	correlationID := uuid.New().String()
	log.Printf("[SYNC][%s] Dispatching sync for %s (%s)...", correlationID, integration.Name, integration.ServiceType)

	provider := s.integrationRegistry.Get(integration.ServiceType)
	if provider == nil {
		return s.handleSyncError(userID, integration, fmt.Errorf("unsupported service type: %s", integration.ServiceType))
	}

	ctx := ContextWithCorrelationID(context.Background(), correlationID)
	res := provider.Sync(ctx, integration, force)

	if res.Error == nil {
		s.eventBus.Publish(context.Background(), bus.TopicSyncFinished, bus.SyncFinishedPayload{
			UserID:          userID,
			IntegrationID:   integrationID,
			IntegrationName: integration.Name,
			ServiceType:     integration.ServiceType,
			DiscoveredCount: res.DiscoveredCount,
		})
	}

	if res.Error != nil {
		return s.handleSyncError(userID, integration, res.Error)
	}

	return s.finalizeSync(userID, integration, integration.CachedBalance)
}

func (s *SyncService) GetMasterKey(userID string, integrationID string) ([]byte, error) {

	s.cacheMu.RLock()
	userCache := s.recoveryCache[userID]
	if userCache != nil {
		if cachedMik, ok := userCache[integrationID]; ok {
			s.cacheMu.RUnlock()
			return cachedMik, nil
		}
	}
	s.cacheMu.RUnlock()

	user, _ := s.userRepo.GetUserByID(userID)
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	return s.GetMasterKeyForUser(user, integrationID)
}

func (s *SyncService) CacheMasterKey(userID string, integrationID string, key []byte) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if _, ok := s.recoveryCache[userID]; !ok {
		s.recoveryCache[userID] = make(map[string][]byte)
	}
	s.recoveryCache[userID][integrationID] = key
}

func (s *SyncService) GetMasterKeyForUser(user *domain.User, integrationID string) ([]byte, error) {

	// 1. Try Slot-based (Multi-key support)
	for _, auth := range user.Authenticators {
		wrappedB64, err := s.integrationRepo.GetKeySlot(integrationID, auth.ID)
		if err == nil && wrappedB64 != "" {
			wrapped, _ := base64.StdEncoding.DecodeString(wrappedB64)
			ik, _ := s.cryptoService.DeriveIdentityKey(auth.PublicKey)
			mik, err := s.cryptoService.UnwrapKey(ik, wrapped)
			if err == nil {
				return mik, nil
			}
		}
	}

	return nil, fmt.Errorf("failed to retrieve master key")
}

func (s *SyncService) handleSyncError(userID string, i *domain.Integration, err error) error {
	log.Printf("[SYNC] Error on %s: %v", i.Name, err)
	i.LastError = err.Error()
	i.Status = "ERROR"

	s.integrationRepo.Save(userID, i)
	return err
}

func (s *SyncService) finalizeSync(userID string, i *domain.Integration, balance float64) error {
	now := time.Now().UTC()
	i.LastSyncAt = &now
	i.Status = "ACTIVE"
	i.LastError = ""
	i.CachedBalance = balance
	return s.integrationRepo.Save(userID, i)
}

func (s *SyncService) EnsureRecoveryTokens() {

	log.Printf("[RECOVERY] Checking for missing recovery tokens...")
	users, err := s.userRepo.ListAll()
	if err != nil {
		log.Printf("[RECOVERY] Failed to list users: %v", err)
		return
	}

	for _, u := range users {
		if u.RecoveryHash != "" {
			continue
		}

		// Generate new token: MB-XXXX-XXXX-XXXX
		chars := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
		token := "MB-"
		for i := 0; i < 3; i++ {
			for j := 0; j < 4; j++ {
				idx, _ := crand.Int(crand.Reader, big.NewInt(int64(len(chars))))
				token += string(chars[idx.Int64()])
			}
			if i < 2 {
				token += "-"
			}
		}

		log.Printf("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")
		log.Printf("!!! NEW RECOVERY TOKEN GENERATED FOR %s: %s !!!", u.Username, token)
		log.Printf("!!! STORE THIS SAFELY. IT WILL NOT BE SHOWN AGAIN.     !!!")
		log.Printf("!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!")

		hash, _ := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
		s.userRepo.UpdateRecoveryHash(u.ID, string(hash))
		s.SetupRecoveryKey(u.ID, token)
	}
}

func (s *SyncService) PropagateKeyToNewAuthenticator(userID string, authPubKey []byte, authID []byte) error {

	integrations, err := s.integrationRepo.List(userID)
	if err != nil {
		return err
	}

	newIk, _ := s.cryptoService.DeriveIdentityKey(authPubKey)

	s.cacheMu.RLock()
	userCache := s.recoveryCache[userID]
	s.cacheMu.RUnlock()

	for _, i := range integrations {
		var mik []byte
		var err error

		if userCache != nil {
			if cachedMik, ok := userCache[i.ID]; ok {
				mik = cachedMik
			}
		}

		if mik == nil {
			mik, err = s.GetMasterKey(userID, i.ID)
		}

		if err != nil || mik == nil {
			continue
		}

		wrapped, _ := s.cryptoService.WrapKey(newIk, mik)
		s.integrationRepo.SaveKeySlot(i.ID, authID, base64.StdEncoding.EncodeToString(wrapped))
		log.Printf("[CRYPTO] Propagated MIK to new authenticator for integration %s", i.Name)
	}

	s.cacheMu.Lock()
	delete(s.recoveryCache, userID)
	s.cacheMu.Unlock()

	return nil
}

func (s *SyncService) SetupRecoveryKey(userID string, phrase string) error {

	integrations, err := s.integrationRepo.List(userID)
	if err != nil {
		return err
	}

	h := sha256.New()
	h.Write([]byte(phrase))
	rik, _ := s.cryptoService.DeriveIdentityKey(h.Sum(nil))

	for _, i := range integrations {
		mik, err := s.GetMasterKey(userID, i.ID)
		if err != nil {
			continue
		}

		wrapped, _ := s.cryptoService.WrapKey(rik, mik)
		s.integrationRepo.SaveKeySlot(i.ID, []byte("RECOVERY"), base64.StdEncoding.EncodeToString(wrapped))
	}
	return nil
}

func (s *SyncService) RecoverMIKsToCache(userID string, phrase string) int {

	integrations, err := s.integrationRepo.List(userID)
	if err != nil {
		return 0
	}

	h := sha256.New()
	h.Write([]byte(phrase))
	rik, _ := s.cryptoService.DeriveIdentityKey(h.Sum(nil))

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	if _, ok := s.recoveryCache[userID]; !ok {
		s.recoveryCache[userID] = make(map[string][]byte)
	}

	recoveredCount := 0
	for _, i := range integrations {
		wrappedB64, err := s.integrationRepo.GetKeySlot(i.ID, []byte("RECOVERY"))
		if err != nil || wrappedB64 == "" {
			continue
		}

		wrapped, _ := base64.StdEncoding.DecodeString(wrappedB64)
		mik, err := s.cryptoService.UnwrapKey(rik, wrapped)
		if err != nil {
			continue
		}

		s.recoveryCache[userID][i.ID] = mik
		recoveredCount++
	}

	return recoveredCount
}

func (s *SyncService) GetAccountActiveIntegrations(userID string, mikCache map[string][]byte, integrations []domain.Integration) (map[string]string, error) {

	if integrations == nil {
		var err error
		integrations, err = s.integrationRepo.List(userID)
		if err != nil {
			return nil, err
		}
	}

	accountActiveIntegration := make(map[string]string)
	for _, i := range integrations {
		provider := s.integrationRegistry.Get(string(i.ServiceType))
		if provider == nil {
			continue
		}

		// Ensure mik is in cache for future use (e.g. by DecryptTransaction)
		if mikCache != nil {
			if _, ok := mikCache[i.ID]; !ok {
				if mik, err := s.GetMasterKey(userID, i.ID); err == nil {
					mikCache[i.ID] = mik
				}
			}
		}

		accounts, err := provider.GetAccounts(userID, &i)
		if err != nil {
			continue
		}

		for _, acc := range accounts {
			if acc.Enabled {
				accountActiveIntegration[acc.ID] = i.ID
			}
		}
	}
	return accountActiveIntegration, nil
}

func (s *SyncService) MigrateTransactionsBetweenChains() {

	log.Printf("[SYNC] Starting cross-chain transaction migration check...")
	users, err := s.userRepo.ListAll()
	if err != nil {
		log.Printf("[SYNC] Migration failed to list users: %v", err)
		return
	}

	for _, user := range users {
		mikCache := make(map[string][]byte)
		integrations, err := s.integrationRepo.List(user.ID)
		if err != nil {
			continue
		}

		accountActiveIntegration, err := s.GetAccountActiveIntegrations(user.ID, mikCache, integrations)
		if err != nil {
			continue
		}

		txs, err := s.transactionRepo.List(user.ID)
		if err != nil {
			continue
		}

		for _, tx := range txs {
			activeIntegrationID, ok := accountActiveIntegration[tx.AccountID]
			if ok && tx.IntegrationID != activeIntegrationID {
				log.Printf("[SYNC] Migrating tx %s (Account: %s) from Integration %s to Integration %s", tx.ID, tx.AccountID, tx.IntegrationID, activeIntegrationID)

				_, err := s.DecryptTransaction(user.ID, &tx, mikCache, accountActiveIntegration)
				if err != nil {
					log.Printf("[SYNC] Failed to decrypt and migrate tx %s: %v", tx.ID, err)
					continue
				}
				log.Printf("[SYNC] Successfully migrated tx %s", tx.ID)
			}
		}
	}
}

func (s *SyncService) decrypt(key []byte, ciphertextB64 string) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, err
	}
	return s.cryptoService.Decrypt(key, ciphertext)
}

func (s *SyncService) DecryptIntegrationConfig(userID string, integration *domain.Integration) ([]byte, error) {
	masterKey, err := s.GetMasterKey(userID, integration.ID)
	if err != nil {
		return nil, err
	}

	ciphertext, err := base64.StdEncoding.DecodeString(integration.EncryptedConfig)
	if err != nil {
		return nil, err
	}

	return s.cryptoService.Decrypt(masterKey, ciphertext)
}

func (s *SyncService) DecryptTransaction(userID string, tx *domain.BankTransaction, mikCache map[string][]byte, activeIntegrations map[string]string) ([]byte, error) {

	// Determine if there is a cross-chain mismatch (encryption chain != active account chain)
	var err error
	activeIntegrationID := ""
	if tx.AccountID != "" {
		if activeIntegrations == nil {
			activeIntegrations, err = s.GetAccountActiveIntegrations(userID, mikCache, nil)
			if err == nil {
				activeIntegrationID = activeIntegrations[tx.AccountID]
			}
		} else {
			activeIntegrationID = activeIntegrations[tx.AccountID]
		}
	}

	// 1. Try primary key from current integration_id
	var decrypted []byte
	var decryptedWithKeyID string
	if mk, ok := mikCache[tx.IntegrationID]; ok {
		decrypted, err = s.decrypt(mk, tx.EncryptedData)
		if err == nil {
			decryptedWithKeyID = tx.IntegrationID
		}
	}

	// 2. Recovery: Try all other available integration keys if primary failed
	if decrypted == nil {
		log.Printf("[SYNC] [RECOVERY] Primary decryption failed for tx %s (integration %s). Attempting recovery with %d alternate keys...", tx.ID, tx.IntegrationID, len(mikCache))
		for intID, alternateMIK := range mikCache {
			if intID == tx.IntegrationID {
				continue
			}

			recoveredData, rerr := s.decrypt(alternateMIK, tx.EncryptedData)
			if rerr == nil {
				decrypted = recoveredData
				decryptedWithKeyID = intID
				log.Printf("[SYNC] [RECOVERY] Recovery SUCCESS for tx %s: decrypted using key from integration %s.", tx.ID, intID)
				break
			}
		}
	}

	// 3. Handle live migration, self-healing, and DB sync
	if decrypted != nil {
		if activeIntegrationID != "" && tx.IntegrationID != activeIntegrationID {
			// Cross-chain mismatch! Perform live on-the-fly migration to activeIntegrationID
			log.Printf("[SYNC] [MIGRATION] Live cross-chain migration for tx %s: migrating from Integration %s to active Integration %s...", tx.ID, tx.IntegrationID, activeIntegrationID)
			targetMK, ok := mikCache[activeIntegrationID]
			if !ok {
				// Fallback to deriving target key
				targetMK, err = s.GetMasterKey(userID, activeIntegrationID)
				if err == nil {
					mikCache[activeIntegrationID] = targetMK
				}
			}

			if ok || err == nil {
				newEncrypted, eerr := s.cryptoService.Encrypt(targetMK, decrypted)
				if eerr == nil {
					newEncryptedBase64 := base64.StdEncoding.EncodeToString(newEncrypted)
					if uerr := s.transactionRepo.UpdateIntegrationAndEncryptedData(userID, tx.ID, activeIntegrationID, newEncryptedBase64); uerr != nil {
						log.Printf("[SYNC] [MIGRATION] Warning: Failed to save migrated transaction data for tx %s: %v", tx.ID, uerr)
					} else {
						log.Printf("[SYNC] [MIGRATION] Live migration complete for tx %s.", tx.ID)
						tx.IntegrationID = activeIntegrationID
						tx.EncryptedData = newEncryptedBase64
					}
				} else {
					log.Printf("[SYNC] [MIGRATION] Error: Failed to re-encrypt tx %s under target key: %v", tx.ID, eerr)
				}
			}
		} else if decryptedWithKeyID != "" && decryptedWithKeyID != tx.IntegrationID {
			// Normal key mismatch recovery path (self-healing for current integration)
			log.Printf("[SYNC] [RECOVERY] Mismatched key detected for tx %s (decrypted with %s but tx belongs to %s). Self-healing...", tx.ID, decryptedWithKeyID, tx.IntegrationID)
			if primaryMK, ok := mikCache[tx.IntegrationID]; ok {
				newEncrypted, eerr := s.cryptoService.Encrypt(primaryMK, decrypted)
				if eerr == nil {
					newEncryptedBase64 := base64.StdEncoding.EncodeToString(newEncrypted)
					if uerr := s.transactionRepo.UpdateEncryptedData(userID, tx.ID, newEncryptedBase64); uerr != nil {
						log.Printf("[SYNC] [RECOVERY] Warning: Failed to save healed encrypted data for tx %s: %v", tx.ID, uerr)
					} else {
						log.Printf("[SYNC] [RECOVERY] Self-healing complete for tx %s.", tx.ID)
						tx.EncryptedData = newEncryptedBase64
					}
				}
			}
		}
		return decrypted, nil
	}

	// 4. Recovery failed completely
	log.Printf("[SYNC] [RECOVERY] Transaction %s could not be recovered. Deleting from database...", tx.ID)
	s.transactionRepo.Delete(userID, tx.ID)

	return nil, fmt.Errorf("decryption failed for tx %s using all available keys", tx.ID)
}

func (s *SyncService) ReapplyAllRules(userID string) {

	log.Printf("[SYNC] Manually triggering rule re-application for user %s...", userID)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[SYNC] Panic in ReapplyAllRules: %v", r)
		}
	}()

	if err := s.ApplyRulesToAllTransactions(userID); err != nil {
		log.Printf("[SYNC] Failed to re-apply rules for user %s: %v", userID, err)
	} else {
		log.Printf("[SYNC] Successfully re-applied all rules for user %s.", userID)
		s.eventBus.Publish(context.Background(), bus.TopicRulesChanged, userID)
	}
}

func (s *SyncService) ApplyRulesToAllTransactions(userID string) error {

	txs, err := s.transactionRepo.List(userID)
	if err != nil {
		return err
	}

	// Cache MIKs and Account Metadata to avoid redundant derivation/decryption
	mikCache := make(map[string][]byte)
	accountTagsCache := make(map[string]map[string]string)

	// Pre-load all available integration MIKs for recovery
	integrations, _ := s.integrationRepo.List(userID)
	for _, i := range integrations {
		if _, ok := mikCache[i.ID]; !ok {
			mik, err := s.GetMasterKey(userID, i.ID)
			if err == nil {
				mikCache[i.ID] = mik
			}
		}
	}

	activeIntegrations, _ := s.GetAccountActiveIntegrations(userID, mikCache, integrations)

	for _, t := range txs {
		if _, ok := mikCache[t.IntegrationID]; !ok {
			continue
		}

		if _, ok := accountTagsCache[t.IntegrationID]; !ok {
			// Find the integration in the list we already fetched
			var integration *domain.Integration
			for _, i := range integrations {
				if i.ID == t.IntegrationID {
					integration = &i
					break
				}
			}

			if integration != nil {
				decrypted, err := s.DecryptIntegrationConfig(userID, integration)
				if err == nil {
					var config struct {
						AccountsMetadata map[string]*domain.AccountMeta `json:"accounts_metadata"`
					}
					if err := json.Unmarshal(decrypted, &config); err == nil {
						tagsMap := make(map[string]string)
						for accID, meta := range config.AccountsMetadata {
							if meta != nil {
								tagsMap[accID] = meta.Tags
							}
						}
						accountTagsCache[t.IntegrationID] = tagsMap
					}
				}
			}
		}

		data, err := s.DecryptTransaction(userID, &t, mikCache, activeIntegrations)
		if err != nil {
			continue
		}

		// Identify provider to parse data correctly
		var integration *domain.Integration
		for _, i := range integrations {
			if i.ID == t.IntegrationID {
				integration = &i
				break
			}
		}
		if integration == nil {
			continue
		}

		provider := s.integrationRegistry.Get(integration.ServiceType)
		if provider == nil {
			continue
		}

		meta, err := provider.ParseTransaction(data, t.AccountID)
		if err != nil {
			log.Printf("[SYNC] Failed to parse transaction %s: %v", t.ID, err)
			continue
		}

		accountTags := ""
		if tagsMap, ok := accountTagsCache[t.IntegrationID]; ok {
			accountTags = tagsMap[t.AccountID]
		}

		poolIDs, _ := s.ruleService.ProcessTransaction(userID, t.IntegrationID, meta.Receiver, meta.Description, t.Tags, accountTags, meta.Amount)

		// Check if pool_ids actually changed
		changed := len(poolIDs) != len(t.PoolIDs)
		if !changed {
			poolMap := make(map[string]bool)
			for _, p := range t.PoolIDs {
				poolMap[p] = true
			}
			for _, p := range poolIDs {
				if !poolMap[p] {
					changed = true
					break
				}
			}
		}

		if changed {
			s.transactionRepo.UpdatePools(userID, t.ID, poolIDs)
		}
	}

	return nil
}

func (s *SyncService) CheckAndCorrectTransactionTimestamps(userID string) {

	txs, err := s.transactionRepo.List(userID)
	if err != nil {
		log.Printf("[SYNC] Failed to load transactions for verification: %v", err)
		return
	}

	integrations, err := s.integrationRepo.List(userID)
	if err != nil {
		log.Printf("[SYNC] Failed to list integrations: %v", err)
		return
	}

	serviceTypeMap := make(map[string]string)
	mikCache := make(map[string][]byte)
	for _, i := range integrations {
		serviceTypeMap[i.ID] = i.ServiceType
		mk, err := s.GetMasterKey(userID, i.ID)
		if err == nil {
			mikCache[i.ID] = mk
		}
	}

	activeIntegrations, _ := s.GetAccountActiveIntegrations(userID, mikCache, integrations)

	existingExternalIDs := make(map[string]string)
	for _, tx := range txs {
		if tx.ExternalID != "" {
			existingExternalIDs[tx.ExternalID] = tx.ID
		}
	}

	for _, t := range txs {
		serviceType, ok := serviceTypeMap[t.IntegrationID]
		if !ok {
			continue
		}

		data, err := s.DecryptTransaction(userID, &t, mikCache, activeIntegrations)
		if err != nil {
			continue
		}

		provider := s.integrationRegistry.Get(serviceType)
		if provider == nil {
			continue
		}

		meta, err := provider.ParseTransaction(data, t.AccountID)
		if err != nil {
			log.Printf("[SYNC] Failed to parse transaction %s for correction: %v", t.ID, err)
			continue
		}

		correctTime := meta.CreatedAt
		correctExternalID := meta.ExternalID

		needsCorrection := !t.CreatedAt.Equal(correctTime) || t.ExternalID != correctExternalID
		if needsCorrection {
			// Duplicate check: see if another transaction already possesses this correct ID
			duplicateTxID, exists := existingExternalIDs[correctExternalID]
			if exists && duplicateTxID != t.ID {
				log.Printf("[SYNC] [DUPLICATE] Mismatching duplicate detected: tx %s has incorrect ExternalID %s (should be %s). The correct transaction is %s. Database UNIQUE constraint will prevent normal correction.", t.ID, t.ExternalID, correctExternalID, duplicateTxID)
			}

			log.Printf("[SYNC] Mismatch detected for tx %s (Service: %s):\n  -> DB CreatedAt: %v vs True CreatedAt: %v\n  -> DB ExternalID: %s vs True ExternalID: %s. Correcting...", t.ID, serviceType, t.CreatedAt, correctTime, t.ExternalID, correctExternalID)
			if err := s.transactionRepo.UpdateTimestampAndExternalID(userID, t.ID, correctTime, correctExternalID); err != nil {
				log.Printf("[SYNC] Failed to correct transaction %s: %v", t.ID, err)
			} else {
				log.Printf("[SYNC] Successfully updated transaction %s in database.", t.ID)
				// Update local map to reflect the new state
				if t.ExternalID != "" {
					delete(existingExternalIDs, t.ExternalID)
				}
				existingExternalIDs[correctExternalID] = t.ID
			}
		}
	}
}

type TempLogger struct {
}

func (t *TempLogger) Debug(v ...interface{}) {

	log.Printf("[DEBUG] %v", v)
}

func (s *SyncService) RetroactivelyFixGoCardlessSigns() {
	log.Printf("[MIGRATION] Starting retroactive GoCardless sign and peer correction...")
	integrations, err := s.integrationRepo.ListAll()
	if err != nil {
		log.Printf("[MIGRATION] Failed to list integrations: %v", err)
		return
	}

	fixedCount := 0
	for _, i := range integrations {
		if i.ServiceType != "GOCARDLESS" {
			continue
		}

		masterKey, err := s.GetMasterKey(i.UserID, i.ID)
		if err != nil {
			log.Printf("[MIGRATION][%s] Failed to get master key: %v", i.Name, err)
			continue
		}

		// Read integration config to get account tags
		decryptedConfig, err := s.DecryptIntegrationConfig(i.UserID, &i)
		if err != nil {
			log.Printf("[MIGRATION][%s] Failed to decrypt integration config: %v", i.Name, err)
			continue
		}

		var config struct {
			AccountsMetadata map[string]*domain.AccountMeta `json:"accounts_metadata"`
		}
		if err := json.Unmarshal(decryptedConfig, &config); err != nil {
			log.Printf("[MIGRATION][%s] Failed to unmarshal integration config: %v", i.Name, err)
			continue
		}

		txs, err := s.transactionRepo.ListByIntegration(i.UserID, i.ID)
		if err != nil {
			log.Printf("[MIGRATION][%s] Failed to list transactions: %v", i.Name, err)
			continue
		}

		mikCache := map[string][]byte{i.ID: masterKey}

		for _, tx := range txs {
			decrypted, err := s.DecryptTransaction(i.UserID, &tx, mikCache, nil)
			if err != nil {
				log.Printf("[MIGRATION][%s] Failed to decrypt tx %s", i.Name, tx.ID)
				continue
			}

			// We need to know if it's already a GenericTransaction or raw GoCardless data
			var txMap map[string]interface{}
			json.Unmarshal(decrypted, &txMap)

			var amt float64
			var receiver string
			var receiverIBAN string
			var desc string
			var externalID string
			var createdAt time.Time

			// Check if it's a GenericTransaction (has "Peer" or "Amount" as float)
			isGeneric := false
			if _, ok := txMap["Peer"]; ok {
				isGeneric = true
			} else if _, ok := txMap["peer"]; ok {
				isGeneric = true
			}

			if isGeneric {
				var gtx domain.GenericTransaction
				json.Unmarshal(decrypted, &gtx)
				amt = gtx.Amount
				desc = gtx.Description
				createdAt = gtx.CreatedAt
				externalID = gtx.ExternalID
				// We still need to re-calculate receiver to see if it was wrong
				// But we need the RAW data for that.
				// If it's already generic, the raw data is GONE unless we stored it inside?
				// GC provider doesn't seem to store raw data inside GenericTransaction.
				// So if it's already generic, we can't easily fix the peer if it was wrong.
				// However, GC's old logic was "prefer Debtor", and for expenses the debtor is the USER.
				// So if Peer == User's Name, it's probably wrong.

				// For now, let's see if we can find raw data.
				// If we can't, we skip peer fix but can still fix pools.
				receiver = gtx.Peer
				receiverIBAN = gtx.PeerIBAN
			} else {
				// Raw GoCardless data
				// Use the provider's ParseTransaction logic (which we just fixed)
				provider := s.integrationRegistry.Get("GOCARDLESS")
				meta, err := provider.ParseTransaction(decrypted, tx.AccountID)
				if err != nil {
					continue
				}
				amt = meta.Amount
				receiver = meta.Receiver
				receiverIBAN = meta.ReceiverIBAN
				desc = meta.Description
				externalID = meta.ExternalID
				createdAt = meta.CreatedAt
			}

			needsPoolFix := len(tx.PoolIDs) == 0

			// For GC, we especially want to fix cases where the receiver is the user themselves (old broken logic)
			// But without the user's name at hand, it's hard.
			// However, re-running ParseTransaction on RAW data WILL fix it because it's now direction-aware.

			if !isGeneric || needsPoolFix {
				accountTags := ""
				if meta, ok := config.AccountsMetadata[tx.AccountID]; ok && meta != nil {
					accountTags = meta.Tags
				}

				poolIDs, _ := s.ruleService.ProcessTransaction(tx.UserID, tx.IntegrationID, receiver, desc, "", accountTags, amt)
				tx.PoolIDs = poolIDs

				genericTx := domain.GenericTransaction{
					Amount:      amt,
					Description: desc,
					Peer:        receiver,
					PeerIBAN:    receiverIBAN,
					CreatedAt:   createdAt,
					ExternalID:  externalID,
				}

				newJSON, _ := json.Marshal(genericTx)
				encrypted, _ := s.cryptoService.Encrypt(masterKey, newJSON)
				tx.EncryptedData = base64.StdEncoding.EncodeToString(encrypted)

				err = s.transactionRepo.SaveBulk(tx.UserID, []domain.BankTransaction{tx})
				if err == nil {
					fixedCount++
				}
			}
		}
	}
	log.Printf("[MIGRATION] Finished GoCardless correction. Fixed %d transactions.", fixedCount)
}

func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func Ptr[T any](v T) *T {
	return &v
}

func (s *SyncService) DeduplicateAndCorrectExternalIDs() {
	log.Printf("[DEDUPLICATE] Starting database deduplication and external ID correction...")

	// 1. Run database backup first
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://budget:budgetpass@db:5432/budget?sslmode=disable"
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/app/data"
	}
	db.BackupDB(dbURL, dataDir)

	users, err := s.userRepo.ListAll()
	if err != nil {
		log.Printf("[DEDUPLICATE] Failed to list users: %v", err)
		return
	}

	for _, user := range users {
		log.Printf("[DEDUPLICATE] Processing user %s...", user.Username)

		mikCache := make(map[string][]byte)
		integrations, err := s.integrationRepo.List(user.ID)
		if err != nil {
			log.Printf("[DEDUPLICATE] Failed to list integrations for user %s: %v", user.Username, err)
			continue
		}

		for _, i := range integrations {
			mk, err := s.GetMasterKey(user.ID, i.ID)
			if err == nil {
				mikCache[i.ID] = mk
			}
		}

		// List ALL transactions (including soft-deleted ones)
		txs, err := s.transactionRepo.ListAll(user.ID)
		if err != nil {
			log.Printf("[DEDUPLICATE] Failed to list transactions for user %s: %v", user.Username, err)
			continue
		}

		log.Printf("[DEDUPLICATE] User %s: Found %d total transactions in DB.", user.Username, len(txs))

		// Map to keep track of processed external IDs to detect duplicates.
		// Key: correct_external_id -> transaction ID in DB
		seenExternalIDs := make(map[string]string)

		correctedCount := 0
		deletedCount := 0

		// Sort: active (IsDeleted=false) comes first, then sorted by CreatedAt DESC (newest first).
		// That way, we process/keep the active, newest transaction as the primary one.
		sort.Slice(txs, func(i, j int) bool {
			if txs[i].IsDeleted != txs[j].IsDeleted {
				return !txs[i].IsDeleted
			}
			return txs[i].CreatedAt.After(txs[j].CreatedAt)
		})

		// 1. First pass: Correct ExternalIDs and group by composite key
		// Key: compositeKey -> map[correlationID] -> list of transactions
		groups := make(map[string]map[string][]domain.BankTransaction)
		seenExternalIDs := make(map[string]string)

		correctedCount := 0
		deletedCount := 0

		for _, tx := range txs {
			// Find integration type
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

			// Only deduplicate/correct GoCardless and Enable Banking
			if integrationObj.ServiceType != "GOCARDLESS" && integrationObj.ServiceType != "ENABLEBANKING" {
				continue
			}

			// Decrypt transaction
			data, err := s.DecryptTransaction(user.ID, &tx, mikCache, nil)
			if err != nil {
				log.Printf("[DEDUPLICATE] Warning: Failed to decrypt transaction %s: %v", tx.ID, err)
				continue
			}

			// Get provider
			provider := s.GetProvider(integrationObj.ServiceType)
			if provider == nil {
				continue
			}

			// Parse transaction to extract correct external ID and metadata
			meta, err := provider.ParseTransaction(data, tx.AccountID)
			if err != nil {
				log.Printf("[DEDUPLICATE] Warning: Failed to parse transaction %s: %v", tx.ID, err)
				continue
			}

			correctExternalID := meta.ExternalID
			if correctExternalID != "" && tx.ExternalID != correctExternalID {
				log.Printf("[DEDUPLICATE] Correcting external_id for transaction %s: '%s' -> '%s'", tx.ID, tx.ExternalID, correctExternalID)
				if err := s.transactionRepo.UpdateExternalID(user.ID, tx.ID, correctExternalID); err != nil {
					log.Printf("[DEDUPLICATE] Failed to update external_id for transaction %s: %v", tx.ID, err)
				} else {
					tx.ExternalID = correctExternalID
					correctedCount++
				}
			}

			// Create composite key for occurrence-based check
			// Signature: date | amount | receiver | description
			dk := fmt.Sprintf("%s|%.2f|%s|%s",
				tx.CreatedAt.Format("2006-01-02"),
				math.Abs(meta.Amount),
				strings.ToUpper(strings.TrimSpace(meta.Receiver)),
				strings.TrimSpace(meta.Description),
			)

			if groups[dk] == nil {
				groups[dk] = make(map[string][]domain.BankTransaction)
			}
			cID := tx.CorrelationID
			if cID == "" {
				cID = "MANUAL_" + tx.ID // Treat manual/missing CID as unique
			}
			groups[dk][cID] = append(groups[dk][cID], tx)
		}

		// 2. Second pass: Deduplicate based on max occurrences per sync run
		for dk, syncRuns := range groups {
			// Find the maximum number of times this transaction appeared in any SINGLE sync run
			maxPerRun := 0
			allInGroup := []domain.BankTransaction{}
			for _, txsInRun := range syncRuns {
				if len(txsInRun) > maxPerRun {
					maxPerRun = len(txsInRun)
				}
				allInGroup = append(allInGroup, txsInRun...)
			}

			if len(allInGroup) <= maxPerRun {
				continue // No duplicates
			}

			// We have duplicates!
			// Sort the whole group: active first, then newest, then those with external IDs
			sort.Slice(allInGroup, func(i, j int) bool {
				if allInGroup[i].IsDeleted != allInGroup[j].IsDeleted {
					return !allInGroup[i].IsDeleted
				}
				if allInGroup[i].ExternalID != "" && allInGroup[j].ExternalID == "" {
					return true
				}
				if allInGroup[i].ExternalID == "" && allInGroup[j].ExternalID != "" {
					return false
				}
				return allInGroup[i].SyncedAt.After(allInGroup[j].SyncedAt)
			})

			// Also, we must respect the UNIQUE(user_id, external_id) constraint.
			// If multiple transactions in this group have the SAME external_id, they are definitely duplicates.
			// We'll track seen ExternalIDs within this group.
			keptExternalIDs := make(map[string]bool)
			keptCount := 0

			for _, tx := range allInGroup {
				isDuplicate := false

				// Rule 1: Strict ExternalID uniqueness
				if tx.ExternalID != "" {
					if keptExternalIDs[tx.ExternalID] {
						isDuplicate = true
					}
				}

				// Rule 2: Occurrence limit
				if !isDuplicate && keptCount >= maxPerRun {
					isDuplicate = true
				}

				if isDuplicate {
					log.Printf("[DEDUPLICATE] Deleting duplicate transaction %s (Group: %s, CID: %s)", tx.ID, dk, tx.CorrelationID)
					if err := s.transactionRepo.HardDelete(user.ID, tx.ID); err != nil {
						log.Printf("[DEDUPLICATE] Failed to hard delete transaction %s: %v", tx.ID, err)
					} else {
						deletedCount++
					}
				} else {
					keptCount++
					if tx.ExternalID != "" {
						keptExternalIDs[tx.ExternalID] = true
					}
				}
			}
		}

		log.Printf("[DEDUPLICATE] User %s: Deduplication complete. Corrected: %d, Deleted: %d.", user.Username, correctedCount, deletedCount)
	}
}
