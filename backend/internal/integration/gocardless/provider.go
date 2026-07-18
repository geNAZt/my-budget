package gocardless

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"encoding/json"

	"github.com/genazt/my-budget-script/backend/internal/bus"
	"github.com/genazt/my-budget-script/backend/internal/crypto"
	"github.com/genazt/my-budget-script/backend/internal/domain"
	"github.com/genazt/my-budget-script/backend/internal/integration"
	"github.com/genazt/my-budget-script/backend/internal/repository"
	"github.com/genazt/my-budget-script/backend/internal/service"
	"github.com/genazt/my-budget-script/backend/pkg/apis/gocardless"
	"github.com/google/uuid"
)

type Provider struct {
	integrationRepo   *repository.IntegrationRepository
	transactionRepo   *repository.TransactionRepository
	cryptoService     *crypto.CryptoService
	gocardless        *service.GoCardlessService
	ruleService       *service.RuleService
	masterKeyProvider integration.MasterKeyProvider
	eventBus          *bus.Bus
}

func NewProvider(
	ir *repository.IntegrationRepository,
	tr *repository.TransactionRepository,
	cs *crypto.CryptoService,
	gs *service.GoCardlessService,
	rs *service.RuleService,
	mkp integration.MasterKeyProvider,
	eventBus *bus.Bus,
) *Provider {
	return &Provider{
		integrationRepo:   ir,
		transactionRepo:   tr,
		cryptoService:     cs,
		gocardless:        gs,
		ruleService:       rs,
		masterKeyProvider: mkp,
		eventBus:          eventBus,
	}
}

func (p *Provider) ServiceType() string {
	return "GOCARDLESS"
}

