package enablebanking

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"encoding/json"

	"github.com/genazt/my-budget-script/web/backend/internal/bus"
	"github.com/genazt/my-budget-script/web/backend/internal/crypto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/integration"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	"github.com/genazt/my-budget-script/web/backend/internal/service"
	"github.com/genazt/my-budget-script/web/backend/pkg/apis/enablebanking"
	"github.com/google/uuid"
)

type Provider struct {
	integrationRepo   *repository.IntegrationRepository
	transactionRepo   *repository.TransactionRepository
	cryptoService     *crypto.CryptoService
	enableBanking     *service.EnableBankingService
	ruleService       *service.RuleService
	masterKeyProvider integration.MasterKeyProvider
	eventBus          *bus.Bus
}

func NewProvider(
	ir *repository.IntegrationRepository,
	tr *repository.TransactionRepository,
	cs *crypto.CryptoService,
	eb *service.EnableBankingService,
	rs *service.RuleService,
	mkp integration.MasterKeyProvider,
	eventBus *bus.Bus,
) *Provider {
	return &Provider{
		integrationRepo:   ir,
		transactionRepo:   tr,
		cryptoService:     cs,
		enableBanking:     eb,
		ruleService:       rs,
		masterKeyProvider: mkp,
		eventBus:          eventBus,
	}
}

