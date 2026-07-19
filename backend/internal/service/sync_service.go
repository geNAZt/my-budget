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
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/genazt/my-budget-script/backend/internal/bus"
	"github.com/genazt/my-budget-script/backend/internal/crypto"
	"github.com/genazt/my-budget-script/backend/internal/db"
	"github.com/genazt/my-budget-script/backend/internal/domain"
	"github.com/genazt/my-budget-script/backend/internal/integration"
	"github.com/genazt/my-budget-script/backend/internal/repository"
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

	// Thread-safe caches to prevent N+1 query floods
	masterKeyCache          map[string]map[string][]byte // userID -> integrationID -> MIK
	activeIntegrationsCache map[string]map[string]string // userID -> accountID -> integrationID
	cacheMu                 sync.RWMutex
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
		integrationRepo:         ir,
		transactionRepo:         tr,
		connRepo:                cr,
		assetRepo:               ar,
		userRepo:                ur,
		cryptoService:           cs,
		integrationRegistry:     ir_registry,
		ruleService:             rs,
		masterKeyCache:          make(map[string]map[string][]byte),
		activeIntegrationsCache: make(map[string]map[string]string),
		eventBus:                eventBus,
	}
}

func (s *SyncService) GetRegistry() *integration.Registry {
	return s.integrationRegistry
}