func (p *Provider) Sync(ctx context.Context, i *domain.Integration, force bool, psuHeaders map[string]string) integration.SyncResult {
	correlationID := service.CorrelationIDFromContext(ctx)
	userID := i.UserID

	fetchedExternalIDs := make(map[string]bool)
	var backoffUntil *time.Time

	masterKey, err := p.masterKeyProvider.GetMasterKey(userID, i.ID)
	if err != nil {
		return integration.SyncResult{Error: err}
	}

	ciphertext, _ := base64.StdEncoding.DecodeString(i.EncryptedConfig)
	configBytes, err := p.cryptoService.Decrypt(masterKey, ciphertext)
	if err != nil {
		return integration.SyncResult{Error: err}
	}

	var config struct {
		SecretID           string                         `json:"secret_id"`
		SecretKey          string                         `json:"secret_key"`
		RequisitionID      string                         `json:"requisition_id"`
		AccountIDs         []string                       `json:"account_ids"`
		LegacyAccountIDs   []string                       `json:"accounts"`
		ExcludedAccountIDs []string                       `json:"excluded_account_ids"`
		AccountsMetadata   map[string]*domain.AccountMeta `json:"accounts_metadata"`
	}
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return integration.SyncResult{Error: err}
	}

	existingTxs, _ := p.transactionRepo.ListByIntegration(userID, i.ID)
	existingMap := make(map[string]domain.BankTransaction)
	for _, tx := range existingTxs {
		if tx.ExternalID != "" {
			existingMap[tx.ExternalID] = tx
		}
	}

	token, tokenResp, err := p.gocardless.GetAccessToken(ctx, config.SecretID, config.SecretKey)
	if err != nil {
		return integration.SyncResult{Error: err}
	}

	if bu := p.gocardless.ExtractRateLimit(tokenResp); bu != nil {
		backoffUntil = bu
	}

	if len(config.AccountIDs) == 0 || i.Status == "AWAITING_AUTH" {
		reqResult, err := p.gocardless.GetRequisition(ctx, config.RequisitionID, token)
		if err != nil {
			return integration.SyncResult{Error: err}
		}

		if reqResult == nil || reqResult.Accounts == nil || len(*reqResult.Accounts) == 0 {
			return integration.SyncResult{Error: fmt.Errorf("no accounts linked")}
		}

		config.AccountIDs = []string{}
		for _, a := range *reqResult.Accounts {
			config.AccountIDs = append(config.AccountIDs, a.String())
		}

		updatedJSON, _ := json.Marshal(config)
		newEncrypted, _ := p.cryptoService.Encrypt(masterKey, updatedJSON)
		i.EncryptedConfig = base64.StdEncoding.EncodeToString(newEncrypted)
		p.integrationRepo.Save(userID, i)
	}

	// Gather all unique account IDs from all sources for syncing
	idMap := make(map[string]bool)
	for _, id := range config.AccountIDs {
		idMap[id] = true
	}
	for _, id := range config.LegacyAccountIDs {
		idMap[id] = true
	}
	for id := range config.AccountsMetadata {
		idMap[id] = true
	}

	log.Printf("[SYNC][%s] [GOCARDLESS] Found %d potential accounts to sync for integration '%s'", correlationID, len(idMap), i.Name)

	var allNewTxs []domain.BankTransaction
	var newTxInfos []integration.DecryptedTxInfo
	newCount := 0
	correctedCount := 0
	var totalBalance float64
	configUpdated := false
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)

	for accID := range idMap {
		if accID == "" {
			continue
		}

		// 1. Legacy Exclusion Check
		isExcluded := false
		for _, ex := range config.ExcludedAccountIDs {
			if ex == accID {
				isExcluded = true
				break
			}
		}
		if isExcluded {
			log.Printf("[SYNC][%s] [GOCARDLESS] Skipping excluded account %s", correlationID, accID)
			continue
		}

		// 2. New Metadata Check (Strict Enable/Disable & Account Backoff)
		isEnabled := true
		isRateLimited := false
		var meta *domain.AccountMeta
		if config.AccountsMetadata != nil {
			if m, ok := config.AccountsMetadata[accID]; ok && m != nil {
				meta = m
				if !meta.Enabled {
					log.Printf("[SYNC][%s] [GOCARDLESS] Skipping disabled account %s (%s)", correlationID, meta.Alias, accID)
					isEnabled = false
				} else if !force && meta.BackoffUntil != nil && time.Now().Before(*meta.BackoffUntil) {
					log.Printf("[SYNC][%s] [GOCARDLESS] Skipping rate-limited account %s (%s) until %v", correlationID, meta.Alias, accID, meta.BackoffUntil)
					isRateLimited = true
				}
			}
		}

		if !isEnabled {
			continue
		}

		if isRateLimited {
			// Accumulate last known balance from metadata
			if config.AccountsMetadata != nil {
				if meta, ok := config.AccountsMetadata[accID]; ok && meta != nil {
					totalBalance += meta.Balance
				}
			}
			continue
		}

		log.Printf("[SYNC][%s] [GOCARDLESS] Syncing account %s...", correlationID, accID)

		// Fetch Balance
		balanceFetched := false
		var fetchedBalance float64

		balResult, err := p.gocardless.GetBalances(ctx, accID, token)
		if err != nil {
			if re, ok := err.(*service.RateLimitError); ok {
				log.Printf("[SYNC] Rate limit hit on balance fetch for account %s: %v", accID, err)
				p.recordAccountBackoff(userID, i, masterKey, &config, accID, re.RetryAfter)
				// Accumulate last known balance from metadata
				if config.AccountsMetadata != nil {
					if m, ok := config.AccountsMetadata[accID]; ok && m != nil {
						totalBalance += m.Balance
					}
				}
				continue
			}
			log.Printf("[SYNC] Failed to fetch balances for account %s: %v", accID, err)
		} else if balResult != nil && balResult.Balances != nil {
			for _, b := range *balResult.Balances {
				if b.BalanceType == "closingBooked" || b.BalanceType == "expected" || b.BalanceType == "interimAvailable" {
					val, _ := strconv.ParseFloat(b.BalanceAmount.Amount, 64)
					fetchedBalance = val
					balanceFetched = true
					break
				}
			}
		}

		if balanceFetched {
			totalBalance += fetchedBalance

			// Store the successfully fetched balance in metadata
			if config.AccountsMetadata == nil {
				config.AccountsMetadata = make(map[string]*domain.AccountMeta)
			}
			if m, ok := config.AccountsMetadata[accID]; ok && m != nil {
				m.Balance = fetchedBalance
			} else {
				config.AccountsMetadata[accID] = &domain.AccountMeta{
					Alias:   "Account " + accID,
					Enabled: true,
					Balance: fetchedBalance,
				}
			}
			configUpdated = true
		} else {
			// If fetching failed (but not rate-limited), fallback to last known balance
			if config.AccountsMetadata != nil {
				if m, ok := config.AccountsMetadata[accID]; ok && m != nil {
					totalBalance += m.Balance
				}
			}
		}

		// Fetch Transactions
		dateFrom := thirtyDaysAgo.Format("2006-01-02")
		if !force && meta != nil && meta.LastSyncedAt != nil {
			lastSyncDate := meta.LastSyncedAt.AddDate(0, 0, -14)
			if lastSyncDate.After(thirtyDaysAgo) {
				dateFrom = lastSyncDate.Format("2006-01-02")
			}
		}

		result, txResp, err := p.gocardless.GetTransactions(ctx, accID, token, dateFrom)
		if err != nil {
			if re, ok := err.(*service.RateLimitError); ok {
				log.Printf("[SYNC] Rate limit hit on transactions fetch for account %s: %v", accID, err)
				p.recordAccountBackoff(userID, i, masterKey, &config, accID, re.RetryAfter)
				if backoffUntil == nil || re.RetryAfter.After(*backoffUntil) {
					backoffUntil = &re.RetryAfter
				}
				continue
			}
			log.Printf("[SYNC] Failed to fetch transactions for account %s: %v", accID, err)
			continue
		}

		if bu := p.gocardless.ExtractRateLimit(txResp); bu != nil {
			backoffUntil = bu
		}

		// Set last successful sync time
		if config.AccountsMetadata == nil {
			config.AccountsMetadata = make(map[string]*domain.AccountMeta)
		}
		nowSync := time.Now().UTC()
		if meta, ok := config.AccountsMetadata[accID]; ok && meta != nil {
			meta.LastSyncedAt = &nowSync
		} else {
			config.AccountsMetadata[accID] = &domain.AccountMeta{
				Alias:        "Account " + accID,
				Enabled:      true,
				LastSyncedAt: &nowSync,
			}
		}
		configUpdated = true

		if result != nil {
			now := time.Now()
			for _, t := range result.Transactions.Booked {
				externalID := ""
				if t.TransactionId != nil && *t.TransactionId != "" {
					externalID = accID + "_" + *t.TransactionId
				}

				if externalID == "" {
					continue
				}

				fetchedExternalIDs[externalID] = true

				createdAt := now
				if t.BookingDateTime != nil {
					if parsed, err := time.Parse(time.RFC3339, *t.BookingDateTime); err == nil {
						createdAt = parsed
					}
				} else if t.ValueDateTime != nil {
					if parsed, err := time.Parse(time.RFC3339, *t.ValueDateTime); err == nil {
						createdAt = parsed
					}
				} else if t.BookingDate != nil {
					if parsed, err := time.Parse("2006-01-02", *t.BookingDate); err == nil {
						createdAt = parsed.Add(12 * time.Hour)
					}
				} else if t.ValueDate != nil {
					if parsed, err := time.Parse("2006-01-02", *t.ValueDate); err == nil {
						createdAt = parsed.Add(12 * time.Hour)
					}
				}

				amt := 0.0
				if t.TransactionAmount.Amount != "" {
					if val, err := strconv.ParseFloat(t.TransactionAmount.Amount, 64); err == nil {
						amt = val
					}
				}

				receiver := ""
				receiverIBAN := ""

				// Pick peer based on direction:
				// If amt > 0 (Income), we are the Creditor, peer is the Debtor.
				// If amt < 0 (Expense), we are the Debtor, peer is the Creditor.
				if amt > 0 {
					if t.DebtorName != nil {
						receiver = *t.DebtorName
						if t.DebtorAccount != nil && t.DebtorAccount.Iban != nil {
							receiverIBAN = *t.DebtorAccount.Iban
						}
					}

					// Fallback to Creditor if Debtor is missing or has no name
					if receiver == "" && t.CreditorName != nil {
						receiver = *t.CreditorName
						if t.CreditorAccount != nil && t.CreditorAccount.Iban != nil {
							receiverIBAN = *t.CreditorAccount.Iban
						}
					}
				} else {
					if t.CreditorName != nil {
						receiver = *t.CreditorName
						if t.CreditorAccount != nil && t.CreditorAccount.Iban != nil {
							receiverIBAN = *t.CreditorAccount.Iban
						}
					}

					// Fallback to Debtor if Creditor is missing or has no name
					if receiver == "" && t.DebtorName != nil {
						receiver = *t.DebtorName
						if t.DebtorAccount != nil && t.DebtorAccount.Iban != nil {
							receiverIBAN = *t.DebtorAccount.Iban
						}
					}
				}
				desc := ""
				if t.RemittanceInformationUnstructured != nil {
					desc = *t.RemittanceInformationUnstructured
				}

				// Check by externalID first
				if externalID != "" {
					if existingTx, found := existingMap[externalID]; found {
						if !existingTx.CreatedAt.Equal(createdAt) {
							if err := p.transactionRepo.UpdateTimestampAndExternalID(userID, existingTx.ID, createdAt, externalID); err == nil {
								correctedCount++
								// Update local map to prevent multiple correction counts
								existingTx.CreatedAt = createdAt
								existingMap[externalID] = existingTx
							}
						}
						continue
					}
				}

				accountTags := ""
				accountName := ""
				if meta, ok := config.AccountsMetadata[accID]; ok && meta != nil {
					accountTags = meta.Tags
					accountName = meta.Alias
				}

				txID := uuid.New().String()
				poolIDs, _ := p.ruleService.ProcessTransaction(userID, txID, i.ID, receiver, desc, "", accountTags, accountName, amt)
				genericTx := domain.GenericTransaction{
					Amount:      amt,
					Description: desc,
					Peer:        receiver,
					PeerIBAN:    receiverIBAN,
					CreatedAt:   createdAt,
					ExternalID:  externalID,
				}
				txJSON, _ := json.Marshal(genericTx)
				encryptedData, _ := p.cryptoService.Encrypt(masterKey, txJSON)

				sourceAcc := ""
				destAcc := accID
				if amt < 0 {
					sourceAcc = accID
					destAcc = ""
				}

				newTx := domain.BankTransaction{
					ID: txID, UserID: userID, IntegrationID: i.ID, AccountID: accID,
					SourceAccountID: sourceAcc, DestinationAccountID: destAcc,
					PoolIDs: poolIDs, ExternalID: externalID, EncryptedData: base64.StdEncoding.EncodeToString(encryptedData),
					CorrelationID: correlationID,
					CreatedAt:     createdAt, SyncedAt: now,
				}
				allNewTxs = append(allNewTxs, newTx)
				newTxInfos = append(newTxInfos, integration.DecryptedTxInfo{
					Tx:          newTx,
					Amount:      amt,
					Receiver:    receiver,
					Description: desc,
				})
				newCount++
			}
		}
	}

	log.Printf("[SYNC] [GOCARDLESS] Sync summary for integration '%s': %d new transactions discovered, %d transactions corrected.", i.Name, newCount, correctedCount)

	if configUpdated {
		updatedJSON, _ := json.Marshal(config)
		newEncrypted, _ := p.cryptoService.Encrypt(masterKey, updatedJSON)
		i.EncryptedConfig = base64.StdEncoding.EncodeToString(newEncrypted)
		p.integrationRepo.Save(userID, i)
	}

	if len(allNewTxs) > 0 {
		p.transactionRepo.SaveBulk(userID, allNewTxs)
		for _, info := range newTxInfos {
			p.eventBus.Publish(ctx, bus.TopicTransactionDiscovered, bus.TransactionDiscoveredPayload{
				UserID:      userID,
				Tx:          info.Tx,
				Amount:      info.Amount,
				Receiver:    info.Receiver,
				Description: info.Description,
			})
		}
	}

	i.CachedBalance = totalBalance
	return integration.SyncResult{
		DiscoveredCount:    newCount,
		BackoffUntil:       backoffUntil,
		FetchedExternalIDs: fetchedExternalIDs,
	}
}

