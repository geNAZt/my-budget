package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/web/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/service"
	"github.com/google/uuid"
)

type System struct {
	handler       *api.WebSocketHandler
	logService    *service.LogService
	dockerService *service.DockerService
	syncService   *service.SyncService
	db            *sql.DB

	backendVersion   string
	watchtowerStatus string
	statusMu         sync.RWMutex
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

func NewSystem(handler *api.WebSocketHandler, logService *service.LogService, dockerService *service.DockerService, syncService *service.SyncService, db *sql.DB) *System {
	sys := &System{
		handler:       handler,
		logService:    logService,
		dockerService: dockerService,
		syncService:   syncService,
		db:            db,

		backendVersion:   os.Getenv("GIT_COMMIT"),
		watchtowerStatus: "green",
	}

	go sys.startWatchtowerMonitor()

	return sys
}

type userIntegration struct {
	ID          string
	Name        string
	ServiceType string
	AccountIDs  []string
	ReqID       string
}

func (sys *System) loadUserIntegrations(userID string) ([]userIntegration, error) {
	var userInts []userIntegration
	rowsI, err := sys.db.Query("SELECT id, name, service_type, encrypted_config FROM integrations WHERE user_id = $1", userID)
	if err != nil {
		return nil, err
	}
	defer rowsI.Close()
	for rowsI.Next() {
		var id, name, st, encConfig string
		if err := rowsI.Scan(&id, &name, &st, &encConfig); err == nil {
			var accIDs []string
			var requisitionID string

			configBytes, errDec := sys.syncService.DecryptIntegrationConfig(userID, &domain.Integration{ID: id, EncryptedConfig: encConfig})
			if errDec == nil {
				var config struct {
					RequisitionID string   `json:"requisition_id"`
					AccountIDs    []string `json:"account_ids"`
					AccountsMetadata map[string]interface{} `json:"accounts_metadata"`
				}
				if json.Unmarshal(configBytes, &config) == nil {
					requisitionID = config.RequisitionID
					accIDs = config.AccountIDs
					for accID := range config.AccountsMetadata {
						accIDs = append(accIDs, accID)
					}
				}
			}

			userInts = append(userInts, userIntegration{
				ID:          id,
				Name:        name,
				ServiceType: st,
				AccountIDs:  accIDs,
				ReqID:       requisitionID,
			})
		}
	}
	return userInts, nil
}



// Containers lists available containers in the system
func (sys *System) Containers(s *api.WebsocketSession, reqID string, _ *apiproto.Empty) {
	userID, _ := s.GetAuth()
	if userID == "" {
		sys.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	ctx := context.Background()
	var list apiproto.ContainerList

	if sys.dockerService.IsAvailable() {
		containers, err := sys.dockerService.ListContainers(ctx)
		if err == nil {
			for _, c := range containers {
				name := ""
				if len(c.Names) > 0 {
					name = c.Names[0]
				}
				list.Containers = append(list.Containers, &apiproto.ContainerInfo{
					Id:     c.ID,
					Name:   name,
					State:  c.State,
					Status: c.Status,
				})
			}
		}
	}

	sys.handler.SendResponse(s, reqID, &list, false)
}

// Logs streams logs to the client based on selected container
func (sys *System) Logs(s *api.WebsocketSession, reqID string, req *apiproto.SystemLogRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		sys.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	containerID := ""
	if req != nil {
		containerID = req.ContainerId
	}

	// Case 1: Stream standard backend logs using current process LogService
	if containerID == "" || containerID == "current" {
		subID := uuid.New().String()
		ch, initialBuffer := sys.logService.Subscribe(subID)
		defer sys.logService.Unsubscribe(subID)

		// Send initial buffer in one batch for performance
		if len(initialBuffer) > 0 {
			if s.IsClosed() {
				return
			}
			if err := sys.handler.SendResponse(s, reqID, &apiproto.SystemLogChunk{Lines: initialBuffer}, false); err != nil {
				return
			}
		}

		// Stream new logs with heartbeat
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case line, ok := <-ch:
				if !ok {
					sys.handler.SendResponse(s, reqID, nil, true)
					return
				}
				if s.IsClosed() {
					return
				}
				if err := sys.handler.SendResponse(s, reqID, &apiproto.SystemLogChunk{Line: line}, false); err != nil {
					return
				}
			case <-ticker.C:
				// Heartbeat to keep connection alive and detect dead sessions
				if s.IsClosed() {
					return
				}
				if err := sys.handler.SendResponse(s, reqID, &apiproto.SystemLogChunk{}, false); err != nil {
					return
				}
			}
		}
	}

	// Case 2: Stream logs from a Docker container
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logChan := make(chan string, 1000)

	// Start streaming docker logs in a goroutine
	go func() {
		defer close(logChan)
		err := sys.dockerService.StreamLogs(ctx, containerID, logChan)
		if err != nil {
			select {
			case logChan <- fmt.Sprintf("[Docker Logs Error] %v", err):
			case <-ctx.Done():
			}
		}
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case line, ok := <-logChan:
			if !ok {
				sys.handler.SendResponse(s, reqID, nil, true)
				return
			}
			if s.IsClosed() {
				return
			}
			if err := sys.handler.SendResponse(s, reqID, &apiproto.SystemLogChunk{Line: line}, false); err != nil {
				return
			}
		case <-ticker.C:
			// Heartbeat
			if s.IsClosed() {
				return
			}
			if err := sys.handler.SendResponse(s, reqID, &apiproto.SystemLogChunk{}, false); err != nil {
				return
			}
		}
	}
}