func (s *SyncService) ReconcileBlockedFunds() {
	log.Printf("[RECONCILE] Starting blocked funds reconciliation...")

	users, _ := s.userRepo.ListAll()
	for _, user := range users {
		// 1. Find all PENDING_REJECTION transactions
		blocked, err := s.transactionRepo.ListByInternalStatus(user.ID, "PENDING_REJECTION")
		if err != nil || len(blocked) == 0 {
			continue
		}

		// 2. Load all transactions for matching
		allTxs, _ := s.transactionRepo.ListAll(user.ID)

		// Pre-calculate master keys
		integrations, _ := s.integrationRepo.List(user.ID)
		mikCache := make(map[string][]byte)
		for _, i := range integrations {
			if mk, err := s.GetMasterKey(user.ID, i.ID); err == nil {
				mikCache[i.ID] = mk
			}
		}

		// Helper to resolve a block
		resolveBlock := func(btxID string, status string, correlationID string) {
			s.transactionRepo.UpdateInternalStatus(user.ID, btxID, status)
			s.transactionRepo.Delete(user.ID, btxID)
		}

		for _, btx := range blocked {
			log.Printf("[RECONCILE] Investigating blocked fund: %s (%s, ExternalID: %s)", btx.ID, btx.CorrelationID, btx.ExternalID)

			// Expiry check: 14 days
			if btx.CreatedAt.Before(time.Now().AddDate(0, 0, -14)) {
				log.Printf("[RECONCILE] CID %s: Expiring blocked fund %s (14 days passed)", btx.CorrelationID, btx.ID)
				resolveBlock(btx.ID, "EXPIRED_REJECTION", btx.CorrelationID)
				continue
			}

			bData, err := s.DecryptTransaction(user.ID, &btx, mikCache, nil)
			if err != nil {
				log.Printf("[RECONCILE] Failed to decrypt block %s: %v", btx.ID, err)
				continue
			}

			provider := s.GetProvider("ENABLEBANKING")
			bMeta, err := provider.ParseTransaction(bData, btx.AccountID)
			if err != nil {
				log.Printf("[RECONCILE] Failed to parse block %s: %v", btx.ID, err)
				continue
			}

			bReceiver := strings.ToUpper(strings.TrimSpace(bMeta.Receiver))
			bAmountAbs := math.Abs(bMeta.Amount)
			log.Printf("[RECONCILE] Block metadata: Amount=%.2f, Receiver='%s', CreatedAt=%v", bMeta.Amount, bReceiver, btx.CreatedAt)

			// 1. Try to find a SINGLE exact match (Amount + Merchant)
			foundExact := false
			for _, dbtx := range allTxs {
				if dbtx.ID == btx.ID || dbtx.InternalStatus != "" || dbtx.IsDeleted {
					continue
				}
				if dbtx.AccountID != btx.AccountID {
					continue
				}

				// Check date window (-2 days to +14 days)
				windowStart := btx.CreatedAt.AddDate(0, 0, -2)
				windowEnd := btx.CreatedAt.AddDate(0, 0, 14)

				if (dbtx.CreatedAt.Equal(windowStart) || dbtx.CreatedAt.After(windowStart)) && dbtx.CreatedAt.Before(windowEnd) {
					dData, err := s.DecryptTransaction(user.ID, &dbtx, mikCache, nil)
					if err != nil {
						continue
					}

					dIntegration, _ := s.integrationRepo.GetByID(user.ID, dbtx.IntegrationID)
					if dIntegration == nil {
						continue
					}
					dProvider := s.GetProvider(dIntegration.ServiceType)
					dMeta, _ := dProvider.ParseTransaction(dData, dbtx.AccountID)
					dReceiver := strings.ToUpper(strings.TrimSpace(dMeta.Receiver))
					dAmountAbs := math.Abs(dMeta.Amount)

					if (bReceiver == dReceiver || strings.Contains(dReceiver, bReceiver) || strings.Contains(bReceiver, dReceiver)) && math.Abs(dAmountAbs-bAmountAbs) < 0.01 {
						log.Printf("[RECONCILE] CID %s: Found EXACT match for blocked fund %s: success %s (%.2f)", btx.CorrelationID, btx.ID, dbtx.ID, dMeta.Amount)
						resolveBlock(btx.ID, "RECONCILED", btx.CorrelationID)
						foundExact = true
						break
					}
				}
			}

			if foundExact {
				continue
			}

			// 2. Fallback to Split Settlement Logic
			var merchantSuccesses []integration.TransactionMetadata
			for _, dbtx := range allTxs {
				if dbtx.ID == btx.ID || dbtx.InternalStatus != "" || dbtx.IsDeleted {
					continue
				}
				if dbtx.AccountID != btx.AccountID {
					continue
				}

				windowStart := btx.CreatedAt.AddDate(0, 0, -2)
				windowEnd := btx.CreatedAt.AddDate(0, 0, 14)

				if (dbtx.CreatedAt.Equal(windowStart) || dbtx.CreatedAt.After(windowStart)) && dbtx.CreatedAt.Before(windowEnd) {
					dData, err := s.DecryptTransaction(user.ID, &dbtx, mikCache, nil)
					if err != nil {
						continue
					}

					dIntegration, _ := s.integrationRepo.GetByID(user.ID, dbtx.IntegrationID)
					if dIntegration == nil {
						continue
					}
					dProvider := s.GetProvider(dIntegration.ServiceType)
					dMeta, _ := dProvider.ParseTransaction(dData, dbtx.AccountID)
					dReceiver := strings.ToUpper(strings.TrimSpace(dMeta.Receiver))

					if bReceiver == dReceiver || strings.Contains(dReceiver, bReceiver) || strings.Contains(bReceiver, dReceiver) {
						merchantSuccesses = append(merchantSuccesses, dMeta)
					}
				}
			}

			successSum := 0.0
			for _, m := range merchantSuccesses {
				successSum += math.Abs(m.Amount)
			}

			if successSum > 0 && math.Abs(successSum-bAmountAbs) < 0.01 {
				log.Printf("[RECONCILE] CID %s: Matching blocked fund %s (%.2f) with %d settlements summing to %.2f", btx.CorrelationID, btx.ID, bMeta.Amount, len(merchantSuccesses), successSum)
				resolveBlock(btx.ID, "RECONCILED", btx.CorrelationID)
			}
		}
	}
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

func (s *SyncService) AutoLinkInternalTransfers() {
	log.Printf("[LINK] Starting internal transfer auto-linking...")

	users, _ := s.userRepo.ListAll()
	for _, user := range users {
		ibanMap := make(map[string]string)
		aliasMap := make(map[string]string)
		integrations, _ := s.integrationRepo.List(user.ID)
		mikCache := make(map[string][]byte)

		for _, i := range integrations {
			if mk, err := s.GetMasterKey(user.ID, i.ID); err == nil {
				mikCache[i.ID] = mk

				configBytes, err := s.DecryptIntegrationConfig(user.ID, &i)
				if err == nil {
					var config struct {
						AccountsMetadata map[string]*domain.AccountMeta `json:"accounts_metadata"`
					}
					if err := json.Unmarshal(configBytes, &config); err == nil {
						for accID, meta := range config.AccountsMetadata {
							if meta != nil {
								if meta.IBAN != "" {
									cleanIBAN := strings.ReplaceAll(strings.ToUpper(meta.IBAN), " ", "")
									ibanMap[cleanIBAN] = accID
								}
								if meta.Alias != "" {
									aliasMap[accID] = strings.TrimSpace(meta.Alias)
								}
							}
						}
					}
				}
			}
		}

		if len(ibanMap) == 0 {
			continue
		}

		allTxs, _ := s.transactionRepo.ListAll(user.ID)
		var candidates []domain.BankTransaction
		for _, tx := range allTxs {
			if !tx.IsLinkConfirmed && !tx.IsDeleted {
				candidates = append(candidates, tx)
			}
		}

		// 3. Match pairs
		linkedIDs := make(map[string]bool)
		for _, tx := range candidates {
			if linkedIDs[tx.ID] {
				continue
			}

			// Decrypt to get details
			data, err := s.DecryptTransaction(user.ID, &tx, mikCache, nil)
			if err != nil {
				continue
			}

			var genTx domain.GenericTransaction
			if err := json.Unmarshal(data, &genTx); err != nil {
				continue
			}

			cleanPeerIBAN := strings.ReplaceAll(strings.ToUpper(genTx.PeerIBAN), " ", "")
			cleanDesc := strings.TrimSpace(genTx.Description)

			// Strategy A: IBAN Match
			if targetAccID, ok := ibanMap[cleanPeerIBAN]; ok && targetAccID != tx.AccountID {
				for _, other := range candidates {
					if other.ID == tx.ID || other.AccountID != targetAccID || linkedIDs[other.ID] {
						continue
					}

					otherData, err := s.DecryptTransaction(user.ID, &other, mikCache, nil)
					if err != nil {
						continue
					}
					var otherGenTx domain.GenericTransaction
					if err := json.Unmarshal(otherData, &otherGenTx); err != nil {
						continue
					}

					if math.Abs(genTx.Amount+otherGenTx.Amount) < 0.01 {
						diff := tx.CreatedAt.Sub(other.CreatedAt)
						if diff < 0 {
							diff = -diff
						}

						if diff <= 96*time.Hour {
							sourceAcc, destAcc := tx.AccountID, targetAccID
							if genTx.Amount > 0 {
								sourceAcc, destAcc = targetAccID, tx.AccountID
							}

							log.Printf("[LINK] Linking internal transfer (IBAN match): %s <-> %s", tx.ID, other.ID)
							if err := s.transactionRepo.LinkInternalTransfer(user.ID, tx.ID, other.ID, sourceAcc, destAcc); err == nil {
								linkedIDs[tx.ID] = true
								linkedIDs[other.ID] = true
							}
							break
						}
					}
				}
			}

			if linkedIDs[tx.ID] {
				continue
			}

			// Strategy B: Fallback Description + Amount Match (for missing IBANs)
			if cleanDesc != "" {
				for _, other := range candidates {
					if other.ID == tx.ID || other.AccountID == tx.AccountID || linkedIDs[other.ID] {
						continue
					}

					otherData, err := s.DecryptTransaction(user.ID, &other, mikCache, nil)
					if err != nil {
						continue
					}
					var otherGenTx domain.GenericTransaction
					if err := json.Unmarshal(otherData, &otherGenTx); err != nil {
						continue
					}

					match := false
					d1 := strings.ToLower(cleanDesc)
					d2 := strings.ToLower(strings.TrimSpace(otherGenTx.Description))

					if d1 == d2 && d1 != "" {
						match = true
					} else {
						aliasTx := strings.ToLower(aliasMap[tx.AccountID])
						aliasOther := strings.ToLower(aliasMap[other.AccountID])

						if len(aliasOther) >= 3 && d1 != "" && strings.Contains(d1, aliasOther) {
							match = true
						} else if len(aliasTx) >= 3 && d2 != "" && strings.Contains(d2, aliasTx) {
							match = true
						}
					}

					if math.Abs(genTx.Amount+otherGenTx.Amount) < 0.01 && match {
						diff := tx.CreatedAt.Sub(other.CreatedAt)
						if diff < 0 {
							diff = -diff
						}

						if diff <= 96*time.Hour {
							sourceAcc, destAcc := tx.AccountID, other.AccountID
							if genTx.Amount > 0 {
								sourceAcc, destAcc = other.AccountID, tx.AccountID
							}

							log.Printf("[LINK] Linking internal transfer (Fallback match): %s <-> %s", tx.ID, other.ID)
							if err := s.transactionRepo.LinkInternalTransfer(user.ID, tx.ID, other.ID, sourceAcc, destAcc); err == nil {
								linkedIDs[tx.ID] = true
								linkedIDs[other.ID] = true
							}
							break
						}
					}
				}
			}
		}
	}
}

func (s *SyncService) TraceMoneyMovementChains() {
	log.Printf("[LINK] Starting money movement chain tracing...")

	users, _ := s.userRepo.ListAll()
	for _, user := range users {
		allTxs, err := s.transactionRepo.ListAll(user.ID)
		if err != nil {
			continue
		}

		// 1. Separate expenses (unlinked) and received credits (linked)
		var expenses []domain.BankTransaction
		var transfers []domain.BankTransaction

		for _, tx := range allTxs {
			if tx.IsDeleted {
				continue
			}

			if !tx.IsLinkConfirmed && tx.AccountID != "" && tx.SourceAccountID == tx.AccountID {
				// We look at unlinked transactions where source == account (normal expense)
				// Note: DecryptTransaction is needed to check amount sign accurately
				expenses = append(expenses, tx)
			} else if tx.IsLinkConfirmed && tx.SourceAccountID != "" && tx.SourceAccountID != tx.AccountID {
				// Confirmed transfer: money moving between accounts
				// We care about the "received" side (Amount > 0)
				transfers = append(transfers, tx)
			}
		}

		// 2. Pre-load MIKs for decryption
		integrations, _ := s.integrationRepo.List(user.ID)
		mikCache := make(map[string][]byte)
		for _, i := range integrations {
			if mk, err := s.GetMasterKey(user.ID, i.ID); err == nil {
				mikCache[i.ID] = mk
			}
		}

		// 3. Match Expenses to Funding Transfers
		for _, ex := range expenses {
			// Decrypt expense to get amount
			exData, err := s.DecryptTransaction(user.ID, &ex, mikCache, nil)
			if err != nil {
				continue
			}
			var exGen domain.GenericTransaction
			if err := json.Unmarshal(exData, &exGen); err != nil || exGen.Amount >= 0 {
				continue // Only care about actual outgoing expenses
			}

			for _, tr := range transfers {
				if tr.AccountID != ex.AccountID {
					continue
				}

				// Decrypt transfer to verify amount
				trData, err := s.DecryptTransaction(user.ID, &tr, mikCache, nil)
				if err != nil {
					continue
				}
				var trGen domain.GenericTransaction
				if err := json.Unmarshal(trData, &trGen); err != nil || trGen.Amount <= 0 {
					continue // Only care about received funds
				}

				// Match conditions:
				// - Same absolute amount
				// - Within 48 hours
				if math.Abs(exGen.Amount+trGen.Amount) < 0.01 {
					diff := ex.CreatedAt.Sub(tr.CreatedAt)
					if diff < 0 {
						diff = -diff
					}

					if diff <= 48*time.Hour {
						// Found the chain! 
						// Log: Tracing chain: Merchant (Ex Account) -> Intermediary (Tr Account) -> Original Source (Tr Source)
						log.Printf("[LINK] Tracing chain: %s (%.2f) funded by transfer %s (source: %s)", ex.ID, exGen.Amount, tr.ID, tr.SourceAccountID)
						s.transactionRepo.UpdateSourceAccountID(user.ID, ex.ID, tr.SourceAccountID)
						break
					}
				}
			}
		}
	}
}

func (s *SyncService) SyncAll() {
	all, err := s.integrationRepo.ListAll()
	if err == nil {
		log.Printf("[SYNC] Total integrations in DB: %d", len(all))
		for _, i := range all {
			log.Printf("[SYNC] - %s (ID: %s, User: %s, Status: %s, Service: %s, LastError: %s)", i.Name, i.ID, i.UserID, i.Status, i.ServiceType, i.LastError)
		}
	}

	integrations, err := s.integrationRepo.ListAllActive()
	if err != nil {
		log.Printf("[SYNC] Failed to list integrations: %v", err)
		return
	}

	log.Printf("[SYNC] Syncing %d active integrations...", len(integrations))

	for _, i := range integrations {
		if i.BackoffUntil != nil && i.BackoffUntil.After(time.Now()) {
			log.Printf("[SYNC] Skipping %s (Backoff until %v)", i.Name, i.BackoffUntil)
			continue
		}

		log.Printf("[SYNC] Starting scheduled sync for %s (User: %s)...", i.Name, i.UserID)
		s.SyncIntegration(i.UserID, i.ID, false, nil)
	}

	s.ReconcileBlockedFunds()
	s.AutoLinkInternalTransfers()
	s.TraceMoneyMovementChains()
}

type syncMetadata struct {
	IntegrationID   string    `json:"integration_id"`
	IntegrationName string    `json:"integration_name"`
	ServiceType     string    `json:"service_type"`
	UserID          string    `json:"user_id"`
	Timestamp       time.Time `json:"timestamp"`
	Status          string    `json:"status"`
	Error           string    `json:"error,omitempty"`
}

func (s *SyncService) SyncIntegration(userID string, integrationID string, force bool, psuHeaders map[string]string) error {
	integration, err := s.integrationRepo.GetByID(userID, integrationID)
	if err != nil || integration == nil {
		return fmt.Errorf("integration not found")
	}

	if integration.BackoffUntil != nil && integration.BackoffUntil.After(time.Now()) && !force {
		log.Printf("[SYNC] Skipping %s due to backoff until %v", integration.Name, integration.BackoffUntil)
		return nil
	}

	correlationID := uuid.New().String()
	logDir := getLogDir(correlationID)
	_ = os.MkdirAll(logDir, 0755)

	meta := syncMetadata{
		IntegrationID:   integration.ID,
		IntegrationName: integration.Name,
		ServiceType:     integration.ServiceType,
		UserID:          userID,
		Timestamp:       time.Now(),
		Status:          "STARTED",
	}
	if metaBytes, err := json.MarshalIndent(meta, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(logDir, "metadata.json"), metaBytes, 0644)
	}

	_ = s.integrationRepo.CreateSyncRun(correlationID, userID, integration.ID, integration.Name, integration.ServiceType, "STARTED", time.Now().UTC())

	writeMetaUpdate := func(status string, errMsg string) {
		meta.Status = status
		meta.Error = errMsg
		if metaBytes, err := json.MarshalIndent(meta, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(logDir, "metadata.json"), metaBytes, 0644)
		}
	}

	log.Printf("[SYNC][%s] Dispatching sync for %s (%s)...", correlationID, integration.Name, integration.ServiceType)

	provider := s.integrationRegistry.Get(integration.ServiceType)
	if provider == nil {
		errMsg := fmt.Sprintf("unsupported service type: %s", integration.ServiceType)
		writeMetaUpdate("FAILED", errMsg)
		return s.handleSyncError(userID, integration, fmt.Errorf("%s", errMsg))
	}

	ctx := ContextWithCorrelationID(context.Background(), correlationID)
	res := provider.Sync(ctx, integration, force, psuHeaders)

	if res.BackoffUntil != nil {
		log.Printf("[SYNC][%s] Provider %s is rate limited until %v", correlationID, integration.Name, res.BackoffUntil)
	} else {
		log.Printf("[SYNC][%s] Provider %s is NOT rate limited", correlationID, integration.Name)
	}

	if res.Error == nil {
		writeMetaUpdate("COMPLETED", "")
		s.ReconcilePendingDuplicates(userID, integrationID, res.FetchedExternalIDs)
		s.eventBus.Publish(context.Background(), bus.TopicSyncFinished, bus.SyncFinishedPayload{
			UserID:          userID,
			IntegrationID:   integrationID,
			IntegrationName: integration.Name,
			ServiceType:     integration.ServiceType,
			DiscoveredCount: res.DiscoveredCount,
		})

		hasLogFiles := res.DiscoveredCount > 0
		errStr := ""
		if !hasLogFiles {
			errStr = "No new transactions found"
			_ = os.RemoveAll(logDir)
		}
		_ = s.integrationRepo.UpdateSyncRun(correlationID, "COMPLETED", int(res.DiscoveredCount), hasLogFiles, errStr)
	}

	if res.Error != nil {
		writeMetaUpdate("FAILED", res.Error.Error())
		if res.BackoffUntil != nil {
			integration.BackoffUntil = res.BackoffUntil
			_ = s.integrationRepo.Save(userID, integration)
		}

		_ = os.RemoveAll(logDir)
		_ = s.integrationRepo.UpdateSyncRun(correlationID, "FAILED", 0, false, res.Error.Error())

		return s.handleSyncError(userID, integration, res.Error)
	}

	return s.finalizeSync(userID, integration, integration.CachedBalance, res.BackoffUntil)
}


