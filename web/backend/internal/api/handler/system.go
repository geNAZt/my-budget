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

	// Load user integrations for fallback matching
	userInts, _ := sys.loadUserIntegrations(userID)

	// Read logs directory
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if !s.IsClosed() {
			sys.handler.SendResponse(s, reqID, nil, true)
		}
		return
	}

	var directories []os.DirEntry
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, entry)
		}
	}

	numWorkers := 16
	if len(directories) < numWorkers {
		numWorkers = len(directories)
	}

	taskChan := make(chan os.DirEntry, len(directories))
	for _, entry := range directories {
		taskChan <- entry
	}
	close(taskChan)

	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for entry := range taskChan {
				correlationID := entry.Name()
				runDir := filepath.Join(logsDir, correlationID)

				info, err := entry.Info()
				if err != nil {
					continue
				}

				timestampStr := info.ModTime().Format(time.RFC3339)

				// List files in the folder (fast os.ReadDir)
				runFiles, err := os.ReadDir(runDir)
				if err != nil {
					continue
				}

				var respFiles []string
				var reqFiles []string
				for _, f := range runFiles {
					if f.IsDir() {
						continue
					}
					name := f.Name()
					if strings.HasSuffix(name, "_resp.json") {
						respFiles = append(respFiles, filepath.Join(runDir, name))
					} else if strings.HasSuffix(name, "_req.json") {
						reqFiles = append(reqFiles, filepath.Join(runDir, name))
					}
				}

				isEmpty := len(respFiles) == 0 && len(reqFiles) == 0

				// Cleanup empty logs older than 24h
				if isEmpty && time.Since(info.ModTime()) > 24*time.Hour {
					os.RemoveAll(runDir)
					continue
				}

				// Skip empty runs
				if isEmpty {
					continue
				}

				// Fallback guess: guess/match based on user's integrations and log files
				matches := respFiles
				if len(matches) == 0 {
					matches = reqFiles
				}
				if len(matches) == 0 {
					continue
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

				integrationID := ""
				integrationName := ""
				serviceType := detectedST

				var candidateInts []userIntegration
				for _, ui := range userInts {
					if ui.ServiceType == detectedST {
						candidateInts = append(candidateInts, ui)
					}
				}
				if len(candidateInts) == 1 {
					integrationID = candidateInts[0].ID
					integrationName = candidateInts[0].Name
				} else if len(candidateInts) > 1 {
					content, err := os.ReadFile(matches[0])
					if err == nil {
						contentStr := string(content)
						for i := range candidateInts {
							ui := &candidateInts[i]
							if ui.ReqID != "" && strings.Contains(contentStr, ui.ReqID) {
								integrationID = ui.ID
								integrationName = ui.Name
								break
							}
							for _, accID := range ui.AccountIDs {
								if accID != "" && strings.Contains(contentStr, accID) {
									integrationID = ui.ID
									integrationName = ui.Name
									break
								}
							}
						}
					}
					if integrationID == "" {
						integrationID = candidateInts[0].ID
						integrationName = candidateInts[0].Name
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

				run := &apiproto.SyncRun{
					CorrelationId:    correlationID,
					IntegrationId:    integrationID,
					IntegrationName:  integrationName,
					ServiceType:      serviceType,
					Timestamp:        timestampStr,
					Status:           "COMPLETED",
					TransactionCount: txCount,
					HasLogFiles:      !isEmpty,
				}

				if s.IsClosed() {
					return
				}
				_ = sys.handler.SendResponse(s, reqID, run, false)
			}
		}()
	}

	wg.Wait()

	// Send terminal done signal
	if !s.IsClosed() {
		sys.handler.SendResponse(s, reqID, nil, true)
	}
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

	// mod time of folder
	timestamp := ""
	if info, err := os.Stat(runDir); err == nil {
		timestamp = info.ModTime().Format(time.RFC3339)
	} else {
		sys.handler.SendError(s, reqID, http.StatusNotFound, "Sync run log folder not found")
		return
	}

	// Load user integrations for fallback matching
	userInts, _ := sys.loadUserIntegrations(userID)

	// Read files in current run folder
	files, err := os.ReadDir(runDir)
	if err != nil {
		sys.handler.SendError(s, reqID, http.StatusNotFound, "Sync run log folder not found")
		return
	}

	var respFiles []string
	var reqFiles []string
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		filename := f.Name()
		filePath := filepath.Join(runDir, filename)

		if filename == "metadata.json" {
			continue
		}

		isRequest := strings.HasSuffix(filename, "_req.json")
		isResponse := strings.HasSuffix(filename, "_resp.json")

		if !isRequest && !isResponse {
			continue
		}

		if isResponse {
			respFiles = append(respFiles, filePath)
		} else if isRequest {
			reqFiles = append(reqFiles, filePath)
		}
	}

	// Guess/match integration based on user's integrations and log files
	matches := respFiles
	if len(matches) == 0 {
		matches = reqFiles
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

	integrationID := ""
	integrationName := ""
	serviceType := detectedST

	var candidateInts []userIntegration
	for _, ui := range userInts {
		if ui.ServiceType == detectedST {
			candidateInts = append(candidateInts, ui)
		}
	}

	if len(candidateInts) == 1 {
		integrationID = candidateInts[0].ID
		integrationName = candidateInts[0].Name
	} else if len(candidateInts) > 1 {
		content, err := os.ReadFile(matches[0])
		if err == nil {
			contentStr := string(content)
			for i := range candidateInts {
				ui := &candidateInts[i]
				if ui.ReqID != "" && strings.Contains(contentStr, ui.ReqID) {
					integrationID = ui.ID
					integrationName = ui.Name
					break
				}
				for _, accID := range ui.AccountIDs {
					if accID != "" && strings.Contains(contentStr, accID) {
						integrationID = ui.ID
						integrationName = ui.Name
						break
					}
				}
			}
		}
		if integrationID == "" {
			integrationID = candidateInts[0].ID
			integrationName = candidateInts[0].Name
		}
	}

	// Pre-build filesystem transaction map to find other runs containing transaction IDs.
	// This only runs ONCE per SyncRunDetails request.
	fsTxMap := make(map[string][]*apiproto.SyncRunInfoForTx)
	if entries, err := os.ReadDir(logsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			otherCid := entry.Name()
			if otherCid == correlationID {
				continue
			}

			otherInfo, err := entry.Info()
			if err != nil {
				continue
			}
			otherTimestamp := otherInfo.ModTime().Format(time.RFC3339)

			otherRunDir := filepath.Join(logsDir, otherCid)
			otherRunFiles, err := os.ReadDir(otherRunDir)
			if err != nil {
				continue
			}

			var otherRespFiles []string
			for _, f := range otherRunFiles {
				if !f.IsDir() && strings.HasSuffix(f.Name(), "_resp.json") {
					otherRespFiles = append(otherRespFiles, filepath.Join(otherRunDir, f.Name()))
				}
			}
			if len(otherRespFiles) == 0 {
				continue
			}

			otherST := ""
			firstOtherFile := filepath.Base(otherRespFiles[0])
			if strings.Contains(firstOtherFile, "gocardless") {
				otherST = "GOCARDLESS"
			} else if strings.Contains(firstOtherFile, "enablebanking") {
				otherST = "ENABLEBANKING"
			} else if strings.Contains(firstOtherFile, "trading212") {
				otherST = "TRADING212"
			}
			if otherST == "" {
				continue
			}

			otherIntegrationName := ""
			var otherCandidateInts []userIntegration
			for _, ui := range userInts {
				if ui.ServiceType == otherST {
					otherCandidateInts = append(otherCandidateInts, ui)
				}
			}
			if len(otherCandidateInts) == 1 {
				otherIntegrationName = otherCandidateInts[0].Name
			} else if len(otherCandidateInts) > 1 {
				if content, err := os.ReadFile(otherRespFiles[0]); err == nil {
					contentStr := string(content)
					for i := range otherCandidateInts {
						ui := &otherCandidateInts[i]
						if ui.ReqID != "" && strings.Contains(contentStr, ui.ReqID) {
							otherIntegrationName = ui.Name
							break
						}
						for _, accID := range ui.AccountIDs {
							if accID != "" && strings.Contains(contentStr, accID) {
								otherIntegrationName = ui.Name
								break
							}
						}
					}
				}
				if otherIntegrationName == "" {
					otherIntegrationName = otherCandidateInts[0].Name
				}
			}

			// Parse response files in the folder to find transaction IDs
			for _, rf := range otherRespFiles {
				if content, err := os.ReadFile(rf); err == nil {
					var dump struct {
						Body interface{} `json:"body"`
					}
					if err := json.Unmarshal(content, &dump); err == nil && dump.Body != nil {
						detected := parseTransactionsFromJSON(strings.ToLower(otherST), dump.Body)
						for _, t := range detected {
							if t.ExternalId != "" {
								fsTxMap[t.ExternalId] = append(fsTxMap[t.ExternalId], &apiproto.SyncRunInfoForTx{
									CorrelationId:   otherCid,
									Timestamp:       otherTimestamp,
									IntegrationName: otherIntegrationName,
								})
							}
						}
					}
				}
			}
		}
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
					// Lookup pre-built filesystem matches in O(1) time
					otherRuns := fsTxMap[tx.ExternalId]

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