func (p *Provider) ServiceType() string {
	return "ENABLEBANKING"
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
		ApplicationID    string                         `json:"application_id"`
		PrivateKey       string                         `json:"private_key"`
		SessionID        string                         `json:"session_id"`
		AccountIDs       []string                       `json:"account_ids"`
		LegacyAccountIDs []string                       `json:"accounts"`
		AccountsMetadata map[string]*domain.AccountMeta `json:"accounts_metadata"`
	}
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return integration.SyncResult{Error: err}
	}

	if config.SessionID == "" {
		return integration.SyncResult{Error: fmt.Errorf("session_id missing")}
	}

	token, err := p.enableBanking.CreateJWT(config.ApplicationID, config.PrivateKey)
	if err != nil {
		return integration.SyncResult{Error: err}
	}

	newCount := 0
	correctedCount := 0
	now := time.Now()

	if config.AccountsMetadata == nil {
		config.AccountsMetadata = make(map[string]*domain.AccountMeta)
	}

	var allNewTxs []domain.BankTransaction
	var newTxInfos []integration.DecryptedTxInfo

	existingTxs, _ := p.transactionRepo.ListByIntegration(userID, i.ID)
	existingMap := make(map[string]domain.BankTransaction)
	for _, tx := range existingTxs {
		if tx.ExternalID != "" {
			existingMap[tx.ExternalID] = tx
		}
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

	log.Printf("[SYNC][%s] [ENABLEBANKING] Found %d potential accounts to sync for integration '%s'", correlationID, len(idMap), i.Name)

	totalBalance := 0.0

	for accID := range idMap {
		if accID == "" {
			continue
		}

		meta, ok := config.AccountsMetadata[accID]
		if ok && meta != nil && !meta.Enabled {
			log.Printf("[SYNC][%s] [ENABLEBANKING] Skipping disabled account %s", correlationID, accID)
			continue
		}

		if !force && meta != nil && meta.BackoffUntil != nil && now.Before(*meta.BackoffUntil) {
			log.Printf("[SYNC][%s] [ENABLEBANKING] Skipping account %s (on backoff until %v)", correlationID, accID, meta.BackoffUntil)
			continue
		}

		metaJSON, _ := json.Marshal(meta)
		log.Printf("[SYNC][%s] [ENABLEBANKING] Syncing account %s with metadata %s...", correlationID, accID, metaJSON)

		handleRateLimit := func(err error) bool {
			if err == nil {
				return false
			}

			errStr := err.Error()
			if strings.Contains(errStr, "Status 429") || strings.Contains(errStr, "RateLimitException") || strings.Contains(errStr, "ASPSP_RATE_LIMIT_EXCEEDED") {
				backoff := now.Add(8 * time.Hour)
				if meta == nil {
					meta = &domain.AccountMeta{Enabled: true, Alias: accID}
					config.AccountsMetadata[accID] = meta
				}
				meta.BackoffUntil = &backoff
				log.Printf("[SYNC] [ENABLEBANKING] Rate limit exceeded for account %s. Backing off until %v", accID, backoff)
				return true
			}

			return false
		}

		// Fetch Account Details (Metadata)
		if (!force && meta != nil && meta.MetadataCheckedAt != nil && now.Sub(*meta.MetadataCheckedAt) < 24*time.Hour) || (meta != nil && meta.IBAN != "") {
			if meta.MetadataCheckedAt != nil {
				log.Printf("[SYNC][%s] [ENABLEBANKING] Skipping metadata check for account %s (checked %v ago)", correlationID, accID, now.Sub(*meta.MetadataCheckedAt))
			} else {
				log.Printf("[SYNC][%s] [ENABLEBANKING] Skipping metadata check for account %s (IBAN already present)", correlationID, accID)
			}
		} else {
			details, err := p.enableBanking.GetAccountDetails(ctx, token, accID)
			if handleRateLimit(err) {
				continue
			}

			if err == nil && details != nil {
				if meta == nil {
					meta = &domain.AccountMeta{Enabled: true, Alias: accID}
					config.AccountsMetadata[accID] = meta
				}
				if details.Iban != nil {
					meta.IBAN = *details.Iban
				}
				if details.Name != nil && (meta.Alias == "" || meta.Alias == accID) {
					meta.Alias = *details.Name
				}
				meta.MetadataCheckedAt = &now
			}
		}

		// Fetch Balances
		if !force && meta != nil && meta.LastSyncedAt != nil && now.Sub(*meta.LastSyncedAt) < 6*time.Hour {
			log.Printf("[SYNC][%s] [ENABLEBANKING] Skipping balance fetch for account %s (fetched %v ago)", correlationID, accID, now.Sub(*meta.LastSyncedAt))
			totalBalance += meta.Balance
		} else {
			balances, err := p.enableBanking.GetBalances(ctx, token, accID)
			if handleRateLimit(err) {
				continue
			}

			if err == nil && len(balances) > 0 {
				for _, b := range balances {
					if b.BalanceAmount != nil && b.BalanceAmount.Amount != nil {
						amt, _ := strconv.ParseFloat(*b.BalanceAmount.Amount, 64)
						if meta == nil {
							meta = &domain.AccountMeta{Enabled: true, Alias: accID}
							config.AccountsMetadata[accID] = meta
						}
						meta.Balance = amt
						totalBalance += amt
						break
					}
				}
			}
		}

		// Fetch Transactions
		thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
		dateFrom := thirtyDaysAgo.Format("2006-01-02")
		if !force && meta != nil && meta.LastSyncedAt != nil {
			lastSyncDate := meta.LastSyncedAt.AddDate(0, 0, -14)
			if lastSyncDate.After(thirtyDaysAgo) {
				dateFrom = lastSyncDate.Format("2006-01-02")
			}
		}

		strategy := ""
		if force {
			strategy = "longest"
		}

		ebTxs, txResp, err := p.enableBanking.GetTransactions(ctx, token, accID, dateFrom, psuHeaders, strategy)
		if err != nil {
			if strings.Contains(err.Error(), "ASPSP_RATE_LIMIT_EXCEEDED") {
				log.Printf("[SYNC] Rate limit hit on transactions fetch for account %s: %v", accID, err)
				backoff := now.Add(24 * time.Hour)
				backoffUntil = &backoff
				p.recordAccountBackoff(userID, i, masterKey, &config, accID, backoff)
				continue
			}
			log.Printf("[SYNC] Enable Banking transaction fetch failed for account %s: %v", accID, err)
			continue
		}

		_ = txResp // used for rate limit extraction if needed later

		for _, t := range ebTxs {
			// Parse using the new robust logic
			tBytes, _ := json.Marshal(t)
			txMeta, err := p.ParseTransaction(tBytes, accID)
			if err != nil {
				// We skip if parsing fails completely, but ParseTransaction now handles UPCT tagging
				continue
			}

			fetchedExternalIDs[txMeta.ExternalID] = true

			if existingTx, found := existingMap[txMeta.ExternalID]; found {
				if !existingTx.CreatedAt.Equal(txMeta.CreatedAt) {
					p.transactionRepo.UpdateTimestampAndExternalID(userID, existingTx.ID, txMeta.CreatedAt, txMeta.ExternalID)
					correctedCount++
				}
				continue
			}

			accountTags := ""
			accountName := ""
			if meta != nil {
				accountTags = meta.Tags
				accountName = meta.Alias
			}

			poolIDs, _ := p.ruleService.ProcessTransaction(userID, i.ID, txMeta.Receiver, txMeta.Description, "", accountTags, accountName, txMeta.Amount)
			genericTx := domain.GenericTransaction{
				Amount:         txMeta.Amount,
				Description:    txMeta.Description,
				Peer:           txMeta.Receiver,
				PeerIBAN:       txMeta.ReceiverIBAN,
				CreatedAt:      txMeta.CreatedAt,
				ExternalID:     txMeta.ExternalID,
				InternalStatus: txMeta.InternalStatus,
			}
			txJSON, _ := json.Marshal(genericTx)
			encryptedData, _ := p.cryptoService.Encrypt(masterKey, txJSON)

			sourceAcc := ""
			destAcc := accID
			if txMeta.Amount < 0 {
				sourceAcc = accID
				destAcc = ""
			}

			newTx := domain.BankTransaction{
				ID: uuid.New().String(), UserID: userID, IntegrationID: i.ID, AccountID: accID,
				SourceAccountID: sourceAcc, DestinationAccountID: destAcc,
				PoolIDs: poolIDs, ExternalID: txMeta.ExternalID, EncryptedData: base64.StdEncoding.EncodeToString(encryptedData),
				CorrelationID: correlationID, InternalStatus: txMeta.InternalStatus,
				CreatedAt: txMeta.CreatedAt, SyncedAt: now,
			}

			allNewTxs = append(allNewTxs, newTx)
			newTxInfos = append(newTxInfos, integration.DecryptedTxInfo{
				Tx:          newTx,
				Amount:      txMeta.Amount,
				Receiver:    txMeta.Receiver,
				Description: txMeta.Description,
			})
			newCount++
		}

		if meta != nil {
			meta.LastSyncedAt = &now

			// Proactive backoff after success to respect PSD2 "4 calls per day" unattended access limits
			if !force {
				backoff := now.Add(12 * time.Hour)
				meta.BackoffUntil = &backoff
				log.Printf("[SYNC] [ENABLEBANKING] Successful sync for account %s. Setting proactive backoff until %v", accID, backoff)
			}
		}
	}

	log.Printf("[SYNC] [ENABLEBANKING] Sync summary for integration '%s': %d new transactions discovered, %d transactions corrected.", i.Name, newCount, correctedCount)

	updatedJSON, _ := json.Marshal(config)
	newEncrypted, _ := p.cryptoService.Encrypt(masterKey, updatedJSON)
	i.EncryptedConfig = base64.StdEncoding.EncodeToString(newEncrypted)
	p.integrationRepo.Save(userID, i)

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
			Amount:         genericTx.Amount,
			Receiver:       genericTx.Peer,
			ReceiverIBAN:   genericTx.PeerIBAN,
			Description:    genericTx.Description,
			CreatedAt:      genericTx.CreatedAt,
			ExternalID:     extID,
			InternalStatus: genericTx.InternalStatus,
		}, nil
	}

	var et enablebanking.Transaction
	if err := json.Unmarshal(decryptedData, &et); err != nil {
		var raw map[string]interface{}
		if unmarshalErr := json.Unmarshal(decryptedData, &raw); unmarshalErr == nil {
			keys := make([]string, 0, len(raw))
			for k := range raw {
				keys = append(keys, k)
			}
			log.Printf("[SYNC] [ENABLEBANKING] ParseTransaction failed: %v. Available JSON keys: %v", err, keys)
		}

		// Minimal fallback for missing/different fields just in case
		var item struct {
			EntryReference      *string `json:"entry_reference"`
			TransactionId       *string `json:"transaction_id"`
			Status              *string `json:"status"`
			BankTransactionCode *struct {
				SubCode *string `json:"sub_code"`
			} `json:"bank_transaction_code"`
			BookingDate       *string `json:"booking_date"`
			ValueDate         *string `json:"value_date"`
			TransactionAmount *struct {
				Amount   *string `json:"amount"`
				Currency *string `json:"currency"`
			} `json:"transaction_amount"`
			CreditDebitIndicator *string `json:"credit_debit_indicator"`
			Creditor             *struct {
				Name *string `json:"name"`
			} `json:"creditor"`
			Debtor *struct {
				Name *string `json:"name"`
			} `json:"debtor"`
			RemittanceInformation *[]string `json:"remittance_information"`
		}
		if err := json.Unmarshal(decryptedData, &item); err == nil {
			meta := integration.TransactionMetadata{}

			if item.BankTransactionCode != nil && item.BankTransactionCode.SubCode != nil && *item.BankTransactionCode.SubCode == "UPCT" {
				if item.Status == nil || *item.Status != "BOOK" {
					meta.InternalStatus = "PENDING_REJECTION"
				}
			}

			if item.TransactionId != nil && *item.TransactionId != "" {
				meta.ExternalID = accountID + "_" + *item.TransactionId
			} else if item.EntryReference != nil && *item.EntryReference != "" {
				meta.ExternalID = accountID + "_" + *item.EntryReference
			}

			if item.BookingDate != nil && *item.BookingDate != "" {
				if t, err := time.Parse("2006-01-02", *item.BookingDate); err == nil {
					meta.CreatedAt = t
				}
			}
			if meta.CreatedAt.IsZero() && item.ValueDate != nil && *item.ValueDate != "" {
				if t, err := time.Parse("2006-01-02", *item.ValueDate); err == nil {
					meta.CreatedAt = t
				}
			}

			if item.TransactionAmount != nil && item.TransactionAmount.Amount != nil {
				meta.Amount, _ = strconv.ParseFloat(*item.TransactionAmount.Amount, 64)
			}
			if item.CreditDebitIndicator != nil {
				if *item.CreditDebitIndicator == "DBIT" {
					meta.Amount = -math.Abs(meta.Amount)
				} else if *item.CreditDebitIndicator == "CRDT" {
					meta.Amount = math.Abs(meta.Amount)
				}
			}

			if meta.Amount > 0 {
				if item.Debtor != nil && item.Debtor.Name != nil {
					meta.Receiver = *item.Debtor.Name
				} else if item.Creditor != nil && item.Creditor.Name != nil {
					meta.Receiver = *item.Creditor.Name
				}
			} else {
				if item.Creditor != nil && item.Creditor.Name != nil {
					meta.Receiver = *item.Creditor.Name
				} else if item.Debtor != nil && item.Debtor.Name != nil {
					meta.Receiver = *item.Debtor.Name
				}
			}

			if item.RemittanceInformation != nil && len(*item.RemittanceInformation) > 0 {
				meta.Description = strings.Join(*item.RemittanceInformation, " ")
			}

			if meta.ExternalID != "" {
				return meta, nil
			}
		}
		return integration.TransactionMetadata{}, err
	}

	meta := integration.TransactionMetadata{}

	if et.BankTransactionCode != nil && et.BankTransactionCode.SubCode != nil && *et.BankTransactionCode.SubCode == "UPCT" {
		if et.Status == nil || *et.Status != "BOOK" {
			meta.InternalStatus = "PENDING_REJECTION"
		}
	}

	if et.TransactionId != nil && *et.TransactionId != "" {
		meta.ExternalID = accountID + "_" + *et.TransactionId
	} else if et.EntryReference != nil && *et.EntryReference != "" {
		meta.ExternalID = accountID + "_" + *et.EntryReference
	}

	if et.BookingDate != nil {
		meta.CreatedAt = et.BookingDate.Time
	}
	if meta.CreatedAt.IsZero() && et.ValueDate != nil {
		meta.CreatedAt = et.ValueDate.Time
	}

	if et.TransactionAmount != nil && et.TransactionAmount.Amount != nil {
		meta.Amount, _ = strconv.ParseFloat(*et.TransactionAmount.Amount, 64)
	}

	// EB Logic for normalization
	if et.CreditDebitIndicator != nil {
		if *et.CreditDebitIndicator == "DBIT" {
			meta.Amount = -math.Abs(meta.Amount)
		} else if *et.CreditDebitIndicator == "CRDT" {
			meta.Amount = math.Abs(meta.Amount)
		}
	}

	// Pick peer based on direction
	if meta.Amount > 0 {
		if et.Debtor != nil {
			if et.Debtor.Name != nil {
				meta.Receiver = *et.Debtor.Name
			}
			if et.Debtor.Account != nil && et.Debtor.Account.Iban != nil {
				meta.ReceiverIBAN = *et.Debtor.Account.Iban
			}
		}
		// Fallback to Creditor if Debtor is missing or has no name
		if meta.Receiver == "" && et.Creditor != nil {
			if et.Creditor.Name != nil {
				meta.Receiver = *et.Creditor.Name
			}
			if et.Creditor.Account != nil && et.Creditor.Account.Iban != nil {
				meta.ReceiverIBAN = *et.Creditor.Account.Iban
			}
		}
	} else {
		if et.Creditor != nil {
			if et.Creditor.Name != nil {
				meta.Receiver = *et.Creditor.Name
			}
			if et.Creditor.Account != nil && et.Creditor.Account.Iban != nil {
				meta.ReceiverIBAN = *et.Creditor.Account.Iban
			}
		}
		// Fallback to Debtor if Creditor is missing or has no name
		if meta.Receiver == "" && et.Debtor != nil {
			if et.Debtor.Name != nil {
				meta.Receiver = *et.Debtor.Name
			}
			if et.Debtor.Account != nil && et.Debtor.Account.Iban != nil {
				meta.ReceiverIBAN = *et.Debtor.Account.Iban
			}
		}
	}

	if et.RemittanceInformation != nil && len(*et.RemittanceInformation) > 0 {
		meta.Description = strings.Join(*et.RemittanceInformation, " ")
	}

	return meta, nil
}

