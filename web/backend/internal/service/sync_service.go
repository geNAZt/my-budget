package service

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/bus"
	"github.com/genazt/my-budget-script/web/backend/internal/crypto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/integration"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	"github.com/genazt/my-budget-script/web/backend/pkg/apis/enablebanking"
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

func (s *SyncService) RetroactivelyFixEnableBankingSigns() {

	log.Printf("[MIGRATION] Starting retroactive Enable Banking sign correction...")
	integrations, err := s.integrationRepo.ListAll()
	if err != nil {
		log.Printf("[MIGRATION] Failed to list integrations: %v", err)
		return
	}

	fixedCount := 0
	for _, i := range integrations {
		if i.ServiceType != "ENABLEBANKING" {
			continue
		}

		masterKey, err := s.GetMasterKey(i.UserID, i.ID)
		if err != nil {
			log.Printf("[MIGRATION][%s] Failed to get master key: %v", i.Name, err)
			continue
		}

		// Read integration config to get account IBANs
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

		for _, tx := range txs {
			decrypted, err := s.DecryptTransaction(i.UserID, &tx, map[string][]byte{i.ID: masterKey}, nil)
			if err != nil {
				log.Printf("[MIGRATION][%s] Failed to decrypt tx %s", i.Name, tx.ID)
				continue
			}

			var ebTx enablebanking.Transaction
			if err := json.Unmarshal(decrypted, &ebTx); err != nil {
				log.Printf("[MIGRATION][%s] Failed to unmarshal ebTx %s: %v", i.Name, tx.ID, err)
				continue
			}

			if ebTx.TransactionAmount != nil && ebTx.TransactionAmount.Amount != nil {
				amt, _ := strconv.ParseFloat(*ebTx.TransactionAmount.Amount, 64)
				isDebit := false

				// Normalize fields for frontend if missing in encrypted data
				var txMap map[string]interface{}
				json.Unmarshal(decrypted, &txMap)

				// New Simple IBAN Logic:
				myIBAN := ""
				if config.AccountsMetadata != nil && config.AccountsMetadata[tx.AccountID] != nil {
					myIBAN = strings.ReplaceAll(strings.ToUpper(config.AccountsMetadata[tx.AccountID].IBAN), " ", "")
				}

				isSet := false

				// Check CreditDebitIndicator (Most reliable source)
				indicator := ""
				if ebTx.CreditDebitIndicator != nil {
					indicator = *ebTx.CreditDebitIndicator
				} else if val, ok := txMap["credit_debit_indicator"].(string); ok {
					indicator = val
				}

				if strings.ToUpper(indicator) == "DBIT" {
					isDebit = true
					isSet = true
				} else if strings.ToUpper(indicator) == "CRDT" {
					isDebit = false
					isSet = true
				}

				if !isSet && myIBAN != "" {
					if ebTx.Debtor != nil && ebTx.Debtor.Account != nil && ebTx.Debtor.Account.Iban != nil {
						debtorIBAN := strings.ReplaceAll(strings.ToUpper(*ebTx.Debtor.Account.Iban), " ", "")
						if debtorIBAN == myIBAN {
							isDebit = true
							isSet = true
						}
					}

					if !isSet && ebTx.Creditor != nil && ebTx.Creditor.Account != nil && ebTx.Creditor.Account.Iban != nil {
						creditorIBAN := strings.ReplaceAll(strings.ToUpper(*ebTx.Creditor.Account.Iban), " ", "")
						if creditorIBAN == myIBAN {
							isDebit = false
							isSet = true
						}
					}
				}

				if !isSet {
					if ebTx.Creditor != nil && ebTx.Creditor.Name != nil {
						// Fallback: If Creditor is present, it's likely an outgoing payment
						isDebit = true
						isSet = true
					} else if ebTx.Debtor != nil && ebTx.Debtor.Account != nil {
						// Fallback to Debtor presence
						isDebit = true
						isSet = true
					}
				}

				needsUpdate := false

				// Fix sign/account mapping if necessary
				if isDebit && amt > 0 && tx.DestinationAccountID != "" {
					tx.SourceAccountID = tx.AccountID
					tx.DestinationAccountID = ""
					needsUpdate = true
					log.Printf("[MIGRATION][%s] Correcting sign for tx %s (Amt: %f, Indicator: %s, isDebit: %v)", i.Name, tx.ID, amt, indicator, isDebit)
				}

				receiver := ""
				if ebTx.Creditor != nil && ebTx.Creditor.Name != nil {
					receiver = *ebTx.Creditor.Name
				} else if ebTx.Debtor != nil && ebTx.Debtor.Account != nil && ebTx.Debtor.Name != nil {
					receiver = *ebTx.Debtor.Name
				}

				desc := ""
				if ebTx.RemittanceInformation != nil && len(*ebTx.RemittanceInformation) > 0 {
					desc = strings.Join(*ebTx.RemittanceInformation, " ")
				}

				hasDescription := txMap["description"] != nil && txMap["description"] != ""
				hasCreditorName := txMap["creditor_name"] != nil && txMap["creditor_name"] != ""

				if !hasDescription || !hasCreditorName || (isDebit && amt > 0) || txMap["amount"] == nil {
					txMap["description"] = desc
					txMap["creditor_name"] = receiver
					txMap["amount"] = amt
					if isDebit {
						txMap["amount"] = -math.Abs(amt)
						// Also update the nested amount string that the frontend often uses
						if txMap["transaction_amount"] != nil {
							if ta, ok := txMap["transaction_amount"].(map[string]interface{}); ok {
								ta["amount"] = fmt.Sprintf("%.2f", -math.Abs(amt))
							}
						}
					}

					newJSON, _ := json.Marshal(txMap)
					encrypted, _ := s.cryptoService.Encrypt(masterKey, newJSON)
					tx.EncryptedData = base64.StdEncoding.EncodeToString(encrypted)
					needsUpdate = true
					log.Printf("[MIGRATION][%s] Injecting names/signs for tx %s (Desc: %s, Receiver: %s, Amt: %v, Indicator: %s, isDebit: %v)", i.Name, tx.ID, desc, receiver, txMap["amount"], indicator, isDebit)
				}

				if needsUpdate {
					// Update the database record
					err = s.transactionRepo.SaveBulk(tx.UserID, []domain.BankTransaction{tx})
					if err == nil {
						fixedCount++
					} else {
						log.Printf("[MIGRATION][%s] Failed to save fixed tx %s: %v", i.Name, tx.ID, err)
					}
				}
			} else {
				log.Printf("[MIGRATION][%s] Skipping tx %s: TransactionAmount is nil", i.Name, tx.ID)
			}
		}
	}
	log.Printf("[MIGRATION] Finished Enable Banking sign correction. Fixed %d transactions.", fixedCount)
}