// SyncRuns lists all sync runs (logs) for the authenticated user
func (sys *System) SyncRuns(s *api.WebsocketSession, reqID string, _ *apiproto.Empty) {
	userID, _ := s.GetAuth()
	if userID == "" {
		sys.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	baseDir := "/app"
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		if _, err := os.Stat("web"); err == nil {
			baseDir = "web"
		} else {
			baseDir = "."
		}
	}
	logsDir := filepath.Join(baseDir, "logs", "sync_runs")

	// Get correlation mappings from db
	dbMappings := make(map[string]struct{ IntegrationID, IntegrationName, ServiceType string })
	rowsC, err := sys.db.Query(`
		SELECT DISTINCT t.correlation_id, t.integration_id, i.name, i.service_type
		FROM bank_transactions t
		JOIN integrations i ON t.integration_id = i.id
		WHERE t.user_id = $1 AND t.correlation_id != ''`, userID)
	if err == nil {
		defer rowsC.Close()
		for rowsC.Next() {
			var cid, iid, name, st string
			if err := rowsC.Scan(&cid, &iid, &name, &st); err == nil {
				dbMappings[cid] = struct{ IntegrationID, IntegrationName, ServiceType string }{iid, name, st}
			}
		}
	}

	// Load user integrations for fallback matching
	userInts, _ := sys.loadUserIntegrations(userID)

	var runs []*apiproto.SyncRun

	// Read logs directory
	entries, err := os.ReadDir(logsDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			correlationID := entry.Name()

			// Check metadata.json
			metaPath := filepath.Join(logsDir, correlationID, "metadata.json")
			var meta syncMetadata
			hasMeta := false

			metaBytes, err := os.ReadFile(metaPath)
			if err == nil {
				if err := json.Unmarshal(metaBytes, &meta); err == nil {
					hasMeta = true
				}
			}

			// Security: check user ownership
			integrationID := ""
			integrationName := ""
			serviceType := ""
			status := ""
			timestamp := ""

			// Pre-check for log files to use in cleanup and later
			respFiles, _ := filepath.Glob(filepath.Join(logsDir, correlationID, "*_resp.json"))
			reqFiles, _ := filepath.Glob(filepath.Join(logsDir, correlationID, "*_req.json"))
			isEmpty := len(respFiles) == 0 && len(reqFiles) == 0

			if hasMeta {
				if meta.UserID != userID {
					continue
				}

				// Cleanup: delete empty logs older than 24h
				if isEmpty && time.Since(meta.Timestamp) > 24*time.Hour {
					os.RemoveAll(filepath.Join(logsDir, correlationID))
					continue
				}

				integrationID = meta.IntegrationID
				integrationName = meta.IntegrationName
				serviceType = meta.ServiceType
				status = meta.Status
				timestamp = meta.Timestamp.Format(time.RFC3339)
			} else {
				// Check db mappings
				mapping, ok := dbMappings[correlationID]
				if ok {
					// Cleanup: delete empty logs older than 24h
					if isEmpty {
						if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
							os.RemoveAll(filepath.Join(logsDir, correlationID))
							continue
						}
					}

					integrationID = mapping.IntegrationID
					integrationName = mapping.IntegrationName
					serviceType = mapping.ServiceType
					status = "COMPLETED"
				} else {
					// Fallback: guess/match based on user's integrations and log files
					if isEmpty {
						// Cleanup orphaned empty old logs
						if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
							os.RemoveAll(filepath.Join(logsDir, correlationID))
						}
						continue
					}

					matches := respFiles
					if len(matches) == 0 {
						matches = reqFiles
					}

					detectedST := ""
					firstFile := filepath.Base(matches[0])
					if strings.Contains(firstFile, "gocardless") {
						detectedST = "GOCARDLESS"
					} else if strings.Contains(firstFile, "enablebanking") {
						detectedST = "ENABLEBANKING"
					} else if strings.Contains(firstFile, "trading212") {
						detectedST = "TRADING212"
					}
					if detectedST == "" {
						continue
					}

					var candidateInts []userIntegration
					for _, ui := range userInts {
						if ui.ServiceType == detectedST {
							candidateInts = append(candidateInts, ui)
						}
					}
					if len(candidateInts) == 0 {
						continue
					}

					var matchedInt *userIntegration
					if len(candidateInts) == 1 {
						matchedInt = &candidateInts[0]
					} else {
						content, err := os.ReadFile(matches[0])
						if err == nil {
							contentStr := string(content)
							for i := range candidateInts {
								ui := &candidateInts[i]
								if ui.ReqID != "" && strings.Contains(contentStr, ui.ReqID) {
									matchedInt = ui
									break
								}
								for _, accID := range ui.AccountIDs {
									if accID != "" && strings.Contains(contentStr, accID) {
										matchedInt = ui
										break
									}
								}
								if matchedInt != nil {
									break
								}
							}
						}
						if matchedInt == nil {
							matchedInt = &candidateInts[0]
						}
					}

					integrationID = matchedInt.ID
					integrationName = matchedInt.Name
					serviceType = matchedInt.ServiceType
					status = "COMPLETED"
				}

				// Mod time of folder
				if info, err := entry.Info(); err == nil {
					timestamp = info.ModTime().Format(time.RFC3339)
				}
			}

			// Parse response files in the folder to count transactions
			txCount := int32(0)
			for _, rf := range respFiles {
				if content, err := os.ReadFile(rf); err == nil {
					var dump struct {
						Body interface{} `json:"body"`
					}
					if err := json.Unmarshal(content, &dump); err == nil && dump.Body != nil {
						detected := parseTransactionsFromJSON(strings.ToLower(serviceType), dump.Body)
						txCount += int32(len(detected))
					}
				}
			}

			runs = append(runs, &apiproto.SyncRun{
				CorrelationId:    correlationID,
				IntegrationId:    integrationID,
				IntegrationName:  integrationName,
				ServiceType:      serviceType,
				Timestamp:        timestamp,
				Status:           status,
				TransactionCount: txCount,
				HasLogFiles:      !isEmpty,
			})
		}
	}

	// Sort runs by date DESC
	sort.Slice(runs, func(i, j int) bool {
		ti, errI := time.Parse(time.RFC3339, runs[i].Timestamp)
		tj, errJ := time.Parse(time.RFC3339, runs[j].Timestamp)
		if errI != nil || errJ != nil {
			return runs[i].Timestamp > runs[j].Timestamp
		}
		return ti.After(tj)
	})

	sys.handler.SendResponse(s, reqID, &apiproto.SyncRunList{Runs: runs}, false)
}