func (p *Provider) ParseTransaction(decryptedData []byte, accountID string) (integration.TransactionMetadata, error) {
	// Try to unmarshal as GenericTransaction first
	var genericTx domain.GenericTransaction
	if err := json.Unmarshal(decryptedData, &genericTx); err == nil && (genericTx.ExternalID != "" || genericTx.Peer != "") {
		extID := genericTx.ExternalID
		if extID != "" && accountID != "" && !strings.HasPrefix(extID, accountID+"_") {
			extID = accountID + "_" + extID
		}

		return integration.TransactionMetadata{
			Amount:       genericTx.Amount,
			Receiver:     genericTx.Peer,
			ReceiverIBAN: genericTx.PeerIBAN,
			Description:  genericTx.Description,
			CreatedAt:    genericTx.CreatedAt,
			ExternalID:   extID,
		}, nil
	}

	var gt gocardless.TransactionSchema
	if err := json.Unmarshal(decryptedData, &gt); err != nil {
		var raw map[string]interface{}
		if unmarshalErr := json.Unmarshal(decryptedData, &raw); unmarshalErr == nil {
			keys := make([]string, 0, len(raw))
			for k := range raw {
				keys = append(keys, k)
			}
			log.Printf("[SYNC] [GOCARDLESS] ParseTransaction failed: %v. Available JSON keys: %v", err, keys)
		}

		// Try minimal unmarshal for metadata
		var item struct {
			EntryReference        *string `json:"entryReference"`
			TransactionId         *string `json:"transaction_id"`
			InternalTransactionId *string `json:"internal_transaction_id"`
			BookingDateTime       *string `json:"booking_date_time"`
			ValueDateTime         *string `json:"value_date_time"`
			BookingDate           *string `json:"booking_date"`
			ValueDate             *string `json:"value_date"`
		}
		if err := json.Unmarshal(decryptedData, &item); err == nil {
			meta := integration.TransactionMetadata{}
			if item.TransactionId != nil && *item.TransactionId != "" {
				meta.ExternalID = accountID + "_" + *item.TransactionId
			}

			if item.BookingDateTime != nil && *item.BookingDateTime != "" {
				if parsed, err := time.Parse(time.RFC3339, *item.BookingDateTime); err == nil {
					meta.CreatedAt = parsed
				}
			} else if item.ValueDateTime != nil && *item.ValueDateTime != "" {
				if parsed, err := time.Parse(time.RFC3339, *item.ValueDateTime); err == nil {
					meta.CreatedAt = parsed
				}
			} else if item.BookingDate != nil && *item.BookingDate != "" {
				if parsed, err := time.Parse("2006-01-02", *item.BookingDate); err == nil {
					meta.CreatedAt = parsed.Add(12 * time.Hour)
				}
			} else if item.ValueDate != nil && *item.ValueDate != "" {
				if parsed, err := time.Parse("2006-01-02", *item.ValueDate); err == nil {
					meta.CreatedAt = parsed.Add(12 * time.Hour)
				}
			}
			return meta, nil
		}
		return integration.TransactionMetadata{}, err
	}

	meta := integration.TransactionMetadata{}
	if gt.TransactionAmount.Amount != "" {
		meta.Amount, _ = strconv.ParseFloat(gt.TransactionAmount.Amount, 64)
	}

	// Pick peer based on direction:
	// If amt > 0 (Income), we are the Creditor, peer is the Debtor.
	// If amt < 0 (Expense), we are the Debtor, peer is the Creditor.
	if meta.Amount > 0 {
		if gt.DebtorName != nil {
			meta.Receiver = *gt.DebtorName
			if gt.DebtorAccount != nil && gt.DebtorAccount.Iban != nil {
				meta.ReceiverIBAN = *gt.DebtorAccount.Iban
			}
		}

		// Fallback to Creditor if Debtor is missing or has no name
		if meta.Receiver == "" && gt.CreditorName != nil {
			meta.Receiver = *gt.CreditorName
			if gt.CreditorAccount != nil && gt.CreditorAccount.Iban != nil {
				meta.ReceiverIBAN = *gt.CreditorAccount.Iban
			}
		}
	} else {
		if gt.CreditorName != nil {
			meta.Receiver = *gt.CreditorName
			if gt.CreditorAccount != nil && gt.CreditorAccount.Iban != nil {
				meta.ReceiverIBAN = *gt.CreditorAccount.Iban
			}
		}

		// Fallback to Debtor if Creditor is missing or has no name
		if meta.Receiver == "" && gt.DebtorName != nil {
			meta.Receiver = *gt.DebtorName
			if gt.DebtorAccount != nil && gt.DebtorAccount.Iban != nil {
				meta.ReceiverIBAN = *gt.DebtorAccount.Iban
			}
		}
	}

	if gt.RemittanceInformationUnstructured != nil {
		meta.Description = *gt.RemittanceInformationUnstructured
	}

	if gt.TransactionId != nil && *gt.TransactionId != "" {
		meta.ExternalID = accountID + "_" + *gt.TransactionId
	}

	if gt.BookingDateTime != nil {
		if parsed, err := time.Parse(time.RFC3339, *gt.BookingDateTime); err == nil {
			meta.CreatedAt = parsed
		}
	} else if gt.ValueDateTime != nil {
		if parsed, err := time.Parse(time.RFC3339, *gt.ValueDateTime); err == nil {
			meta.CreatedAt = parsed
		}
	} else if gt.BookingDate != nil {
		if parsed, err := time.Parse("2006-01-02", *gt.BookingDate); err == nil {
			meta.CreatedAt = parsed.Add(12 * time.Hour)
		}
	} else if gt.ValueDate != nil {
		if parsed, err := time.Parse("2006-01-02", *gt.ValueDate); err == nil {
			meta.CreatedAt = parsed.Add(12 * time.Hour)
		}
	}

	return meta, nil
}