func (p *Provider) recordAccountBackoff(
	userID string,
	i *domain.Integration,
	masterKey []byte,
	config *struct {
		ApplicationID    string                         `json:"application_id"`
		PrivateKey       string                         `json:"private_key"`
		SessionID        string                         `json:"session_id"`
		AccountIDs       []string                       `json:"account_ids"`
		LegacyAccountIDs []string                       `json:"accounts"`
		AccountsMetadata map[string]*domain.AccountMeta `json:"accounts_metadata"`
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
	masterKey, err := p.masterKeyProvider.GetMasterKey(userID, integrationObj.ID)
	if err != nil {
		return nil, err
	}

	ciphertext, _ := base64.StdEncoding.DecodeString(integrationObj.EncryptedConfig)
	decrypted, err := p.cryptoService.Decrypt(masterKey, ciphertext)
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
		tags := ""
		var backoffUntil *time.Time

		if meta, ok := config.AccountsMetadata[accID]; ok && meta != nil {
			if meta.Alias != "" {
				name = meta.Alias
			}
			balance = meta.Balance
			enabled = meta.Enabled
			iban = meta.IBAN
			tags = meta.Tags
			backoffUntil = meta.BackoffUntil
		}

		accounts = append(accounts, integration.Account{
			ID:           accID,
			Name:         name,
			Balance:      balance,
			Enabled:      enabled,
			IBAN:         iban,
			BackoffUntil: backoffUntil,
			Tags:         tags,
		})
	}

	return accounts, nil
}
