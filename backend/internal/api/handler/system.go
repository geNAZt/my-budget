package handler

import (
	"container/list"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/genazt/my-budget-script/backend/internal/api"
	apiproto "github.com/genazt/my-budget-script/backend/internal/api/proto"
	"github.com/genazt/my-budget-script/backend/internal/domain"
	"github.com/genazt/my-budget-script/backend/internal/service"
	"github.com/google/uuid"
)

type cacheEntry struct {
	key       string
	value     []*apiproto.SyncRun
	expiresAt time.Time
}

type LRUCache struct {
	capacity  int
	evictList *list.List
	items     map[string]*list.Element
	mu        sync.Mutex
	ttl       time.Duration
}

func NewLRUCache(capacity int, ttl time.Duration) *LRUCache {
	return &LRUCache{
		capacity:  capacity,
		evictList: list.New(),
		items:     make(map[string]*list.Element),
		ttl:       ttl,
	}
}

func (c *LRUCache) Get(key string) ([]*apiproto.SyncRun, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, exists := c.items[key]
	if !exists {
		return nil, false
	}

	entry := element.Value.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.removeElement(element)
		return nil, false
	}

	c.evictList.MoveToFront(element)
	return entry.value, true
}

func (c *LRUCache) Add(key string, value []*apiproto.SyncRun) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if element, exists := c.items[key]; exists {
		c.evictList.MoveToFront(element)
		entry := element.Value.(*cacheEntry)
		entry.value = value
		entry.expiresAt = time.Now().Add(c.ttl)
		return
	}

	entry := &cacheEntry{
		key:       key,
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
	element := c.evictList.PushFront(entry)
	c.items[key] = element

	if c.evictList.Len() > c.capacity {
		c.removeOldest()
	}
}

func (c *LRUCache) UpdateTransactionCount(key string, index int, txCount int32, status string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	element, exists := c.items[key]
	if !exists {
		return
	}

	entry := element.Value.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		return
	}

	if index >= 0 && index < len(entry.value) {
		entry.value[index].TransactionCount = txCount
		entry.value[index].Status = status
	}
}

func (c *LRUCache) removeOldest() {
	element := c.evictList.Back()
	if element != nil {
		c.removeElement(element)
	}
}

func (c *LRUCache) removeElement(element *list.Element) {
	c.evictList.Remove(element)
	entry := element.Value.(*cacheEntry)
	delete(c.items, entry.key)
}

type System struct {
	handler       *api.WebSocketHandler
	logService    *service.LogService
	dockerService *service.DockerService
	syncService   *service.SyncService
	db            *sql.DB

	backendVersion   string
	watchtowerStatus string
	statusMu         sync.RWMutex

	runsCache *LRUCache
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
		runsCache:        NewLRUCache(100, 3*time.Hour),
	}

	go sys.startWatchtowerMonitor()
	go sys.startBackgroundSyncRunsParser()

	return sys
}

func (sys *System) startBackgroundSyncRunsParser() {
	// Add some delay on boot to allow DB migrations to fully complete and startup log noise to settle
	time.Sleep(5 * time.Second)
	log.Printf("[DB] Starting background parser for historical sync runs...")

	baseDir := "/app"
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		if _, err := os.Stat("web"); err == nil {
			baseDir = "web"
		} else {
			baseDir = "."
		}
	}
	logsDir := filepath.Join(baseDir, "logs", "sync_runs")

	rows, err := sys.db.Query("SELECT correlation_id, service_type FROM sync_runs WHERE transaction_count = -1")
	if err != nil {
		log.Printf("[DB] Background parser error querying runs: %v", err)
		return
	}
	defer rows.Close()

	type runTask struct {
		cid   string
		stype string
	}
	var tasks []runTask
	for rows.Next() {
		var t runTask
		if err := rows.Scan(&t.cid, &t.stype); err == nil {
			tasks = append(tasks, t)
		}
	}

	if len(tasks) == 0 {
		log.Printf("[DB] Background parser: no historical sync runs to parse.")
		return
	}

	log.Printf("[DB] Background parser found %d historical sync runs to parse.", len(tasks))

	// Queue up tasks
	taskChan := make(chan runTask, len(tasks))
	for _, t := range tasks {
		taskChan <- t
	}
	close(taskChan)

	numWorkers := 4
	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				runDir := filepath.Join(logsDir, task.cid)
				runFiles, err := os.ReadDir(runDir)
				if err != nil {
					// Directory does not exist on disk, transaction count is 0
					_, _ = sys.db.Exec("UPDATE sync_runs SET status = 'COMPLETED', transaction_count = 0 WHERE correlation_id = $1", task.cid)
					continue
				}

				txCount := int32(0)
				for _, f := range runFiles {
					if f.IsDir() || !strings.HasSuffix(f.Name(), "_resp.json") {
						continue
					}
					content, err := os.ReadFile(filepath.Join(runDir, f.Name()))
					if err == nil {
						var dump struct {
							Body interface{} `json:"body"`
						}
						if err := json.Unmarshal(content, &dump); err == nil && dump.Body != nil {
							detected := parseTransactionsFromJSON(strings.ToLower(task.stype), dump.Body)
							txCount += int32(len(detected))
						}
					}
				}

				_, _ = sys.db.Exec("UPDATE sync_runs SET status = 'COMPLETED', transaction_count = $1 WHERE correlation_id = $2", txCount, task.cid)
			}
		}()
	}
	wg.Wait()
	log.Printf("[DB] Background parser successfully finished parsing all historical sync runs.")
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