func (p *Provider) recordAccountBackoff(
	userID string,
	i *domain.Integration,
	masterKey []byte,
	config *struct {
		SecretID           string                         `json:"secret_id"`
		SecretKey          string                         `json:"secret_key"`
		RequisitionID      string                         `json:"requisition_id"`
		AccountIDs         []string                       `json:"account_ids"`
		LegacyAccountIDs   []string                       `json:"accounts"`
		ExcludedAccountIDs []string                       `json:"excluded_account_ids"`
		AccountsMetadata   map[string]*domain.AccountMeta `json:"accounts_metadata"`
	},
	accID string,
	retryAfter time.Time,
) {
	if config.AccountsMetadata == nil {
		config.AccountsMetadata = make(map[string]*domain.AccountMeta)
	}

	meta, ok := config.AccountsMetadata[accID]
	if !ok || meta == nil {
		config.AccountsMetadata[accID] = &domain.AccountMeta{
			Alias:        "Account " + accID,
			Enabled:      true,
			BackoffUntil: &retryAfter,
		}
	} else {
		meta.BackoffUntil = &retryAfter
	}

	updatedJSON, _ := json.Marshal(config)
	newEncrypted, _ := p.cryptoService.Encrypt(masterKey, updatedJSON)
	i.EncryptedConfig = base64.StdEncoding.EncodeToString(newEncrypted)

	p.integrationRepo.Save(userID, i)
}