// SyncRunDetails returns details of a specific sync run
func (sys *System) SyncRunDetails(s *api.WebsocketSession, reqID string, req *apiproto.SyncRunDetailsRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		sys.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	correlationID := req.CorrelationId
	if correlationID == "" {
		sys.handler.SendError(s, reqID, http.StatusBadRequest, "Missing correlation ID")
		return
	}

	baseDir := "/app"
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		if _, err := os.Stat("web"); err == nil {
			baseDir = "web"
		} else {
			baseDir = "."
		}
	}
	logsDir := filepath.Join(baseDir, "logs", "sync_runs")
	runDir := filepath.Join(logsDir, correlationID)

	// Check metadata.json
	metaPath := filepath.Join(runDir, "metadata.json")
	var meta syncMetadata
	hasMeta := false

	metaBytes, err := os.ReadFile(metaPath)
	if err == nil {
		if err := json.Unmarshal(metaBytes, &meta); err == nil {
			hasMeta = true
		}
	}

	// Security: verify ownership
	integrationID := ""
	integrationName := ""
	serviceType := ""
	timestamp := ""

	if hasMeta {
		if meta.UserID != userID {
			sys.handler.SendError(s, reqID, http.StatusForbidden, "Forbidden")
			return
		}
		integrationID = meta.IntegrationID
		integrationName = meta.IntegrationName
		serviceType = meta.ServiceType
		timestamp = meta.Timestamp.Format(time.RFC3339)
	} else {
		// Verify via db mappings
		var iid, name, st string
		err := sys.db.QueryRow(`
			SELECT DISTINCT t.integration_id, i.name, i.service_type
			FROM bank_transactions t
			JOIN integrations i ON t.integration_id = i.id
			WHERE t.user_id = $1 AND t.correlation_id = $2`, userID, correlationID).Scan(&iid, &name, &st)
		if err == nil {
			integrationID = iid
			integrationName = name
			serviceType = st
		} else {
			// Fallback: guess/match based on user's integrations and log files
			userInts, _ := sys.loadUserIntegrations(userID)
			matches, _ := filepath.Glob(filepath.Join(runDir, "*_resp.json"))
			if len(matches) == 0 {
				matches, _ = filepath.Glob(filepath.Join(runDir, "*_req.json"))
			}
			if len(matches) == 0 {
				sys.handler.SendError(s, reqID, http.StatusForbidden, "Forbidden or not found")
				return
			}

			detectedST := ""
			firstFile := filepath.Base(matches[0])
			if strings.Contains(firstFile, "gocardless") {
				detectedST = "GOCARDLESS"
			} else if strings.Contains(firstFile, "enablebanking") {
				detectedST = "ENABLEBANKING"
			} else if strings.Contains(firstFile, "trading212") {
				detectedST = "TRADING212"
			}
			if detectedST == "" {
				sys.handler.SendError(s, reqID, http.StatusForbidden, "Forbidden or not found")
				return
			}

			var candidateInts []userIntegration
			for _, ui := range userInts {
				if ui.ServiceType == detectedST {
					candidateInts = append(candidateInts, ui)
				}
			}
			if len(candidateInts) == 0 {
				sys.handler.SendError(s, reqID, http.StatusForbidden, "Forbidden or not found")
				return
			}

			var matchedInt *userIntegration
			if len(candidateInts) == 1 {
				matchedInt = &candidateInts[0]
			} else {
				content, err := os.ReadFile(matches[0])
				if err == nil {
					contentStr := string(content)
					for i := range candidateInts {
						ui := &candidateInts[i]
						if ui.ReqID != "" && strings.Contains(contentStr, ui.ReqID) {
							matchedInt = ui
							break
						}
						for _, accID := range ui.AccountIDs {
							if accID != "" && strings.Contains(contentStr, accID) {
								matchedInt = ui
								break
							}
						}
						if matchedInt != nil {
							break
						}
					}
				}
				if matchedInt == nil {
					matchedInt = &candidateInts[0]
				}
			}

			integrationID = matchedInt.ID
			integrationName = matchedInt.Name
			serviceType = matchedInt.ServiceType
		}

		// mod time of folder
		if info, err := os.Stat(runDir); err == nil {
			timestamp = info.ModTime().Format(time.RFC3339)
		}
	}

	// Read all files in folder
	files, err := os.ReadDir(runDir)
	if err != nil {
		sys.handler.SendError(s, reqID, http.StatusNotFound, "Sync run log folder not found")
		return
	}

	var detectedTxs []*apiproto.DetectedTransaction
	var rawLogs []*apiproto.SyncRawLogFile

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		filename := f.Name()
		filePath := filepath.Join(runDir, filename)

		// Only return req/resp JSON files, skip metadata.json
		if filename == "metadata.json" {
			continue
		}

		contentBytes, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		isRequest := strings.HasSuffix(filename, "_req.json")
		isResponse := strings.HasSuffix(filename, "_resp.json")

		if !isRequest && !isResponse {
			continue
		}

		// Parse detected transactions from response files
		if isResponse {
			var dump struct {
				Body interface{} `json:"body"`
			}
			if err := json.Unmarshal(contentBytes, &dump); err == nil && dump.Body != nil {
				txs := parseTransactionsFromJSON(strings.ToLower(serviceType), dump.Body)
				for _, tx := range txs {
					// Find other sync runs containing this transaction ID
					otherRuns := findOtherSyncRunsForTx(sys.db, userID, correlationID, tx.ExternalId, logsDir)

					detectedTxs = append(detectedTxs, &apiproto.DetectedTransaction{
						ExternalId:    tx.ExternalId,
						Amount:        tx.Amount,
						Currency:      tx.Currency,
						Description:   tx.Description,
						Peer:          tx.Peer,
						BookingDate:   tx.BookingDate,
						RawJson:       tx.RawJson,
						OtherSyncRuns: otherRuns,
					})
				}
			}
		}

		rawLogs = append(rawLogs, &apiproto.SyncRawLogFile{
			Filename:  filename,
			Content:   string(contentBytes),
			IsRequest: isRequest,
		})
	}

	// Sort rawLogs by name
	sort.Slice(rawLogs, func(i, j int) bool {
		return rawLogs[i].Filename < rawLogs[j].Filename
	})

	sys.handler.SendResponse(s, reqID, &apiproto.SyncRunDetailsResponse{
		CorrelationId:        correlationID,
		IntegrationId:        integrationID,
		IntegrationName:      integrationName,
		ServiceType:          serviceType,
		Timestamp:            timestamp,
		DetectedTransactions: detectedTxs,
		RawLogs:              rawLogs,
	}, false)
}

