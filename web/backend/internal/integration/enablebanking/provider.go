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

func (p *Provider) Sync(ctx context.Context, i *domain.Integration, force bool) integration.SyncResult {
	correlationID := service.CorrelationIDFromContext(ctx)
	userID := i.UserID

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
		thirtyDaysAgo := time.Now().AddDate(0, 0, -7)
		dateFrom := thirtyDaysAgo.Format("2006-01-02")
		if !force && meta != nil && meta.LastSyncedAt != nil {
			lastSyncDate := meta.LastSyncedAt.AddDate(0, 0, -2)
			if lastSyncDate.After(thirtyDaysAgo) {
				dateFrom = lastSyncDate.Format("2006-01-02")
			}
		}

		ebTxs, err := p.enableBanking.GetTransactions(ctx, token, accID, dateFrom)
		if handleRateLimit(err) {
			continue
		}

		if err != nil {
			log.Printf("[SYNC] Enable Banking transaction fetch failed for account %s: %v", accID, err)
			continue
		}

		for _, t := range ebTxs {
			externalID := ""
			if t.TransactionId != nil && *t.TransactionId != "" {
				externalID = *t.TransactionId
			} else if t.EntryReference != nil && *t.EntryReference != "" {
				externalID = *t.EntryReference
			}

			if externalID == "" {
				continue
			}

			// Normalize amount
			amt := 0.0
			if t.TransactionAmount != nil && t.TransactionAmount.Amount != nil {
				amt, _ = strconv.ParseFloat(*t.TransactionAmount.Amount, 64)
			}

			// Enable Banking usually returns absolute values.
			myIBAN := ""
			if meta != nil {
				myIBAN = strings.ReplaceAll(strings.ToUpper(meta.IBAN), " ", "")
			}

			isSet := false

			// Check CreditDebitIndicator (Most reliable source)
			if t.CreditDebitIndicator != nil {
				if *t.CreditDebitIndicator == "DBIT" {
					amt = -math.Abs(amt)
					isSet = true
				} else if *t.CreditDebitIndicator == "CRDT" {
					amt = math.Abs(amt)
					isSet = true
				}
			}

			// New Simple IBAN Logic:
			// debtor_account.iban == account.iban -> outgoing (negative amount)
			// creditor_account.iban == account.iban -> incoming (positive amount)
			if !isSet && myIBAN != "" {
				if t.Debtor != nil && t.Debtor.Account != nil && t.Debtor.Account.Iban != nil {
					debtorIBAN := strings.ReplaceAll(strings.ToUpper(*t.Debtor.Account.Iban), " ", "")
					if debtorIBAN == myIBAN {
						amt = -math.Abs(amt)
						isSet = true
					}
				}

				if !isSet && t.Creditor != nil && t.Creditor.Account != nil && t.Creditor.Account.Iban != nil {
					creditorIBAN := strings.ReplaceAll(strings.ToUpper(*t.Creditor.Account.Iban), " ", "")
					if creditorIBAN == myIBAN {
						amt = math.Abs(amt)
						isSet = true
					}
				}
			}

			if !isSet {
				// Fallback to existing logic if IBAN comparison is inconclusive
				if t.Creditor != nil && t.Creditor.Name != nil {
					// If Creditor is present and no indicator, it's likely an outgoing payment
					amt = -math.Abs(amt)
				} else if t.Debtor != nil && t.Debtor.Account != nil {
					// Fallback to Debtor presence
					amt = -math.Abs(amt)
				}
			}

			// Ensure the amount string in the raw data is also negative for debits
			if amt < 0 && t.TransactionAmount != nil && t.TransactionAmount.Amount != nil {
				amtStr := fmt.Sprintf("%.2f", amt)
				t.TransactionAmount.Amount = &amtStr
			}

			createdAt := now
			if t.BookingDate != nil {
				createdAt = t.BookingDate.Time
			}

			// Check if exists
			if existingTx, found := existingMap[externalID]; found {
				if !existingTx.CreatedAt.Equal(createdAt) {
					p.transactionRepo.UpdateTimestampAndExternalID(userID, existingTx.ID, createdAt, externalID)
					correctedCount++
				}
				continue
			}

			receiver := ""
			receiverIBAN := ""

			// Pick peer based on direction:
			// If amt > 0 (Income), we are the Creditor, peer is the Debtor.
			// If amt < 0 (Expense), we are the Debtor, peer is the Creditor.
			if amt > 0 {
				if t.Debtor != nil {
					if t.Debtor.Name != nil {
						receiver = *t.Debtor.Name
					}
					if t.Debtor.Account != nil && t.Debtor.Account.Iban != nil {
						receiverIBAN = *t.Debtor.Account.Iban
					}
				}

				// Fallback to Creditor if Debtor is missing or has no name
				if receiver == "" && t.Creditor != nil {
					if t.Creditor.Name != nil {
						receiver = *t.Creditor.Name
					}
					if t.Creditor.Account != nil && t.Creditor.Account.Iban != nil {
						receiverIBAN = *t.Creditor.Account.Iban
					}
				}
			} else {
				if t.Creditor != nil {
					if t.Creditor.Name != nil {
						receiver = *t.Creditor.Name
					}
					if t.Creditor.Account != nil && t.Creditor.Account.Iban != nil {
						receiverIBAN = *t.Creditor.Account.Iban
					}
				}

				// Fallback to Debtor if Creditor is missing or has no name
				if receiver == "" && t.Debtor != nil {
					if t.Debtor.Name != nil {
						receiver = *t.Debtor.Name
					}
					if t.Debtor.Account != nil && t.Debtor.Account.Iban != nil {
						receiverIBAN = *t.Debtor.Account.Iban
					}
				}
			}

			desc := ""
			if t.RemittanceInformation != nil && len(*t.RemittanceInformation) > 0 {
				desc = strings.Join(*t.RemittanceInformation, " ")
			}

			accountTags := ""
			if meta != nil {
				accountTags = meta.Tags
			}

			poolID, _ := p.ruleService.ProcessTransaction(userID, i.ID, receiver, desc, "", accountTags, amt)
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
				ID: uuid.New().String(), UserID: userID, IntegrationID: i.ID, AccountID: accID,
				SourceAccountID: sourceAcc, DestinationAccountID: destAcc,
				PoolID: poolID, ExternalID: externalID, EncryptedData: base64.StdEncoding.EncodeToString(encryptedData),
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
	return integration.SyncResult{DiscoveredCount: newCount}
}

func (p *Provider) ParseTransaction(decryptedData []byte) (integration.TransactionMetadata, error) {
	// Try to unmarshal as GenericTransaction first
	var genericTx domain.GenericTransaction
	if err := json.Unmarshal(decryptedData, &genericTx); err == nil && (genericTx.ExternalID != "" || genericTx.Peer != "") {
		return integration.TransactionMetadata{
			Amount:       genericTx.Amount,
			Receiver:     genericTx.Peer,
			ReceiverIBAN: genericTx.PeerIBAN,
			Description:  genericTx.Description,
			CreatedAt:    genericTx.CreatedAt,
			ExternalID:   genericTx.ExternalID,
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
			TransactionID *string `json:"transaction_id"`
		}
		if err := json.Unmarshal(decryptedData, &item); err == nil {
			if item.TransactionID != nil && *item.TransactionID != "" {
				return integration.TransactionMetadata{ExternalID: *item.TransactionID}, nil
			}
		}
		return integration.TransactionMetadata{}, err
	}

	meta := integration.TransactionMetadata{}
	if et.TransactionId != nil && *et.TransactionId != "" {
		meta.ExternalID = *et.TransactionId
	} else if et.EntryReference != nil && *et.EntryReference != "" {
		meta.ExternalID = *et.EntryReference
	}

	if et.BookingDate != nil {
		meta.CreatedAt = et.BookingDate.Time
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

	// Pick peer based on direction:
	// If amt > 0 (Income), we are the Creditor, peer is the Debtor.
	// If amt < 0 (Expense), we are the Debtor, peer is the Creditor.
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