func (p *Provider) GetAccounts(userID string, integrationObj *domain.Integration) ([]integration.Account, error) {
	decrypted, err := p.masterKeyProvider.(*service.SyncService).DecryptIntegrationConfig(userID, integrationObj)
	if err != nil {
		return nil, err
	}

	var config struct {
		AccountIDs       []string                       `json:"account_ids"`
		LegacyAccountIDs []string                       `json:"accounts"`
		AccountsMetadata map[string]*domain.AccountMeta `json:"accounts_metadata"`
	}
	if err := json.Unmarshal(decrypted, &config); err != nil {
		return nil, err
	}

	idMap := make(map[string]bool)
	for _, id := range config.AccountIDs {
		idMap[id] = true
	}
	for _, id := range config.LegacyAccountIDs {
		idMap[id] = true
	}
	for id := range config.AccountsMetadata {
		idMap[id] = true
	}

	var accounts []integration.Account
	for accID := range idMap {
		if accID == "" {
			continue
		}

		name := accID
		balance := 0.0
		enabled := true
		iban := ""
		var backoffUntil *time.Time

		if meta, ok := config.AccountsMetadata[accID]; ok && meta != nil {
			if meta.Alias != "" {
				name = meta.Alias
			}
			balance = meta.Balance
			enabled = meta.Enabled
			iban = meta.IBAN
			backoffUntil = meta.BackoffUntil
		}

		accounts = append(accounts, integration.Account{
			ID:           accID,
			Name:         name,
			Balance:      balance,
			Enabled:      enabled,
			IBAN:         iban,
			BackoffUntil: backoffUntil,
		})
	}

	return accounts, nil
}