// Helper to find other sync runs containing the same transaction ID
func findOtherSyncRunsForTx(db *sql.DB, userID string, currentCorrelationID string, txID string, logsDir string) []*apiproto.SyncRunInfoForTx {
	var results []*apiproto.SyncRunInfoForTx

	// Query database first since it is extremely fast
	rows, err := db.Query(`
		SELECT DISTINCT t.correlation_id, t.synced_at, i.name
		FROM bank_transactions t
		JOIN integrations i ON t.integration_id = i.id
		WHERE t.user_id = $1 AND t.correlation_id != $2 AND (t.external_id = $3 OR t.external_id LIKE '%' || $3)`,
		userID, currentCorrelationID, txID)
	if err == nil {
		defer rows.Close()
		seenCorrelationIDs := make(map[string]bool)
		for rows.Next() {
			var cid, name string
			var syncedAt time.Time
			if err := rows.Scan(&cid, &syncedAt, &name); err == nil {
				seenCorrelationIDs[cid] = true
				results = append(results, &apiproto.SyncRunInfoForTx{
					CorrelationId:   cid,
					Timestamp:       syncedAt.Format(time.RFC3339),
					IntegrationName: name,
				})
			}
		}
	}

	// Scan filesystem for other matches (covers runs where transaction was filtered/skipped and not written to DB)
	entries, err := os.ReadDir(logsDir)
	if err == nil {
		seen := make(map[string]bool)
		for _, r := range results {
			seen[r.CorrelationId] = true
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			cid := entry.Name()
			if cid == currentCorrelationID || seen[cid] {
				continue
			}

			// Check metadata
			metaPath := filepath.Join(logsDir, cid, "metadata.json")
			metaBytes, err := os.ReadFile(metaPath)
			if err != nil {
				continue
			}
			var meta syncMetadata
			if err := json.Unmarshal(metaBytes, &meta); err != nil || meta.UserID != userID {
				continue
			}

			// Substring check on response files (extremely fast pre-filter)
			respFiles, _ := filepath.Glob(filepath.Join(logsDir, cid, "*_resp.json"))
			foundTx := false
			for _, rf := range respFiles {
				content, err := os.ReadFile(rf)
				if err != nil {
					continue
				}
				if strings.Contains(string(content), txID) {
					var dump struct {
						Body interface{} `json:"body"`
					}
					if err := json.Unmarshal(content, &dump); err == nil && dump.Body != nil {
						detected := parseTransactionsFromJSON(strings.ToLower(meta.ServiceType), dump.Body)
						for _, t := range detected {
							if t.ExternalId == txID {
								foundTx = true
								break
							}
						}
					}
				}
				if foundTx {
					break
				}
			}

			if foundTx {
				results = append(results, &apiproto.SyncRunInfoForTx{
					CorrelationId:   cid,
					Timestamp:       meta.Timestamp.Format(time.RFC3339),
					IntegrationName: meta.IntegrationName,
				})
				seen[cid] = true
			}
		}
	}

	return results
}

