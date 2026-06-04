package trading212

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
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
	"github.com/genazt/my-budget-script/web/backend/pkg/apis/trading212"
	"github.com/google/uuid"
)

type Provider struct {
	integrationRepo   *repository.IntegrationRepository
	transactionRepo   *repository.TransactionRepository
	assetRepo         *repository.AssetRepository
	cryptoService     *crypto.CryptoService
	t212              *service.Trading212Service
	ruleService       *service.RuleService
	masterKeyProvider integration.MasterKeyProvider
	eventBus          *bus.Bus
	db                *sql.DB
}

func NewProvider(
	ir *repository.IntegrationRepository,
	tr *repository.TransactionRepository,
	ar *repository.AssetRepository,
	cs *crypto.CryptoService,
	t212 *service.Trading212Service,
	rs *service.RuleService,
	mkp integration.MasterKeyProvider,
	eventBus *bus.Bus,
	db *sql.DB,
) *Provider {
	return &Provider{
		integrationRepo:   ir,
		transactionRepo:   tr,
		assetRepo:         ar,
		cryptoService:     cs,
		t212:              t212,
		ruleService:       rs,
		masterKeyProvider: mkp,
		eventBus:          eventBus,
		db:                db,
	}
}

func (p *Provider) ServiceType() string {
	return "TRADING212"
}