type syncMetrics struct {
	mu           sync.Mutex
	ioBytes      int64
	ioOps        int64
	ioDurationNs int64
}

// Helper to get CPU time and Memory RSS
func getCPUTimeAndMemory() (time.Duration, uint64) {
	var rusage syscall.Rusage
	var cpuTime time.Duration
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &rusage); err == nil {
		userSec := rusage.Utime.Sec
		userUsec := rusage.Utime.Usec
		sysSec := rusage.Stime.Sec
		sysUsec := rusage.Stime.Usec
		cpuTime = time.Duration(userSec+sysSec)*time.Second + time.Duration(userUsec+sysUsec)*time.Microsecond
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	return cpuTime, memStats.Sys
}

// SyncRuns lists all sync runs (logs) for the authenticated user
func (sys *System) SyncRuns(s *api.WebsocketSession, reqID string, req *apiproto.SyncRunsRequest) {
	userID, _ := s.GetAuth()
	if userID == "" {
		sys.handler.SendError(s, reqID, http.StatusUnauthorized, "Unauthorized")
		return
	}

	offset := int(req.Offset)
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 50
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


	startTime := time.Now()
	startCPUTime, _ := getCPUTimeAndMemory()

	metrics := &syncMetrics{}
	doneChan := make(chan struct{})

	// Start 1-second metrics collector loop immediately
	go func() {
		metricsTicker := time.NewTicker(1 * time.Second)
		defer metricsTicker.Stop()

		lastCPU, _ := getCPUTimeAndMemory()
		lastTime := time.Now()

		for {
			select {
			case <-metricsTicker.C:
				now := time.Now()
				elapsed := now.Sub(lastTime)
				if elapsed <= 0 {
					continue
				}

				currentCPU, currentRSS := getCPUTimeAndMemory()
				cpuDelta := currentCPU - lastCPU

				numCores := runtime.NumCPU()
				cpuPercent := (float64(cpuDelta) / float64(elapsed)) * 100.0 / float64(numCores)
				if cpuPercent > 100.0 {
					cpuPercent = 100.0
				}
				if cpuPercent < 0 {
					cpuPercent = 0
				}

				lastCPU = currentCPU
				lastTime = now

				metrics.mu.Lock()
				ioOps := metrics.ioOps
				ioBytes := metrics.ioBytes
				ioDurationMs := metrics.ioDurationNs / int64(time.Millisecond)
				metrics.mu.Unlock()

				metricMsg := &apiproto.SyncRun{
					IsMetrics: true,
					Metrics: &apiproto.SyncPerformanceMetrics{
						FileIoReadBytes:      ioBytes,
						FileIoReadOperations: int32(ioOps),
						CpuUtilization:       cpuPercent,
						MemoryRssBytes:       currentRSS,
						TotalIoDurationMs:    ioDurationMs,
					},
				}

				if s.IsClosed() {
					return
				}
				_ = sys.handler.SendResponse(s, reqID, metricMsg, false)

			case <-doneChan:
				return
			}
		}
	}()

	readFileWithMetrics := func(filePath string) ([]byte, error) {
		start := time.Now()
		content, err := os.ReadFile(filePath)
		duration := time.Since(start)

		metrics.mu.Lock()
		metrics.ioOps++
		if err == nil {
			metrics.ioBytes += int64(len(content))
		}
		metrics.ioDurationNs += duration.Nanoseconds()
		metrics.mu.Unlock()

		return content, err
	}

	readDirWithMetrics := func(dirPath string) ([]os.DirEntry, error) {
		start := time.Now()
		entries, err := os.ReadDir(dirPath)
		duration := time.Since(start)

		metrics.mu.Lock()
		metrics.ioOps++
		metrics.ioDurationNs += duration.Nanoseconds()
		metrics.mu.Unlock()

		return entries, err
	}

	type folderTask struct {
		index           int
		correlationID   string
		runDir          string
		timestampStr    string
		respFiles       []string
		integrationID   string
		integrationName string
		serviceType     string
		isEmpty         bool
		reqID           string
	}

	// 1. Build the dynamic SQL query with filters
	baseQuery := `
		FROM sync_runs
		WHERE user_id = $1
	`
	args := []interface{}{userID}
	argIdx := 2

	if req.FilterIntegrationId != "" && req.FilterIntegrationId != "ALL" {
		baseQuery += fmt.Sprintf(" AND integration_id = $%d", argIdx)
		args = append(args, req.FilterIntegrationId)
		argIdx++
	}

	if req.FilterTxsValue != nil {
		op := ">="
		switch req.FilterTxsOperator {
		case ">", "<", "=", ">=", "<=":
			op = req.FilterTxsOperator
		}
		baseQuery += fmt.Sprintf(" AND transaction_count %s $%d", op, argIdx)
		args = append(args, *req.FilterTxsValue)
		argIdx++
	}

	// First query the total count of matching runs
	var totalItems int
	countQuery := fmt.Sprintf("SELECT COUNT(*) %s", baseQuery)
	err := sys.db.QueryRow(countQuery, args...).Scan(&totalItems)
	if err != nil {
		close(doneChan)
		if !s.IsClosed() {
			sys.handler.SendResponse(s, reqID, nil, true)
		}
		return
	}

	// Query the current page
	query := "SELECT correlation_id, integration_id, integration_name, service_type, status, timestamp, transaction_count, has_log_files, error_message " +
		baseQuery + fmt.Sprintf(" ORDER BY timestamp DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	pageArgs := append(args, limit, offset)
	rows, err := sys.db.Query(query, pageArgs...)
	if err != nil {
		close(doneChan)
		if !s.IsClosed() {
			sys.handler.SendResponse(s, reqID, nil, true)
		}
		return
	}
	defer rows.Close()

	var pageRuns []*apiproto.SyncRun
	for rows.Next() {
		var r apiproto.SyncRun
		var t time.Time
		var errMsg string
		err := rows.Scan(&r.CorrelationId, &r.IntegrationId, &r.IntegrationName, &r.ServiceType, &r.Status, &t, &r.TransactionCount, &r.HasLogFiles, &errMsg)
		if err != nil {
			continue
		}
		r.Timestamp = t.Format(time.RFC3339)
		pageRuns = append(pageRuns, &r)
	}

	// 2. Emit the current page immediately
	for _, run := range pageRuns {
		if s.IsClosed() || s.IsRequestCancelled(reqID) {
			close(doneChan)
			return
		}
		_ = sys.handler.SendResponse(s, reqID, run, false)
	}

	// 3. Build tasks list for workers (any items in the current page that are still not parsed yet)
	var tasks []folderTask
	for idx, run := range pageRuns {
		if s.IsRequestCancelled(reqID) {
			close(doneChan)
			return
		}
		if run.TransactionCount == -1 {
			runDir := filepath.Join(logsDir, run.CorrelationId)
			runFiles, _ := readDirWithMetrics(runDir)
			var respFiles []string
			for _, f := range runFiles {
				if !f.IsDir() && strings.HasSuffix(f.Name(), "_resp.json") {
					respFiles = append(respFiles, filepath.Join(runDir, f.Name()))
				}
			}

			tasks = append(tasks, folderTask{
				index:           idx,
				correlationID:   run.CorrelationId,
				runDir:          runDir,
				timestampStr:    run.Timestamp,
				respFiles:       respFiles,
				integrationID:   run.IntegrationId,
				integrationName: run.IntegrationName,
				serviceType:     run.ServiceType,
				isEmpty:         !run.HasLogFiles,
				reqID:           reqID,
			})
		}
	}

	// 4. Process tasks with workers
	if len(tasks) > 0 {
		numWorkers := 4
		if len(tasks) < numWorkers {
			numWorkers = len(tasks)
		}

		taskChan := make(chan folderTask, len(tasks))
		for _, t := range tasks {
			taskChan <- t
		}
		close(taskChan)

		var wg sync.WaitGroup
		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for task := range taskChan {
					if s.IsClosed() || s.IsRequestCancelled(task.reqID) {
						return
					}
					txCount := int32(0)
					for _, rf := range task.respFiles {
						if content, err := readFileWithMetrics(rf); err == nil {
							var dump struct {
								Body interface{} `json:"body"`
							}
							if err := json.Unmarshal(content, &dump); err == nil && dump.Body != nil {
								detected := parseTransactionsFromJSON(strings.ToLower(task.serviceType), dump.Body)
								txCount += int32(len(detected))
							}
						}
					}

					// Update DB
					_, _ = sys.db.Exec(`
						UPDATE sync_runs
						SET status = $1, transaction_count = $2
						WHERE correlation_id = $3
					`, "COMPLETED", txCount, task.correlationID)

					// Emit updated run to client
					updatedRun := &apiproto.SyncRun{
						CorrelationId:    task.correlationID,
						Status:           "COMPLETED",
						TransactionCount: txCount,
					}

					if s.IsClosed() || s.IsRequestCancelled(task.reqID) {
						return
					}
					_ = sys.handler.SendResponse(s, reqID, updatedRun, false)
				}
			}()
		}
		wg.Wait()
	}

	// Send final metrics packet
	if !s.IsClosed() {
		currentCPU, currentRSS := getCPUTimeAndMemory()
		elapsed := time.Since(startTime)
		cpuPercent := 0.0
		if elapsed > 0 {
			cpuDelta := currentCPU - startCPUTime
			numCores := runtime.NumCPU()
			cpuPercent = (float64(cpuDelta) / float64(elapsed)) * 100.0 / float64(numCores)
			if cpuPercent > 100.0 {
				cpuPercent = 100.0
			}
			if cpuPercent < 0 {
				cpuPercent = 0
			}
		}

		metrics.mu.Lock()
		ioOps := metrics.ioOps
		ioBytes := metrics.ioBytes
		ioDurationMs := metrics.ioDurationNs / int64(time.Millisecond)
		metrics.mu.Unlock()

		finalMetricMsg := &apiproto.SyncRun{
			IsMetrics: true,
			Metrics: &apiproto.SyncPerformanceMetrics{
				FileIoReadBytes:      ioBytes,
				FileIoReadOperations: int32(ioOps),
				CpuUtilization:       cpuPercent,
				MemoryRssBytes:       currentRSS,
				TotalIoDurationMs:    ioDurationMs,
			},
		}
		_ = sys.handler.SendResponse(s, reqID, finalMetricMsg, false)
	}

	close(doneChan)

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

	timestamp := ""
	if info, err := os.Stat(runDir); err == nil {
		timestamp = info.ModTime().Format(time.RFC3339)
	} else if os.IsNotExist(err) {
		var dbStatus, dbIntegrationID, dbIntegrationName, dbServiceType, dbErrorMessage string
		var dbTimestamp time.Time
		err := sys.db.QueryRow(`
			SELECT status, integration_id, integration_name, service_type, timestamp, error_message
			FROM sync_runs
			WHERE correlation_id = $1 AND user_id = $2
		`, correlationID, userID).Scan(&dbStatus, &dbIntegrationID, &dbIntegrationName, &dbServiceType, &dbTimestamp, &dbErrorMessage)

		if err != nil {
			sys.handler.SendError(s, reqID, http.StatusNotFound, "Sync run details not found")
			return
		}

		content := fmt.Sprintf("Sync Status: %s\n", dbStatus)
		if dbErrorMessage != "" {
			content += fmt.Sprintf("Reason: %s\n", dbErrorMessage)
		} else {
			content += "Reason: No new transactions discovered.\n"
		}
		content += "\nEmpty run logs were cleaned up immediately to save disk space.\n"

		resp := &apiproto.SyncRunDetailsResponse{
			CorrelationId:   correlationID,
			IntegrationId:   dbIntegrationID,
			IntegrationName: dbIntegrationName,
			ServiceType:     dbServiceType,
			Timestamp:       dbTimestamp.Format(time.RFC3339),
			RawLogs: []*apiproto.SyncRawLogFile{
				{
					Filename:  "status.txt",
					Content:   content,
					IsRequest: false,
				},
			},
		}
		_ = sys.handler.SendResponse(s, reqID, resp, true)
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