// Helper to extract transactions from unmarshaled JSON body
func parseTransactionsFromJSON(serviceType string, body interface{}) []*apiproto.DetectedTransaction {
	var txs []*apiproto.DetectedTransaction

	parseTxObj := func(obj map[string]interface{}) (*apiproto.DetectedTransaction, bool) {
		id := ""
		if val, ok := obj["transactionId"].(string); ok {
			id = val
		} else if val, ok := obj["transaction_id"].(string); ok {
			id = val
		} else if val, ok := obj["orderId"].(string); ok {
			id = val
		} else if val, ok := obj["id"].(string); ok {
			id = val
		} else if val, ok := obj["entryReference"].(string); ok {
			id = val
		} else if val, ok := obj["entry_reference"].(string); ok {
			id = val
		}
		if id == "" {
			return nil, false
		}

		dt := &apiproto.DetectedTransaction{
			ExternalId: id,
		}

		// Amount and Currency
		amount := 0.0
		currency := ""
		if amtVal, ok := obj["transactionAmount"].(map[string]interface{}); ok {
			if aStr, ok := amtVal["amount"].(string); ok {
				amount, _ = strconv.ParseFloat(aStr, 64)
			}
			if cStr, ok := amtVal["currency"].(string); ok {
				currency = cStr
			}
		} else if amtVal, ok := obj["transaction_amount"].(map[string]interface{}); ok {
			if aStr, ok := amtVal["amount"].(string); ok {
				amount, _ = strconv.ParseFloat(aStr, 64)
			}
			if cStr, ok := amtVal["currency"].(string); ok {
				currency = cStr
			}
		} else if aNum, ok := obj["amount"].(float64); ok {
			amount = aNum
		} else if aStr, ok := obj["amount"].(string); ok {
			amount, _ = strconv.ParseFloat(aStr, 64)
		} else if cashNum, ok := obj["cash"].(float64); ok {
			amount = cashNum
		} else if totalNum, ok := obj["total"].(float64); ok {
			amount = totalNum
		}
		dt.Amount = amount
		dt.Currency = currency

		// Description/Remittance
		desc := ""
		if val, ok := obj["remittanceInformationUnstructured"].(string); ok {
			desc = val
		} else if val, ok := obj["remittanceInformationUnstructuredArray"].([]interface{}); ok && len(val) > 0 {
			var parts []string
			for _, p := range val {
				if s, ok := p.(string); ok {
					parts = append(parts, s)
				}
			}
			desc = strings.Join(parts, " ")
		} else if val, ok := obj["remittance_information"].([]interface{}); ok && len(val) > 0 {
			var parts []string
			for _, p := range val {
				if s, ok := p.(string); ok {
					parts = append(parts, s)
				}
			}
			desc = strings.Join(parts, " ")
		} else if val, ok := obj["description"].(string); ok {
			desc = val
		} else if val, ok := obj["name"].(string); ok {
			desc = val
		} else if val, ok := obj["symbol"].(string); ok {
			desc = val
		}
		dt.Description = desc

		// Peer
		peer := ""
		if val, ok := obj["creditorName"].(string); ok {
			peer = val
		} else if val, ok := obj["debtorName"].(string); ok && peer == "" {
			peer = val
		} else if cred, ok := obj["creditor"].(map[string]interface{}); ok {
			if name, ok := cred["name"].(string); ok {
				peer = name
			}
		} else if deb, ok := obj["debtor"].(map[string]interface{}); ok && peer == "" {
			if name, ok := deb["name"].(string); ok {
				peer = name
			}
		} else if val, ok := obj["peer"].(string); ok {
			peer = val
		}
		dt.Peer = peer

		// Booking Date
		date := ""
		if val, ok := obj["bookingDate"].(string); ok {
			date = val
		} else if val, ok := obj["booking_date"].(string); ok {
			date = val
		} else if val, ok := obj["valueDate"].(string); ok {
			date = val
		} else if val, ok := obj["value_date"].(string); ok {
			date = val
		} else if val, ok := obj["date"].(string); ok {
			date = val
		} else if val, ok := obj["timestamp"].(string); ok {
			date = val
		} else if val, ok := obj["time"].(string); ok {
			date = val
		}
		dt.BookingDate = date

		// Raw JSON
		rawBytes, _ := json.Marshal(obj)
		dt.RawJson = string(rawBytes)

		return dt, true
	}

	var scanVal func(val interface{})
	scanVal = func(val interface{}) {
		if val == nil {
			return
		}
		switch v := val.(type) {
		case map[string]interface{}:
			if dt, ok := parseTxObj(v); ok {
				txs = append(txs, dt)
				return
			}
			for _, child := range v {
				scanVal(child)
			}
		case []interface{}:
			for _, child := range v {
				scanVal(child)
			}
		}
	}

	scanVal(body)
	return txs
}