func (p *Provider) Sync(ctx context.Context, i *domain.Integration, force bool) integration.SyncResult {
	correlationID := service.CorrelationIDFromContext(ctx)
	userID := i.UserID
	now := time.Now()

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
		ApiKey             string                         `json:"api_key"`
		ApiSecret          string                         `json:"api_secret"`
		LinkedAssetID      string                         `json:"linked_asset_id"`
		ExcludedAccountIDs []string                       `json:"excluded_account_ids"`
		AccountsMetadata   map[string]*domain.AccountMeta `json:"accounts_metadata"`
	}
	json.Unmarshal(configBytes, &config)

	isCashExcluded := false
	isPortfolioExcluded := false
	isCashBackedOff := false
	isPortfolioBackedOff := false

	// 1. Legacy Exclusion Check
	for _, ex := range config.ExcludedAccountIDs {
		if ex == "T212_CASH" {
			isCashExcluded = true
		}
		if ex == "T212_PORTFOLIO" {
			isPortfolioExcluded = true
		}
	}

	// 2. New Metadata Check
	if config.AccountsMetadata != nil {
		if m, ok := config.AccountsMetadata["T212_CASH"]; ok && m != nil {
			if !m.Enabled {
				isCashExcluded = true
			}
			if !force && m.BackoffUntil != nil && time.Now().Before(*m.BackoffUntil) {
				log.Printf("[SYNC] Skipping rate-limited Trading 212 Cash account until %v", m.BackoffUntil)
				isCashBackedOff = true
			}
		}
		if m, ok := config.AccountsMetadata["T212_PORTFOLIO"]; ok && m != nil {
			if !m.Enabled {
				isPortfolioExcluded = true
			}
			if !force && m.BackoffUntil != nil && time.Now().Before(*m.BackoffUntil) {
				log.Printf("[SYNC] Skipping rate-limited Trading 212 Portfolio account until %v", m.BackoffUntil)
				isPortfolioBackedOff = true
			}
		}
	}

	var totalValue float64
	var cashBalance float64
	var portfolioBalance float64

	if (!isCashExcluded && !isCashBackedOff) || (!isPortfolioExcluded && !isPortfolioBackedOff) {
		summary, err := p.t212.GetAccountSummary(ctx, config.ApiKey, config.ApiSecret)
		if err != nil {
			return integration.SyncResult{Error: err}
		}

		if summary.TotalValue != nil {
			totalValue = float64(*summary.TotalValue)
		}

		if summary.Cash != nil {
			if summary.Cash.AvailableToTrade != nil {
				cashBalance += float64(*summary.Cash.AvailableToTrade)
			}
			if summary.Cash.InPies != nil {
				cashBalance += float64(*summary.Cash.InPies)
			}
			if summary.Cash.ReservedForOrders != nil {
				cashBalance += float64(*summary.Cash.ReservedForOrders)
			}
		}

		portfolioBalance = totalValue - cashBalance
	} else {
		// If all accounts are rate-limited or disabled, preserve the last known integration balance
		totalValue = i.CachedBalance
		if m, ok := config.AccountsMetadata["T212_PORTFOLIO"]; ok && m != nil {
			portfolioBalance = m.Balance
		}
		if m, ok := config.AccountsMetadata["T212_CASH"]; ok && m != nil {
			cashBalance = m.Balance
		}
	}

	// Fetch Transactions (Equity History) with pagination
	var allNewTxs []domain.BankTransaction
	var newTxInfos []integration.DecryptedTxInfo
	newCount := 0
	correctedCount := 0
	existingTxs, _ := p.transactionRepo.ListByIntegration(userID, i.ID)
	existingMap := make(map[string]domain.BankTransaction)
	for _, tx := range existingTxs {
		if tx.ExternalID != "" {
			existingMap[tx.ExternalID] = tx
		}
	}

	if !isPortfolioExcluded && !isPortfolioBackedOff {
		cursor := ""
		thirtyDaysAgo := now.AddDate(0, 0, -30)
		reachedOldTransactions := false

		for {
			t212Resp, err := p.t212.GetTransactions(ctx, config.ApiKey, config.ApiSecret, 50, cursor)
			if err != nil {
				log.Printf("[SYNC] Trading 212 transaction fetch failed: %v", err)
				return integration.SyncResult{Error: err}
			}

			if t212Resp == nil || t212Resp.JSON200 == nil || t212Resp.JSON200.Items == nil {
				break
			}

			items := *t212Resp.JSON200.Items
			if len(items) == 0 {
				break
			}

			log.Printf("[SYNC] Trading 212 processing %d history items (cursor: %s)", len(items), cursor)
			for _, t := range items {
				createdAt := now
				if t.DateTime != nil {
					createdAt = *t.DateTime
				}

				if createdAt.Before(thirtyDaysAgo) {
					reachedOldTransactions = true
					break
				}

				// Generate stable ExternalID
				dt := createdAt.Format(time.RFC3339)
				ref := ""
				if t.Reference != nil {
					ref = *t.Reference
				}
				amt := 0.0
				if t.Amount != nil {
					amt = float64(*t.Amount)
				}
				cur := ""
				if t.Currency != nil {
					cur = *t.Currency
				}
				typ := ""
				if t.Type != nil {
					typ = string(*t.Type)
				}

				uniquePayload := fmt.Sprintf("%s-%s-%s-%.4f-%s", typ, dt, ref, amt, cur)
				h := sha256.New()
				h.Write([]byte(uniquePayload))
				externalID := hex.EncodeToString(h.Sum(nil))

				// If already stored, check if the created_at differs and correct it
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

				// Normalize Amount
				if typ == "WITHDRAW" || typ == "WITHDRAWAL" {
					amt = -math.Abs(amt)
				}

				receiver := ref
				if receiver == "" {
					receiver = typ
				}
				desc := typ

				accountTags := ""
				if meta, ok := config.AccountsMetadata["T212_PORTFOLIO"]; ok && meta != nil {
					accountTags = meta.Tags
				}

				poolIDs, _ := p.ruleService.ProcessTransaction(userID, i.ID, receiver, desc, "", accountTags, amt)
				genericTx := domain.GenericTransaction{
					Amount:      amt,
					Description: desc,
					Peer:        receiver,
					CreatedAt:   createdAt,
					ExternalID:  externalID,
				}
				txJSON, _ := json.Marshal(genericTx)
				encryptedData, _ := p.cryptoService.Encrypt(masterKey, txJSON)

				sourceAcc := ""
				destAcc := "T212_PORTFOLIO"
				if amt < 0 {
					sourceAcc = "T212_PORTFOLIO"
					destAcc = ""
				}

				newTx := domain.BankTransaction{
					ID:                   uuid.New().String(),
					UserID:               userID,
					IntegrationID:        i.ID,
					AccountID:            "T212_PORTFOLIO",
					SourceAccountID:      sourceAcc,
					DestinationAccountID: destAcc,
					PoolIDs:              poolIDs,
					ExternalID:           externalID,
					EncryptedData:        base64.StdEncoding.EncodeToString(encryptedData),
					CorrelationID:        correlationID,
					CreatedAt:            createdAt,
					SyncedAt:             now,
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

			if reachedOldTransactions {
				break
			}

			// Check for next page
			if t212Resp.JSON200.NextPagePath == nil || *t212Resp.JSON200.NextPagePath == "" {
				break
			}

			// Extract cursor from nextPagePath
			nextURL := *t212Resp.JSON200.NextPagePath
			if idx := strings.Index(nextURL, "cursor="); idx != -1 {
				cursor = nextURL[idx+7:]
				if ampersandIdx := strings.Index(cursor, "&"); ampersandIdx != -1 {
					cursor = cursor[:ampersandIdx]
				}
			} else {
				break
			}
		}

		// 3b. Fetch and check open positions for portfolio changes
		positions, err := p.t212.GetPositions(ctx, config.ApiKey, config.ApiSecret)
		if err != nil {
			log.Printf("[SYNC] [TRADING212] Positions fetch failed: %v", err)
		} else {
			log.Printf("[SYNC] [TRADING212] Fetched %d open positions for portfolio check", len(positions))
			for _, pos := range positions {
				if pos.Instrument == nil || pos.Instrument.Ticker == nil || pos.Quantity == nil || pos.CurrentPrice == nil {
					continue
				}

				ticker := *pos.Instrument.Ticker
				qty := float64(*pos.Quantity)
				currPrice := float64(*pos.CurrentPrice)
				currentValue := qty * currPrice

				// Retrieve previous value from external_cache
				cacheKey := fmt.Sprintf("t212_pos_val_%s_%s_%s", userID, i.ID, ticker)
				var prevStr string
				var prevValue float64
				hasPrev := false

				err := p.db.QueryRow("SELECT data FROM external_cache WHERE key = ?", cacheKey).Scan(&prevStr)
				if err == nil {
					if parsed, parseErr := strconv.ParseFloat(prevStr, 64); parseErr == nil {
						prevValue = parsed
						hasPrev = true
					}
				}

				// If we don't have a previous value, treat it as 0.0 so we generate a change transaction for the full current value
				if !hasPrev {
					prevValue = 0.0
				}

				changeAmount := currentValue - prevValue
				log.Printf("[SYNC] [TRADING212] Position Ticker: %s | Qty: %f | Current Price: %f | Total Price: %f | Prev Value: %f | Change: %f", ticker, qty, currPrice, currentValue, prevValue, changeAmount)

				if math.Abs(changeAmount) >= 0.01 {
					log.Printf("[SYNC] [TRADING212] Generating transaction for position %s change: %f", ticker, changeAmount)

					receiver := fmt.Sprintf("Portfolio change %s", ticker)
					desc := fmt.Sprintf("Portfolio change %s", ticker)

					accountTags := ""
					if meta, ok := config.AccountsMetadata["T212_PORTFOLIO"]; ok && meta != nil {
						accountTags = meta.Tags
					}

					poolIDs, _ := p.ruleService.ProcessTransaction(userID, i.ID, receiver, desc, "", accountTags, changeAmount)

					// Generate a stable and completely unique ExternalID for this specific change event
					externalID := fmt.Sprintf("T212_PORT_CHG_%s_%d", ticker, now.UnixNano())

					genericTx := domain.GenericTransaction{
						Amount:      changeAmount,
						Description: desc,
						Peer:        receiver,
						CreatedAt:   now,
						ExternalID:  externalID,
					}
					txJSON, _ := json.Marshal(genericTx)
					encryptedData, _ := p.cryptoService.Encrypt(masterKey, txJSON)

					sourceAcc := ""
					destAcc := "T212_PORTFOLIO"
					if changeAmount < 0 {
						sourceAcc = "T212_PORTFOLIO"
						destAcc = ""
					}

					newTx := domain.BankTransaction{
						ID:                   uuid.New().String(),
						UserID:               userID,
						IntegrationID:        i.ID,
						AccountID:            "T212_PORTFOLIO",
						SourceAccountID:      sourceAcc,
						DestinationAccountID: destAcc,
						PoolIDs:              poolIDs,
						ExternalID:           externalID,
						EncryptedData:        base64.StdEncoding.EncodeToString(encryptedData),
						CorrelationID:        correlationID,
						CreatedAt:            now,
						SyncedAt:             now,
					}

					allNewTxs = append(allNewTxs, newTx)
					newTxInfos = append(newTxInfos, integration.DecryptedTxInfo{
						Tx:          newTx,
						Amount:      changeAmount,
						Receiver:    receiver,
						Description: desc,
					})
					newCount++
				}

				// Always save/update the current value as the previous value in external_cache
				currentStr := fmt.Sprintf("%f", currentValue)
				_, err = p.db.Exec(`
					INSERT INTO external_cache (key, data, updated_at)
					VALUES (?, ?, CURRENT_TIMESTAMP)
					ON CONFLICT(key) DO UPDATE SET data = excluded.data, updated_at = CURRENT_TIMESTAMP`,
					cacheKey, currentStr)
				if err != nil {
					log.Printf("[SYNC] [TRADING212] Failed to save current value for %s to external_cache: %v", ticker, err)
				}
			}
		}
	}

	// 4. Pending Active Orders
	if !isCashExcluded && !isCashBackedOff {
		activeOrders, err := p.t212.GetActiveOrders(ctx, config.ApiKey, config.ApiSecret)
		if err != nil {
			log.Printf("[SYNC] [TRADING212] Active orders fetch failed: %v", err)
		} else {
			log.Printf("[SYNC] [TRADING212] Fetched %d active/pending orders", len(activeOrders))
			for _, o := range activeOrders {
				if o.Id == nil {
					continue
				}

				// Only BUY orders represent pending cash debits
				if o.Side != nil && *o.Side == "SELL" {
					continue
				}

				createdAt := now
				if o.CreatedAt != nil {
					createdAt = *o.CreatedAt
				}

				// Calculate amount
				amt := 0.0
				if o.Value != nil {
					amt = float64(*o.Value)
				} else if o.Quantity != nil {
					price := 0.0
					if o.LimitPrice != nil {
						price = float64(*o.LimitPrice)
					} else if o.StopPrice != nil {
						price = float64(*o.StopPrice)
					}
					amt = float64(*o.Quantity) * price
				}

				// Make it a negative debit
				amt = -math.Abs(amt)

				ticker := ""
				if o.Ticker != nil {
					ticker = *o.Ticker
				}

				side := "BUY"
				if o.Side != nil {
					side = string(*o.Side)
				}
				receiver := fmt.Sprintf("Pending Buy %s", ticker)
				desc := fmt.Sprintf("PENDING %s %s", side, ticker)

				accountTags := ""
				if meta, ok := config.AccountsMetadata["T212_CASH"]; ok && meta != nil {
					accountTags = meta.Tags
				}

				createdAtStr := createdAt.Format(time.RFC3339)
				uniquePayload := fmt.Sprintf("%.2f-%s-%s", amt, ticker, createdAtStr)
				h := sha256.New()
				h.Write([]byte(uniquePayload))
				externalID := fmt.Sprintf("T212_PENDING_%s", hex.EncodeToString(h.Sum(nil)))

				poolIDs, _ := p.ruleService.ProcessTransaction(userID, i.ID, receiver, desc, "", accountTags, amt)
				genericTx := domain.GenericTransaction{
					Amount:      amt,
					Description: desc,
					Peer:        receiver,
					CreatedAt:   createdAt,
					ExternalID:  externalID,
				}
				orderJSON, _ := json.Marshal(genericTx)
				encryptedData, _ := p.cryptoService.Encrypt(masterKey, orderJSON)

				pendingTx := domain.BankTransaction{
					ID:                   uuid.New().String(),
					UserID:               userID,
					IntegrationID:        i.ID,
					AccountID:            "T212_CASH",
					SourceAccountID:      "T212_CASH",
					DestinationAccountID: "",
					PoolIDs:              poolIDs,
					ExternalID:           externalID,
					EncryptedData:        base64.StdEncoding.EncodeToString(encryptedData),
					CorrelationID:        correlationID,
					CreatedAt:            createdAt,
					SyncedAt:             now,
				}

				allNewTxs = append(allNewTxs, pendingTx)
				newTxInfos = append(newTxInfos, integration.DecryptedTxInfo{
					Tx:          pendingTx,
					Amount:      amt,
					Receiver:    receiver,
					Description: desc,
				})
				newCount++
			}
		}
	}

	log.Printf("[SYNC] [TRADING212] Sync summary for integration '%s': %d new transactions discovered, %d transactions corrected.", i.Name, newCount, correctedCount)

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

	// 5. Update fake 1-hour rate limit backoff and record last synced time on successfully synced virtual accounts
	if config.AccountsMetadata == nil {
		config.AccountsMetadata = make(map[string]*domain.AccountMeta)
	}

	backoffUntil := time.Now().Add(1 * time.Hour)
	nowSync := time.Now().UTC()

	configChanged := false
	if !isCashExcluded && !isCashBackedOff {
		if meta, ok := config.AccountsMetadata["T212_CASH"]; ok && meta != nil {
			meta.BackoffUntil = &backoffUntil
			meta.LastSyncedAt = &nowSync
			meta.Balance = cashBalance
			configChanged = true
		} else {
			config.AccountsMetadata["T212_CASH"] = &domain.AccountMeta{
				Alias: "Cash", Enabled: true, BackoffUntil: &backoffUntil, LastSyncedAt: &nowSync, Balance: cashBalance,
			}
			configChanged = true
		}
	}

	if !isPortfolioExcluded && !isPortfolioBackedOff {
		if meta, ok := config.AccountsMetadata["T212_PORTFOLIO"]; ok && meta != nil {
			meta.BackoffUntil = &backoffUntil
			meta.LastSyncedAt = &nowSync
			meta.Balance = portfolioBalance
			configChanged = true
		} else {
			config.AccountsMetadata["T212_PORTFOLIO"] = &domain.AccountMeta{
				Alias: "Portfolio", Enabled: true, BackoffUntil: &backoffUntil, LastSyncedAt: &nowSync, Balance: portfolioBalance,
			}
			configChanged = true
		}
	}

	if configChanged {
		updatedJSON, _ := json.Marshal(config)
		newEncrypted, _ := p.cryptoService.Encrypt(masterKey, updatedJSON)
		i.EncryptedConfig = base64.StdEncoding.EncodeToString(newEncrypted)
		p.integrationRepo.Save(userID, i)
	}

	if config.LinkedAssetID != "" {
		asset, err := p.assetRepo.GetByID(userID, config.LinkedAssetID)
		if err == nil && asset != nil && asset.ActiveVersion != nil {
			asset.ActiveVersion.TargetValue = fmt.Sprintf("%.2f", totalValue)
			p.assetRepo.Save(userID, asset)
		}
	}

	i.CachedBalance = totalValue
	return integration.SyncResult{DiscoveredCount: newCount}
}

func (p *Provider) ParseTransaction(decryptedData []byte) (integration.TransactionMetadata, error) {
	// Try to unmarshal as GenericTransaction first
	var gt domain.GenericTransaction
	if err := json.Unmarshal(decryptedData, &gt); err == nil && gt.ExternalID != "" {
		return integration.TransactionMetadata{
			Amount:      gt.Amount,
			Receiver:    gt.Peer,
			Description: gt.Description,
			CreatedAt:   gt.CreatedAt,
			ExternalID:  gt.ExternalID,
		}, nil
	}

	// Try to unmarshal as a pending active Order first
	var order trading212.Order
	if err := json.Unmarshal(decryptedData, &order); err == nil && order.Id != nil && order.Status != nil {
		meta := integration.TransactionMetadata{}
		if order.CreatedAt != nil {
			meta.CreatedAt = *order.CreatedAt
		} else {
			meta.CreatedAt = time.Now()
		}

		// Calculate amount
		amt := 0.0
		if order.Value != nil {
			amt = float64(*order.Value)
		} else if order.Quantity != nil {
			price := 0.0
			if order.LimitPrice != nil {
				price = float64(*order.LimitPrice)
			} else if order.StopPrice != nil {
				price = float64(*order.StopPrice)
			}
			amt = float64(*order.Quantity) * price
		}
		if order.Side != nil && *order.Side == "BUY" {
			amt = -math.Abs(amt)
		}
		meta.Amount = amt

		ticker := ""
		if order.Ticker != nil {
			ticker = *order.Ticker
		}
		meta.Receiver = fmt.Sprintf("Pending Buy %s", ticker)
		meta.Description = "PENDING"

		// Generate the same stable ExternalID: (amount + ticker + createdAt)
		createdAtStr := meta.CreatedAt.Format(time.RFC3339)
		uniquePayload := fmt.Sprintf("%.2f-%s-%s", amt, ticker, createdAtStr)
		h := sha256.New()
		h.Write([]byte(uniquePayload))
		meta.ExternalID = fmt.Sprintf("T212_PENDING_%s", hex.EncodeToString(h.Sum(nil)))

		return meta, nil
	}

	// Try to unmarshal as HistoryTransactionItem
	var dt trading212.HistoryTransactionItem
	if err := json.Unmarshal(decryptedData, &dt); err != nil {
		var raw map[string]interface{}
		if unmarshalErr := json.Unmarshal(decryptedData, &raw); unmarshalErr == nil {
			keys := make([]string, 0, len(raw))
			for k := range raw {
				keys = append(keys, k)
			}
			log.Printf("[SYNC] [TRADING212] ParseTransaction failed: %v. Available JSON keys: %v", err, keys)
		}

		// Try minimal unmarshal for metadata
		var item struct {
			DateTime  *time.Time `json:"date_time"`
			Reference *string    `json:"reference"`
			Amount    *float64   `json:"amount"`
			Currency  *string    `json:"currency"`
			Type      *string    `json:"type"`
		}
		if err := json.Unmarshal(decryptedData, &item); err == nil && item.DateTime != nil {
			dt_str := item.DateTime.Format(time.RFC3339)
			ref := ""
			if item.Reference != nil {
				ref = *item.Reference
			}
			amt := 0.0
			if item.Amount != nil {
				amt = *item.Amount
			}
			cur := ""
			if item.Currency != nil {
				cur = *item.Currency
			}
			typ := ""
			if item.Type != nil {
				typ = *item.Type
			}

			uniquePayload := fmt.Sprintf("%s-%s-%s-%.4f-%s", typ, dt_str, ref, amt, cur)
			h := sha256.New()
			h.Write([]byte(uniquePayload))

			return integration.TransactionMetadata{
				CreatedAt:  *item.DateTime,
				ExternalID: hex.EncodeToString(h.Sum(nil)),
			}, nil
		}
		return integration.TransactionMetadata{}, err
	}

	meta := integration.TransactionMetadata{}
	if dt.Reference != nil {
		meta.Receiver = *dt.Reference
	}
	if dt.Type != nil {
		meta.Description = string(*dt.Type)
	}
	if meta.Receiver == "" {
		meta.Receiver = meta.Description
	}
	if dt.Amount != nil {
		meta.Amount = float64(*dt.Amount)
	}
	if meta.Description == "WITHDRAW" || meta.Description == "WITHDRAWAL" {
		meta.Amount = -math.Abs(meta.Amount)
	}

	if dt.DateTime != nil {
		meta.CreatedAt = *dt.DateTime
	}

	// Generate stable ExternalID
	dt_str := meta.CreatedAt.Format(time.RFC3339)
	ref := ""
	if dt.Reference != nil {
		ref = *dt.Reference
	}
	amt := 0.0
	if dt.Amount != nil {
		amt = float64(*dt.Amount)
	}
	cur := ""
	if dt.Currency != nil {
		cur = *dt.Currency
	}
	typ := ""
	if dt.Type != nil {
		typ = string(*dt.Type)
	}

	uniquePayload := fmt.Sprintf("%s-%s-%s-%.4f-%s", typ, dt_str, ref, amt, cur)
	h := sha256.New()
	h.Write([]byte(uniquePayload))
	meta.ExternalID = hex.EncodeToString(h.Sum(nil))

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

	// Add fixed account IDs for Trading212
	idMap["T212_CASH"] = true
	idMap["T212_PORTFOLIO"] = true

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

		// Set default aliases for Trading212 fixed IDs if not set
		if name == "T212_CASH" {
			name = "Cash"
		} else if name == "T212_PORTFOLIO" {
			name = "Portfolio"
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