func (s *SyncService) GetMasterKey(userID string, integrationID string) ([]byte, error) {
	s.cacheMu.RLock()
	userCache := s.masterKeyCache[userID]
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

	key, err := s.GetMasterKeyForUser(user, integrationID)
	if err == nil {
		s.CacheMasterKey(userID, integrationID, key)
	}
	return key, err
}

func (s *SyncService) CacheMasterKey(userID string, integrationID string, key []byte) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if _, ok := s.masterKeyCache[userID]; !ok {
		s.masterKeyCache[userID] = make(map[string][]byte)
	}
	s.masterKeyCache[userID][integrationID] = key
}

func (s *SyncService) GetMasterKeyForUser(user *domain.User, integrationID string) ([]byte, error) {
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

func (s *SyncService) finalizeSync(userID string, i *domain.Integration, balance float64, backoffUntil *time.Time) error {
	now := time.Now().UTC()
	i.LastSyncAt = &now
	i.Status = "ACTIVE"
	i.LastError = ""
	i.CachedBalance = balance
	i.BackoffUntil = backoffUntil

	// Record account balance history
	provider := s.integrationRegistry.Get(i.ServiceType)
	if provider != nil {
		accounts, err := provider.GetAccounts(userID, i)
		if err == nil {
			for _, acc := range accounts {
				if acc.Enabled {
					err := s.transactionRepo.SaveAccountBalanceHistory(userID, i.ID, acc.ID, acc.Balance, now)
					if err != nil {
						log.Printf("[SYNC] Failed to save account balance history for account %s: %v", acc.ID, err)
					}
				}
			}
		} else {
			log.Printf("[SYNC] Failed to get accounts for balance history in finalizeSync: %v", err)
		}
	}

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
	userCache := s.masterKeyCache[userID]
	s.cacheMu.RUnlock()

	for _, i := range integrations {
		var mik []byte
		if userCache != nil {
			if cachedMik, ok := userCache[i.ID]; ok {
				mik = cachedMik
			}
		}

		if mik == nil {
			mik, _ = s.GetMasterKey(userID, i.ID)
		}

		if mik == nil {
			continue
		}

		wrapped, _ := s.cryptoService.WrapKey(newIk, mik)
		s.integrationRepo.SaveKeySlot(i.ID, authID, base64.StdEncoding.EncodeToString(wrapped))
	}

	s.cacheMu.Lock()
	delete(s.masterKeyCache, userID)
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

	if _, ok := s.masterKeyCache[userID]; !ok {
		s.masterKeyCache[userID] = make(map[string][]byte)
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

		s.masterKeyCache[userID][i.ID] = mik
		recoveredCount++
	}
	return recoveredCount
}

func (s *SyncService) GetAccountActiveIntegrations(userID string, mikCache map[string][]byte, integrations []domain.Integration) (map[string]string, error) {
	s.cacheMu.RLock()
	cached := s.activeIntegrationsCache[userID]
	if cached != nil && integrations == nil {
		s.cacheMu.RUnlock()
		return cached, nil
	}
	s.cacheMu.RUnlock()

	if integrations == nil {
		integrations, _ = s.integrationRepo.List(userID)
	}

	accountActiveIntegration := make(map[string]string)
	for _, i := range integrations {
		provider := s.integrationRegistry.Get(string(i.ServiceType))
		if provider == nil {
			continue
		}

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

	s.cacheMu.Lock()
	s.activeIntegrationsCache[userID] = accountActiveIntegration
	s.cacheMu.Unlock()

	return accountActiveIntegration, nil
}

func (s *SyncService) MigrateTransactionsBetweenChains() {
	log.Printf("[SYNC] Starting cross-chain transaction migration check...")
	users, err := s.userRepo.ListAll()
	if err != nil {
		return
	}

	for _, user := range users {
		mikCache := make(map[string][]byte)
		integrations, _ := s.integrationRepo.List(user.ID)
		accountActiveIntegration, err := s.GetAccountActiveIntegrations(user.ID, mikCache, integrations)
		if err != nil {
			continue
		}

		txs, _ := s.transactionRepo.List(user.ID)
		for _, tx := range txs {
			activeIntegrationID, ok := accountActiveIntegration[tx.AccountID]
			if ok && tx.IntegrationID != activeIntegrationID {
				_, err := s.DecryptTransaction(user.ID, &tx, mikCache, accountActiveIntegration)
				if err == nil {
					log.Printf("[SYNC] Migrated tx %s", tx.ID)
				}
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
	activeIntegrationID := ""
	if tx.AccountID != "" {
		if activeIntegrations == nil {
			activeIntegrations, _ = s.GetAccountActiveIntegrations(userID, mikCache, nil)
		}
		if activeIntegrations != nil {
			activeIntegrationID = activeIntegrations[tx.AccountID]
		}
	}

	var decrypted []byte
	var decryptedWithKeyID string
	if mk, ok := mikCache[tx.IntegrationID]; ok {
		decrypted, _ = s.decrypt(mk, tx.EncryptedData)
		if decrypted != nil {
			decryptedWithKeyID = tx.IntegrationID
		}
	}

	if decrypted == nil {
		for intID, alternateMIK := range mikCache {
			if intID == tx.IntegrationID {
				continue
			}
			recoveredData, rerr := s.decrypt(alternateMIK, tx.EncryptedData)
			if rerr == nil {
				decrypted = recoveredData
				decryptedWithKeyID = intID
				break
			}
		}
	}

	if decrypted != nil {
		if activeIntegrationID != "" && tx.IntegrationID != activeIntegrationID {
			targetMK, err := s.GetMasterKey(userID, activeIntegrationID)
			if err == nil {
				mikCache[activeIntegrationID] = targetMK
				newEncrypted, eerr := s.cryptoService.Encrypt(targetMK, decrypted)
				if eerr == nil {
					newEncryptedBase64 := base64.StdEncoding.EncodeToString(newEncrypted)
					s.transactionRepo.UpdateIntegrationAndEncryptedData(userID, tx.ID, activeIntegrationID, newEncryptedBase64)
					tx.IntegrationID = activeIntegrationID
					tx.EncryptedData = newEncryptedBase64
				}
			}
		} else if decryptedWithKeyID != "" && decryptedWithKeyID != tx.IntegrationID {
			if primaryMK, ok := mikCache[tx.IntegrationID]; ok {
				newEncrypted, eerr := s.cryptoService.Encrypt(primaryMK, decrypted)
				if eerr == nil {
					newEncryptedBase64 := base64.StdEncoding.EncodeToString(newEncrypted)
					s.transactionRepo.UpdateEncryptedData(userID, tx.ID, newEncryptedBase64)
					tx.EncryptedData = newEncryptedBase64
				}
			}
		}
		return decrypted, nil
	}

	s.transactionRepo.Delete(userID, tx.ID)
	return nil, fmt.Errorf("decryption failed for tx %s", tx.ID)
}

func (s *SyncService) ReapplyAllRules(userID string) {
	s.ApplyRulesToAllTransactions(userID)
	s.eventBus.Publish(context.Background(), bus.TopicRulesChanged, userID)
}

func (s *SyncService) ApplyRulesToAllTransactions(userID string) error {
	txs, err := s.transactionRepo.List(userID)
	if err != nil {
		return err
	}

	mikCache := make(map[string][]byte)
	accountTagsCache := make(map[string]map[string]string)
	accountNamesCache := make(map[string]map[string]string)
	integrations, _ := s.integrationRepo.List(userID)
	for _, i := range integrations {
		mik, err := s.GetMasterKey(userID, i.ID)
		if err == nil {
			mikCache[i.ID] = mik
		}
	}

	activeIntegrations, _ := s.GetAccountActiveIntegrations(userID, mikCache, integrations)

	for _, t := range txs {
		if _, ok := mikCache[t.IntegrationID]; !ok {
			continue
		}

		if _, ok := accountTagsCache[t.IntegrationID]; !ok {
			for _, i := range integrations {
				if i.ID == t.IntegrationID {
					decrypted, err := s.DecryptIntegrationConfig(userID, &i)
					if err == nil {
						var config struct {
							AccountsMetadata map[string]*domain.AccountMeta `json:"accounts_metadata"`
						}
						if err := json.Unmarshal(decrypted, &config); err == nil {
							tagsMap := make(map[string]string)
							namesMap := make(map[string]string)
							for accID, meta := range config.AccountsMetadata {
								if meta != nil {
									tagsMap[accID] = meta.Tags
									namesMap[accID] = meta.Alias
								}
							}
							accountTagsCache[t.IntegrationID] = tagsMap
							accountNamesCache[t.IntegrationID] = namesMap
						}
					}
					break
				}
			}
		}

		data, err := s.DecryptTransaction(userID, &t, mikCache, activeIntegrations)
		if err != nil {
			continue
		}

		provider := s.GetProvider("GOCARDLESS") // Fallback
		for _, i := range integrations {
			if i.ID == t.IntegrationID {
				provider = s.GetProvider(i.ServiceType)
				break
			}
		}
		if provider == nil {
			continue
		}

		meta, err := provider.ParseTransaction(data, t.AccountID)
		if err != nil {
			continue
		}

		accountTags := ""
		if tagsMap, ok := accountTagsCache[t.IntegrationID]; ok {
			accountTags = tagsMap[t.AccountID]
		}

		accountName := ""
		if namesMap, ok := accountNamesCache[t.IntegrationID]; ok {
			accountName = namesMap[t.AccountID]
		}

		poolIDs, _ := s.ruleService.ProcessTransaction(userID, t.ID, t.IntegrationID, meta.Receiver, meta.Description, t.Tags, accountTags, accountName, meta.Amount)
		s.transactionRepo.UpdatePools(userID, t.ID, poolIDs)
	}
	return nil
}

func (s *SyncService) WipeAndReimportBankLogs() {
	wipeReimport := strings.ToLower(strings.Trim(os.Getenv("WIPE_AND_REIMPORT"), "\""))
	if wipeReimport != "true" && wipeReimport != "1" {
		return
	}

	log.Printf("[REIMPORT] Starting one-time data wipe and log-based re-import...")
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://budget:budgetpass@db:5432/budget?sslmode=disable"
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/app/data"
	}
	db.BackupDB(dbURL, dataDir)

	logsDir := os.Getenv("LOGS_DIR")
	if logsDir == "" {
		logsDir = "/app/logs"
	}
	logsDir = filepath.Join(logsDir, "sync_runs")
	if _, err := os.Stat(logsDir); os.IsNotExist(err) {
		log.Printf("[REIMPORT] Logs directory does not exist: %s", logsDir)
		return
	}

	users, _ := s.userRepo.ListAll()
	targetServices := []string{"GOCARDLESS", "ENABLEBANKING"}

	for _, user := range users {
		log.Printf("[REIMPORT] Processing user %s...", user.Username)
		mapping, err := s.transactionRepo.GetCorrelationIDMapping(user.ID)
		if err != nil {
			continue
		}

		s.transactionRepo.WipeTransactionsByServiceType(user.ID, targetServices)

		integrations, _ := s.integrationRepo.List(user.ID)
		integrationMap := make(map[string]domain.Integration)
		mikCache := make(map[string][]byte)
		for _, i := range integrations {
			integrationMap[i.ID] = i
			mk, err := s.GetMasterKey(user.ID, i.ID)
			if err == nil {
				mikCache[i.ID] = mk
			}
		}

		type syncRun struct {
			cid       string
			timestamp time.Time
		}
		var runs []syncRun

		entries, _ := os.ReadDir(logsDir)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			cid := entry.Name()
			if _, ok := mapping[cid]; !ok {
				continue
			}

			logFiles, _ := filepath.Glob(filepath.Join(logsDir, cid, "*_resp.json"))
			if len(logFiles) == 0 {
				continue
			}

			foundTimestamp := false
			for i := 0; i < len(logFiles) && i < 3; i++ {
				content, err := os.ReadFile(logFiles[i])
				if err != nil {
					continue
				}
				var meta struct {
					Timestamp string `json:"timestamp"`
				}
				if err := json.Unmarshal(content, &meta); err == nil && meta.Timestamp != "" {
					if t, err := time.Parse(time.RFC3339, meta.Timestamp); err == nil {
						runs = append(runs, syncRun{cid, t})
						foundTimestamp = true
						break
					}
				}
			}
			if !foundTimestamp {
				log.Printf("[REIMPORT] CID %s: No timestamp found.", cid)
			}
		}

		sort.Slice(runs, func(i, j int) bool {
			return runs[i].timestamp.Before(runs[j].timestamp)
		})

		log.Printf("[REIMPORT] User %s: Found %d sync runs.", user.Username, len(runs))
		totalImported := 0
		now := time.Now()

		for _, run := range runs {
			cidMapping := mapping[run.cid]
			integrationObj, ok := integrationMap[cidMapping.IntegrationID]
			if !ok {
				continue
			}

			provider := s.GetProvider(integrationObj.ServiceType)
			if provider == nil {
				continue
			}

			logFilesPattern := filepath.Join(logsDir, run.cid, "*_resp.json")
			respFiles, _ := filepath.Glob(logFilesPattern)
			for _, logFile := range respFiles {
				reqFile := strings.Replace(logFile, "_resp.json", "_req.json", 1)
				targetAccountID := cidMapping.AccountID
				if reqContent, err := os.ReadFile(reqFile); err == nil {
					var reqData struct {
						URL string `json:"url"`
					}
					if err := json.Unmarshal(reqContent, &reqData); err == nil {
						re := regexp.MustCompile(`/accounts/([a-zA-Z0-9-]+)`)
						match := re.FindStringSubmatch(reqData.URL)
						if len(match) > 1 {
							targetAccountID = match[1]
						}
					}
				}

				content, err := os.ReadFile(logFile)
				if err != nil {
					continue
				}

				var logData struct {
					Body json.RawMessage `json:"body"`
				}
				if err := json.Unmarshal(content, &logData); err != nil {
					continue
				}

				var rawTransactions []json.RawMessage
				if strings.Contains(logFile, "gocardless") {
					var gcBody struct {
						Transactions struct {
							Booked []json.RawMessage `json:"booked"`
						} `json:"transactions"`
					}
					if err := json.Unmarshal(logData.Body, &gcBody); err == nil {
						rawTransactions = gcBody.Transactions.Booked
					}
				} else if strings.Contains(logFile, "enablebanking") {
					var ebBody struct {
						Transactions []json.RawMessage `json:"transactions"`
					}
					if err := json.Unmarshal(logData.Body, &ebBody); err == nil {
						rawTransactions = ebBody.Transactions
					}
				}

				if len(rawTransactions) == 0 {
					continue
				}

				// Try to get account metadata if available
				accountTags := ""
				accountName := ""
				decryptedConfig, err := s.DecryptIntegrationConfig(user.ID, &integrationObj)
				if err == nil {
					var config struct {
						AccountsMetadata map[string]*domain.AccountMeta `json:"accounts_metadata"`
					}
					json.Unmarshal(decryptedConfig, &config)
					if config.AccountsMetadata != nil && config.AccountsMetadata[targetAccountID] != nil {
						accountTags = config.AccountsMetadata[targetAccountID].Tags
						accountName = config.AccountsMetadata[targetAccountID].Alias
					}
				}

				var batch []domain.BankTransaction
				for _, raw := range rawTransactions {
					meta, err := provider.ParseTransaction(raw, targetAccountID)
					if err != nil {
						continue
					}

					txID := uuid.New().String()
					poolIDs, _ := s.ruleService.ProcessTransaction(user.ID, txID, integrationObj.ID, meta.Receiver, meta.Description, "", accountTags, accountName, meta.Amount)
					genericTx := domain.GenericTransaction{
						Amount:         meta.Amount,
						Description:    meta.Description,
						Peer:           meta.Receiver,
						PeerIBAN:       meta.ReceiverIBAN,
						CreatedAt:      meta.CreatedAt,
						ExternalID:     meta.ExternalID,
						InternalStatus: meta.InternalStatus,
					}
					txJSON, _ := json.Marshal(genericTx)
					encryptedData, _ := s.cryptoService.Encrypt(mikCache[integrationObj.ID], txJSON)

					sourceAcc := ""
					destAcc := targetAccountID
					if meta.Amount < 0 {
						sourceAcc = targetAccountID
						destAcc = ""
					}

					batch = append(batch, domain.BankTransaction{
						ID: txID, UserID: user.ID, IntegrationID: integrationObj.ID, AccountID: targetAccountID,
						SourceAccountID: sourceAcc, DestinationAccountID: destAcc,
						PoolIDs: poolIDs, ExternalID: meta.ExternalID, EncryptedData: base64.StdEncoding.EncodeToString(encryptedData),
						CorrelationID: run.cid, InternalStatus: meta.InternalStatus,
						CreatedAt:     meta.CreatedAt, SyncedAt: now,
					})
				}

				if len(batch) > 0 {
					if err := s.transactionRepo.SaveBulk(user.ID, batch); err == nil {
						totalImported += len(batch)
					}
				}
			}
		}
		log.Printf("[REIMPORT] User %s: Finished re-import. Processed %d entries.", user.Username, totalImported)
	}
	log.Printf("[REIMPORT] Global re-import complete.")
}

func (s *SyncService) ReconcilePendingDuplicates(userID string, integrationID string, fetchedExternalIDs map[string]bool) {
	if fetchedExternalIDs == nil {
		fetchedExternalIDs = make(map[string]bool)
	}

	txs, err := s.transactionRepo.ListByIntegration(userID, integrationID)
	if err != nil || len(txs) == 0 {
		return
	}

	mik, err := s.GetMasterKey(userID, integrationID)
	if err != nil {
		return
	}
	mikCache := map[string][]byte{integrationID: mik}

	integrationObj, _ := s.integrationRepo.GetByID(userID, integrationID)
	if integrationObj == nil {
		return
	}
	provider := s.GetProvider(integrationObj.ServiceType)
	if provider == nil {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -14)

	type parsedTx struct {
		tx        domain.BankTransaction
		meta      integration.TransactionMetadata
		isPending bool
	}

	var decryptedTxs []parsedTx

	for _, tx := range txs {
		if tx.IsDeleted {
			continue
		}
		if tx.CreatedAt.Before(cutoff) {
			continue
		}

		data, err := s.DecryptTransaction(userID, &tx, mikCache, nil)
		if err != nil {
			continue
		}

		meta, err := provider.ParseTransaction(data, tx.AccountID)
		if err != nil {
			continue
		}

		isPending := false
		if meta.InternalStatus == "PENDING_REJECTION" {
			isPending = true
		} else {
			var rawMap map[string]interface{}
			if err := json.Unmarshal(data, &rawMap); err == nil {
				if statusVal, ok := rawMap["InternalStatus"].(string); ok && statusVal == "PENDING_REJECTION" {
					isPending = true
				} else if codeObj, ok := rawMap["bank_transaction_code"].(map[string]interface{}); ok {
					if subCode, ok := codeObj["sub_code"].(string); ok && subCode == "UPCT" {
						isPending = true
					}
				}
			}
		}

		decryptedTxs = append(decryptedTxs, parsedTx{
			tx:        tx,
			meta:      meta,
			isPending: isPending,
		})
	}

	normalizeName := func(name string) string {
		name = strings.ToLower(name)
		var sb strings.Builder
		for _, r := range name {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				sb.WriteRune(r)
			}
		}
		return sb.String()
	}

	for _, pTx := range decryptedTxs {
		if !pTx.isPending {
			continue
		}

		for _, other := range decryptedTxs {
			if other.tx.ID == pTx.tx.ID || other.isPending {
				continue
			}
			if other.tx.AccountID != pTx.tx.AccountID {
				continue
			}

			diff := other.meta.CreatedAt.Sub(pTx.meta.CreatedAt)
			if math.Abs(diff.Hours()) > 72.0 {
				continue
			}

			if math.Abs(pTx.meta.Amount-other.meta.Amount) >= 0.01 {
				continue
			}

			n1 := normalizeName(pTx.meta.Receiver)
			n2 := normalizeName(other.meta.Receiver)
			isSimilar := false
			if n1 == n2 || (n1 != "" && n2 != "" && (strings.Contains(n1, n2) || strings.Contains(n2, n1))) {
				isSimilar = true
			} else if strings.Contains(n1, "paypal") && strings.Contains(n2, "paypal") {
				isSimilar = true
			} else if strings.Contains(n1, "amzn") && strings.Contains(n2, "amzn") {
				isSimilar = true
			} else if strings.Contains(n1, "amazon") && strings.Contains(n2, "amazon") {
				isSimilar = true
			} else if strings.Contains(n1, "lidl") && strings.Contains(n2, "lidl") {
				isSimilar = true
			}

			if isSimilar {
				log.Printf("[RECONCILE] Soft deleting duplicate pending transaction %s (ExternalID: %s, Amount: %.2f, Receiver: %s) because matching finalized transaction %s (ExternalID: %s) exists.",
					pTx.tx.ID, pTx.tx.ExternalID, pTx.meta.Amount, pTx.meta.Receiver, other.tx.ID, other.tx.ExternalID)

				// Merge tags
				mergedTags := other.tx.Tags
				if pTx.tx.Tags != "" {
					if mergedTags == "" {
						mergedTags = pTx.tx.Tags
					} else {
						tagsMap := make(map[string]bool)
						for _, t := range strings.Split(other.tx.Tags, ",") {
							trimmed := strings.TrimSpace(t)
							if trimmed != "" {
								tagsMap[trimmed] = true
							}
						}
						for _, t := range strings.Split(pTx.tx.Tags, ",") {
							trimmed := strings.TrimSpace(t)
							if trimmed != "" {
								tagsMap[trimmed] = true
							}
						}
						var merged []string
						for t := range tagsMap {
							merged = append(merged, t)
						}
						mergedTags = strings.Join(merged, ",")
					}
				}

				// Merge accounts
				sourceAcc := other.tx.SourceAccountID
				if pTx.tx.SourceAccountID != "" {
					sourceAcc = pTx.tx.SourceAccountID
				}
				destAcc := other.tx.DestinationAccountID
				if pTx.tx.DestinationAccountID != "" {
					destAcc = pTx.tx.DestinationAccountID
				}

				// Merge transfer links
				var linkedTxID *string
				isLinkConfirmed := other.tx.IsLinkConfirmed
				if pTx.tx.LinkedTransactionID != nil && *pTx.tx.LinkedTransactionID != "" {
					linkedTxID = pTx.tx.LinkedTransactionID
					isLinkConfirmed = pTx.tx.IsLinkConfirmed
				} else if other.tx.LinkedTransactionID != nil && *other.tx.LinkedTransactionID != "" {
					linkedTxID = other.tx.LinkedTransactionID
				}

				// Merge pools
				mergedPoolsMap := make(map[string]bool)
				for _, p := range other.tx.PoolIDs {
					if p != "" {
						mergedPoolsMap[p] = true
					}
				}
				for _, p := range pTx.tx.PoolIDs {
					if p != "" {
						mergedPoolsMap[p] = true
					}
				}
				var finalPools []string
				for p := range mergedPoolsMap {
					finalPools = append(finalPools, p)
				}

				// Apply reconciliation transfer in database
				err := s.transactionRepo.ReconcileMetadataAndPools(userID, pTx.tx.ID, other.tx.ID, mergedTags, sourceAcc, destAcc, linkedTxID, isLinkConfirmed, finalPools)
				if err != nil {
					log.Printf("[RECONCILE] Failed to transfer metadata from pending tx %s to finalized tx %s: %v", pTx.tx.ID, other.tx.ID, err)
				}

				s.transactionRepo.Delete(userID, pTx.tx.ID)
				break
			}
		}
	}
}