// Status returns the current system version and watchtower status
func (sys *System) Status(s *api.WebsocketSession, reqID string, _ *apiproto.Empty) {
	sys.statusMu.RLock()
	resp := &apiproto.SystemStatus{
		BackendVersion:   sys.backendVersion,
		WatchtowerStatus: sys.watchtowerStatus,
	}
	sys.statusMu.RUnlock()

	sys.handler.SendResponse(s, reqID, resp, false)
}

func (sys *System) startWatchtowerMonitor() {
	for {
		ctx := context.Background()
		containerID, err := sys.dockerService.FindWatchtowerContainer(ctx)
		if err != nil {
			time.Sleep(30 * time.Second)
			continue
		}

		logChan := make(chan string)
		monitorCtx, cancel := context.WithCancel(ctx)
		
		go func() {
			if err := sys.dockerService.StreamLogs(monitorCtx, containerID, logChan); err != nil {
				fmt.Printf("[SYSTEM] Watchtower log stream error: %v\n", err)
			}
			close(logChan)
		}()

		for line := range logChan {
			sys.parseWatchtowerLog(line)
		}

		cancel()
		time.Sleep(5 * time.Second)
	}
}

func (sys *System) parseWatchtowerLog(line string) {
	status := ""
	line = strings.ToLower(line)

	if strings.Contains(line, "checking all containers") {
		status = "red"
	} else if strings.Contains(line, "found new image for") {
		status = "yellow"
	} else if strings.Contains(line, "stopping") || strings.Contains(line, "starting") ||
		strings.Contains(line, "removing") || strings.Contains(line, "creating") ||
		strings.Contains(line, "updating") {
		status = "yellow-blinking"
	} else if strings.Contains(line, "session lasted") {
		status = "green"
	}

	if status != "" {
		sys.statusMu.Lock()
		if sys.watchtowerStatus != status {
			sys.watchtowerStatus = status
			sys.statusMu.Unlock()
			sys.broadcastStatus()
		} else {
			sys.statusMu.Unlock()
		}
	}
}

func (sys *System) broadcastStatus() {
	sys.statusMu.RLock()
	status := &apiproto.SystemStatus{
		BackendVersion:   sys.backendVersion,
		WatchtowerStatus: sys.watchtowerStatus,
	}
	sys.statusMu.RUnlock()

	sys.handler.BroadcastEvent("system::status", status)
}