func (s *SyncService) RetroactivelyAssignAccounts() {

	log.Printf("[CRYPTO] Starting retroactive account assignment migration...")
	integrations, err := s.integrationRepo.ListAll()
	if err != nil {
		log.Printf("[CRYPTO] Migration failed to list integrations: %v", err)
		return
	}

	for _, i := range integrations {
		if i.ServiceType == "TRADING212" {
			s.transactionRepo.MigrateEmptyAccountIDs(i.ID, "T212_PORTFOLIO")
			continue
		}

		if i.ServiceType == "GOCARDLESS" {
			decrypted, err := s.DecryptIntegrationConfig(i.UserID, &i)
			if err != nil {
				continue
			}

			var config struct {
				AccountIDs       []string `json:"account_ids"`
				LegacyAccountIDs []string `json:"accounts"`
			}
			if err := json.Unmarshal(decrypted, &config); err != nil {
				continue
			}

			ids := config.AccountIDs
			if len(ids) == 0 {
				ids = config.LegacyAccountIDs
			}

			// If only one account exists, we can safely assume legacy transactions belong to it
			if len(ids) == 1 {
				s.transactionRepo.MigrateEmptyAccountIDs(i.ID, ids[0])
			}
		}
	}
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

		meta, err := provider.ParseTransaction(data)
		if err != nil {
			log.Printf("[SYNC] Failed to parse transaction %s: %v", t.ID, err)
			continue
		}

		accountTags := ""
		if tagsMap, ok := accountTagsCache[t.IntegrationID]; ok {
			accountTags = tagsMap[t.AccountID]
		}

		poolID, _ := s.ruleService.ProcessTransaction(userID, t.IntegrationID, meta.Receiver, meta.Description, t.Tags, accountTags, meta.Amount)

		// Only update if pool_id actually changed
		if (poolID == nil && t.PoolID != nil) || (poolID != nil && t.PoolID == nil) || (poolID != nil && t.PoolID != nil && *poolID != *t.PoolID) {
			s.transactionRepo.UpdatePool(userID, t.ID, poolID)
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

		meta, err := provider.ParseTransaction(data)
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
