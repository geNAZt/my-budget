package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/genazt/my-budget-script/web/backend/internal/bus"
	"github.com/genazt/my-budget-script/web/backend/internal/crypto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	executionpb "github.com/genazt/my-budget-script/web/backend/internal/service/proto"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

type ActiveExecutionContext struct {
	UserID   string
	Secrets  map[string]string
	StateMap map[string]interface{}
}

type ExecutionService struct {
	planRepo        *repository.ExecutionRepository
	connRepo        *repository.ConnectionRepository
	userRepo        *repository.UserRepository
	incomeRepo      *repository.IncomeRepository
	assetRepo       *repository.AssetRepository
	loanRepo        *repository.LoanRepository
	cryptoService   *crypto.CryptoService
	integrationRepo *repository.IntegrationRepository
	eventBus        *bus.Bus
	mu              sync.Mutex

	// In-memory cache for plan codes compiled (maps planID -> MD5 hash of code)
	compiledPlans map[string]string
	compiledMu    sync.RWMutex

	// Persistent subprocess handles
	runnerCmd    *exec.Cmd
	runnerStdin  io.WriteCloser
	runnerStdout io.ReadCloser
	stdinMu      sync.Mutex // protects concurrent writing to stdin Pipe

	// Pending Request Multiplexer Registry
	pendingMu       sync.Mutex
	pendingRequests map[string]chan *executionpb.StdioFrame

	syncService  *SyncService
	activeRuns   map[string]string // maps execution token -> userID
	activeRunsMu sync.RWMutex

	activeContexts   map[string]*ActiveExecutionContext
	activeContextsMu sync.RWMutex
}

func NewExecutionService(
	planRepo *repository.ExecutionRepository,
	connRepo *repository.ConnectionRepository,
	userRepo *repository.UserRepository,
	incomeRepo *repository.IncomeRepository,
	assetRepo *repository.AssetRepository,
	loanRepo *repository.LoanRepository,
	cryptoService *crypto.CryptoService,
	eventBus *bus.Bus,
) *ExecutionService {
	s := &ExecutionService{
		planRepo:        planRepo,
		connRepo:        connRepo,
		userRepo:        userRepo,
		incomeRepo:      incomeRepo,
		assetRepo:       assetRepo,
		loanRepo:        loanRepo,
		cryptoService:   cryptoService,
		eventBus:        eventBus,
		compiledPlans:   make(map[string]string),
		activeRuns:      make(map[string]string),
		pendingRequests: make(map[string]chan *executionpb.StdioFrame),
		activeContexts:  make(map[string]*ActiveExecutionContext),
	}

	// Proactively fire the persistent subprocess and setup streams
	go s.StartRunner()

	// Register event handlers
	s.SubscribeToEvents()

	return s
}

func (s *ExecutionService) SubscribeToEvents() {
	s.eventBus.Subscribe(bus.TopicSyncFinished, func(ctx context.Context, e bus.Event) {
		payload := e.Payload.(bus.SyncFinishedPayload)
		s.TriggerPlansForSyncFinished(payload.UserID, payload.IntegrationID, payload.IntegrationName, payload.ServiceType, payload.DiscoveredCount)
	})

	s.eventBus.Subscribe(bus.TopicTransactionDiscovered, func(ctx context.Context, e bus.Event) {
		payload := e.Payload.(bus.TransactionDiscoveredPayload)
		s.TriggerPlansForTransaction(payload.UserID, payload.Tx, payload.Amount, payload.Receiver, payload.Description)
	})
}

func (s *ExecutionService) SetIntegrationRepo(repo *repository.IntegrationRepository) {
	s.integrationRepo = repo
}

func (s *ExecutionService) SetSyncService(syncService *SyncService) {
	s.syncService = syncService
}

func (s *ExecutionService) ValidateExecutionToken(token string) (string, bool) {
	s.activeRunsMu.RLock()
	defer s.activeRunsMu.RUnlock()
	userID, exists := s.activeRuns[token]
	return userID, exists
}

func (s *ExecutionService) SyncIntegration(userID string, integrationID string) error {
	if s.syncService == nil {
		return fmt.Errorf("sync service not configured in execution service")
	}
	return s.syncService.SyncIntegration(userID, integrationID, true)
}

func (s *ExecutionService) GetMasterKeyForUser(user *domain.User, integrationID string) ([]byte, error) {
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
	return nil, fmt.Errorf("failed to derive master integration key")
}

func (s *ExecutionService) DecryptConnectionValue(userID string, conn *domain.Connection) (string, error) {
	user, _ := s.userRepo.GetUserByID(userID)
	if user == nil {
		return "", fmt.Errorf("user not found")
	}

	for _, auth := range user.Authenticators {
		wrappedB64, err := s.connRepo.GetKeySlot(conn.ID, auth.ID)
		if err == nil && wrappedB64 != "" {
			wrapped, _ := base64.StdEncoding.DecodeString(wrappedB64)
			ik, _ := s.cryptoService.DeriveIdentityKey(auth.PublicKey)
			connKey, err := s.cryptoService.UnwrapKey(ik, wrapped)
			if err == nil {
				ciphertext, _ := base64.StdEncoding.DecodeString(conn.Value)
				decrypted, err := s.cryptoService.Decrypt(connKey, ciphertext)
				if err == nil {
					return string(decrypted), nil
				}
			}
		}
	}

	return "", fmt.Errorf("failed to decrypt connection")
}

// Subprocess Management Lifecycle (Auto-Restarting Daemon)
func (s *ExecutionService) StartRunner() {
	for {
		log.Printf("[ExecutionEngine] Spawning persistent Node.js sandbox runner daemon...")

		cmd := exec.Command("node", "runner.js")
		dir := "/app/execution_engine"
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if _, err := os.Stat("execution_engine"); err == nil {
				dir = "execution_engine"
			} else if _, err := os.Stat("web/backend/execution_engine"); err == nil {
				dir = "web/backend/execution_engine"
			} else if _, err := os.Stat("./execution_engine"); err == nil {
				dir = "./execution_engine"
			} else if envDir := os.Getenv("EXECUTION_ENGINE_DIR"); envDir != "" {
				dir = envDir
			}
		}
		cmd.Dir = dir

		stdin, err := cmd.StdinPipe()
		if err != nil {
			log.Printf("[ExecutionEngine] Failed to create stdin pipe: %v. Retrying in 2s...", err)
			time.Sleep(2 * time.Second)
			continue
		}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("[ExecutionEngine] Failed to create stdout pipe: %v. Retrying in 2s...", err)
			time.Sleep(2 * time.Second)
			continue
		}

		cmd.Stderr = os.Stderr

		if err := cmd.Start(); err != nil {
			log.Printf("[ExecutionEngine] Failed to start subprocess: %v. Retrying in 2s...", err)
			time.Sleep(2 * time.Second)
			continue
		}

		s.mu.Lock()
		s.runnerCmd = cmd
		s.runnerStdin = stdin
		s.runnerStdout = stdout
		s.mu.Unlock()

		log.Printf("[ExecutionEngine] Persistent Node.js sandbox runner daemon successfully active.")

		readDone := make(chan struct{})
		go s.readLoop(readDone)

		// Wait for process to exit (self-healing restart trigger)
		_ = cmd.Wait()
		log.Printf("[ExecutionEngine] Subprocess exited. Cleaning up streams...")

		close(readDone)

		s.mu.Lock()
		if s.runnerStdin != nil {
			s.runnerStdin.Close()
		}
		if s.runnerStdout != nil {
			s.runnerStdout.Close()
		}
		s.runnerCmd = nil
		s.runnerStdin = nil
		s.runnerStdout = nil
		s.mu.Unlock()

		// Clean up and notify all pending requests that the runner daemon exited
		s.pendingMu.Lock()
		for cid, ch := range s.pendingRequests {
			ch <- &executionpb.StdioFrame{
				Message: &executionpb.StdioFrame_ExecuteResponse{
					ExecuteResponse: &executionpb.ExecuteResponse{
						CorrelationId: cid,
						Success:       false,
						Stderr:        "[ExecutionEngine Error] Sandbox runner daemon exited unexpectedly during run.",
						ExitCode:      1,
					},
				},
			}
			delete(s.pendingRequests, cid)
		}
		s.pendingMu.Unlock()

		time.Sleep(1 * time.Second)
	}
}

func (s *ExecutionService) readLoop(readDone chan struct{}) {
	for {
		select {
		case <-readDone:
			return
		default:
			payload, err := s.readFrame()
			if err != nil {
				return
			}

			frame := &executionpb.StdioFrame{}
			if err := proto.Unmarshal(payload, frame); err != nil {
				log.Printf("[ExecutionEngine Error] Failed to unmarshal incoming Protobuf frame: %v", err)
				continue
			}

			if frame.GetRpcRequest() != nil {
				go s.handleRpcRequest(frame.GetRpcRequest())
				continue
			}

			// Route response back to the blocked request thread matching correlationID or msg_id
			s.pendingMu.Lock()
			var targetID string
			if resp := frame.GetRpcResponse(); resp != nil {
				targetID = resp.MsgId
			} else if resp := frame.GetExecuteResponse(); resp != nil {
				targetID = resp.CorrelationId
			}

			ch, exists := s.pendingRequests[targetID]
			if exists {
				ch <- frame
				delete(s.pendingRequests, targetID)
			}
			s.pendingMu.Unlock()
		}
	}
}

func (s *ExecutionService) handleRpcRequest(req *executionpb.RpcRequest) {
	s.activeContextsMu.RLock()
	ctx, exists := s.activeContexts[req.CorrelationId]
	s.activeContextsMu.RUnlock()

	var result interface{}
	var errStr string

	if !exists {
		errStr = "active run execution context not found"
	} else {
		switch req.Method {
		case "sync":
			var params struct {
				IntegrationID string `json:"integration_id"`
			}
			if err := json.Unmarshal(req.ParamsJson, &params); err == nil {
				if s.syncService != nil {
					errSync := s.syncService.SyncIntegration(ctx.UserID, params.IntegrationID, true)
					if errSync != nil {
						errStr = fmt.Sprintf("sync failed: %v", errSync)
					} else {
						// Reload realtime accounts in ctx.StateMap
						if s.integrationRepo != nil {
							integrations, err := s.integrationRepo.List(ctx.UserID)
							if err == nil {
								realtimeAccounts := make(map[string]interface{})
								for _, integration := range integrations {
									decrypted, err := s.syncService.DecryptIntegrationConfig(ctx.UserID, &integration)
									if err != nil {
										continue
									}

									var config struct {
										AccountIDs       []string                       `json:"account_ids"`
										LegacyAccountIDs []string                       `json:"accounts"`
										AccountsMetadata map[string]struct {
											Alias             string     `json:"alias"`
											Enabled           bool       `json:"enabled"`
											IBAN              string     `json:"iban"`
											BIC               string     `json:"bic"`
											ReferenceCodes    string     `json:"reference_codes"`
											Tags              string     `json:"tags"`
											BackoffUntil      *time.Time `json:"backoff_until"`
											LastSyncedAt      *time.Time `json:"last_synced_at"`
											MetadataCheckedAt *time.Time `json:"metadata_checked_at"`
											Balance           float64    `json:"balance"`
										} `json:"accounts_metadata"`
									}

									if err := json.Unmarshal(decrypted, &config); err == nil {
										accIDs := config.AccountIDs
										if len(accIDs) == 0 {
											accIDs = config.LegacyAccountIDs
										}
										for _, accID := range accIDs {
											meta, ok := config.AccountsMetadata[accID]
											if !ok {
												continue
											}
											accMap := make(map[string]interface{})
											accMap["id"] = accID
											if meta.Alias != "" {
												accMap["name"] = meta.Alias
											} else {
												accMap["name"] = accID
											}
											accMap["alias"] = meta.Alias
											accMap["currency"] = ""
											accMap["integration_name"] = integration.Name
											accMap["service_type"] = integration.ServiceType
											accMap["amount"] = meta.Balance
											accMap["iban"] = meta.IBAN
											accMap["integration_id"] = integration.ID

											realtimeAccounts[accID] = accMap
											if meta.Alias != "" {
												realtimeAccounts[meta.Alias] = accMap
											}
										}
									}
								}
								ctx.StateMap["realtime_accounts"] = realtimeAccounts
							}
						}
						result = map[string]bool{"synced": true}
					}
				} else {
					errStr = "sync service not initialized in backend"
				}
			} else {
				errStr = "invalid params for sync"
			}

		case "get_secret":
			var params struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(req.ParamsJson, &params); err == nil {
				if val, ok := ctx.Secrets[params.Key]; ok {
					result = map[string]string{"value": val}
				} else {
					errStr = fmt.Sprintf("secret key '%s' not found", params.Key)
				}
			} else {
				errStr = "invalid params for get_secret"
			}

		case "get_realtime_account":
			var params struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(req.ParamsJson, &params); err == nil {
				if realtimeAccounts, ok := ctx.StateMap["realtime_accounts"].(map[string]interface{}); ok {
					if acc, ok := realtimeAccounts[params.Key]; ok {
						result = acc
					} else {
						errStr = fmt.Sprintf("realtime account '%s' not found", params.Key)
					}
				} else {
					errStr = "realtime accounts not initialized"
				}
			} else {
				errStr = "invalid params for get_realtime_account"
			}

		case "get_income":
			var params struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(req.ParamsJson, &params); err == nil {
				var found interface{}
				if incomes, ok := ctx.StateMap["incomes"].([]domain.Income); ok {
					for _, inc := range incomes {
						if inc.Name == params.Name {
							found = inc
							break
						}
					}
				} else if incomes, ok := ctx.StateMap["incomes"].([]*domain.Income); ok {
					for _, inc := range incomes {
						if inc != nil && inc.Name == params.Name {
							found = inc
							break
						}
					}
				}
				if found != nil {
					result = found
				} else {
					errStr = fmt.Sprintf("income '%s' not found", params.Name)
				}
			} else {
				errStr = "invalid params for get_income"
			}

		case "get_asset":
			var params struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(req.ParamsJson, &params); err == nil {
				var found interface{}
				if assets, ok := ctx.StateMap["assets"].([]domain.Asset); ok {
					for _, asset := range assets {
						if asset.Name == params.Name {
							found = asset
							break
						}
					}
				} else if assets, ok := ctx.StateMap["assets"].([]*domain.Asset); ok {
					for _, asset := range assets {
						if asset != nil && asset.Name == params.Name {
							found = asset
							break
						}
					}
				}
				if found != nil {
					result = found
				} else {
					errStr = fmt.Sprintf("asset '%s' not found", params.Name)
				}
			} else {
				errStr = "invalid params for get_asset"
			}

		case "get_loan":
			var params struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(req.ParamsJson, &params); err == nil {
				var found interface{}
				if loans, ok := ctx.StateMap["loans"].([]domain.Loan); ok {
					for _, loan := range loans {
						if loan.Name == params.Name {
							found = loan
							break
						}
					}
				} else if loans, ok := ctx.StateMap["loans"].([]*domain.Loan); ok {
					for _, loan := range loans {
						if loan != nil && loan.Name == params.Name {
							found = loan
							break
						}
					}
				}
				if found != nil {
					result = found
				} else {
					errStr = fmt.Sprintf("loan '%s' not found", params.Name)
				}
			} else {
				errStr = "invalid params for get_loan"
			}

		default:
			errStr = fmt.Sprintf("unknown RPC method '%s'", req.Method)
		}
	}

	var resultBytes []byte
	if result != nil {
		resultBytes, _ = json.Marshal(result)
	}

	frame := &executionpb.StdioFrame{
		Message: &executionpb.StdioFrame_RpcResponse{
			RpcResponse: &executionpb.RpcResponse{
				CorrelationId: req.CorrelationId,
				MsgId:         req.MsgId,
				Success:       errStr == "",
				ResultJson:    resultBytes,
				Error:         errStr,
			},
		},
	}
	respBytes, _ := proto.Marshal(frame)
	_ = s.writeFrame(respBytes)
}

// Length-Prefixed Framing Stream IO Helpers
func (s *ExecutionService) writeFrame(payload []byte) error {
	s.stdinMu.Lock()
	defer s.stdinMu.Unlock()

	s.mu.Lock()
	stdin := s.runnerStdin
	s.mu.Unlock()

	if stdin == nil {
		return fmt.Errorf("runner stdin is not active")
	}

	length := uint32(len(payload))
	lengthBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBytes, length)

	if _, err := stdin.Write(lengthBytes); err != nil {
		return err
	}
	if _, err := stdin.Write(payload); err != nil {
		return err
	}
	return nil
}

func (s *ExecutionService) readFrame() ([]byte, error) {
	s.mu.Lock()
	stdout := s.runnerStdout
	s.mu.Unlock()

	if stdout == nil {
		return nil, fmt.Errorf("runner stdout is not active")
	}

	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(stdout, lengthBytes); err != nil {
		return nil, err
	}

	length := binary.BigEndian.Uint32(lengthBytes)
	payload := make([]byte, length)
	if _, err := io.ReadFull(stdout, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// Multiplexed VM Rule Execution
func (s *ExecutionService) ExecutePlan(userID string, planID string, triggerPayload map[string]interface{}) error {
	// Generate a secure one-off execution token for this run
	tokenBytes, _ := s.cryptoService.GenerateRandomKey()
	token := hex.EncodeToString(tokenBytes)

	s.activeRunsMu.Lock()
	s.activeRuns[token] = userID
	s.activeRunsMu.Unlock()

	defer func() {
		s.activeRunsMu.Lock()
		delete(s.activeRuns, token)
		s.activeRunsMu.Unlock()
	}()

	user, err := s.userRepo.GetUserByID(userID)
	if err != nil || user == nil {
		return fmt.Errorf("user not found: %w", err)
	}

	logEntry := domain.ExecutionLog{
		UserID:    userID,
		PlanID:    planID,
		StartedAt: time.Now(),
		Status:    "PENDING",
	}

	plan, err := s.planRepo.GetByID(userID, planID)
	if err != nil {
		logEntry.Status = "FAILED"
		logEntry.ExitCode = 1
		logEntry.Stderr = fmt.Sprintf("[ExecutionEngine] Plan not found: %v", err)
		_ = s.planRepo.LogExecution(&logEntry)
		return fmt.Errorf("plan not found: %w", err)
	}

	// 1. Compile state Map (to be stored in context and queried on-demand)
	stateMap := make(map[string]interface{})
	incomes, _ := s.incomeRepo.List(userID)
	stateMap["incomes"] = incomes

	assets, _ := s.assetRepo.List(userID)
	stateMap["assets"] = assets

	loans, _ := s.loanRepo.List(userID)
	stateMap["loans"] = loans

	stateMap["execution_token"] = token
	stateMap["api_base"] = "http://localhost:8080" // local sandbox API route

	// Enrich stateMap with accounts
	realtimeAccounts := make(map[string]interface{})
	if s.integrationRepo != nil {
		integrations, err := s.integrationRepo.List(userID)
		if err == nil {
			for _, integration := range integrations {
				decrypted, err := s.syncService.DecryptIntegrationConfig(userID, &integration)
				if err != nil {
					continue
				}

				var config struct {
					AccountIDs       []string                       `json:"account_ids"`
					LegacyAccountIDs []string                       `json:"accounts"`
					AccountsMetadata map[string]struct {
						Alias             string     `json:"alias"`
						Enabled           bool       `json:"enabled"`
						IBAN              string     `json:"iban"`
						BIC               string     `json:"bic"`
						ReferenceCodes    string     `json:"reference_codes"`
						Tags              string     `json:"tags"`
						BackoffUntil      *time.Time `json:"backoff_until"`
						LastSyncedAt      *time.Time `json:"last_synced_at"`
						MetadataCheckedAt *time.Time `json:"metadata_checked_at"`
						Balance           float64    `json:"balance"`
					} `json:"accounts_metadata"`
				}

				if err := json.Unmarshal(decrypted, &config); err == nil {
					accIDs := config.AccountIDs
					if len(accIDs) == 0 {
						accIDs = config.LegacyAccountIDs
					}
					for _, accID := range accIDs {
						meta, ok := config.AccountsMetadata[accID]
						if !ok {
							continue
						}
						accMap := make(map[string]interface{})
						accMap["id"] = accID
						if meta.Alias != "" {
							accMap["name"] = meta.Alias
						} else {
							accMap["name"] = accID
						}
						accMap["alias"] = meta.Alias
						accMap["currency"] = ""
						accMap["integration_name"] = integration.Name
						accMap["service_type"] = integration.ServiceType
						accMap["amount"] = meta.Balance
						accMap["iban"] = meta.IBAN
						accMap["integration_id"] = integration.ID

						realtimeAccounts[accID] = accMap
						if meta.Alias != "" {
							realtimeAccounts[meta.Alias] = accMap
						}
					}
				}
			}
		}
	}
	stateMap["realtime_accounts"] = realtimeAccounts

	// Decrypt connection secrets from vault
	connections, _ := s.connRepo.List(userID)
	secretsMap := make(map[string]string)
	for _, c := range connections {
		fullConn, err := s.connRepo.GetByID(userID, c.ID)
		if err == nil && fullConn != nil {
			decrypted, err := s.DecryptConnectionValue(userID, fullConn)
			if err == nil {
				secretsMap[fullConn.Name] = decrypted
			}
		}
	}

	// Mapped trigger payload
	if triggerPayload == nil {
		triggerPayload = map[string]interface{}{
			"type": "CRON",
			"data": map[string]interface{}{},
		}
	}

	// Calculate SHA-256 code hash
	hash := sha256.Sum256([]byte(plan.Code))
	codeHash := hex.EncodeToString(hash[:])

	correlationID := uuid.New().String()

	// 2. Register Active Execution Context for this correlationID
	s.activeContextsMu.Lock()
	s.activeContexts[correlationID] = &ActiveExecutionContext{
		UserID:   userID,
		Secrets:  secretsMap,
		StateMap: stateMap,
	}
	s.activeContextsMu.Unlock()

	defer func() {
		s.activeContextsMu.Lock()
		delete(s.activeContexts, correlationID)
		s.activeContextsMu.Unlock()
	}()

	// 3. Bidirectional RPC checking: Ask daemon if it already has this code compiled
	isCompiled := false
	checkMsgID := uuid.New().String()
	checkParams, _ := json.Marshal(map[string]interface{}{
		"plan_id":   plan.ID,
		"code_hash": codeHash,
	})

	checkFrameReq := &executionpb.StdioFrame{
		Message: &executionpb.StdioFrame_RpcRequest{
			RpcRequest: &executionpb.RpcRequest{
				CorrelationId: correlationID,
				MsgId:         checkMsgID,
				Method:        "check_compiled",
				ParamsJson:    checkParams,
			},
		},
	}
	checkBytes, _ := proto.Marshal(checkFrameReq)

	checkCh := make(chan *executionpb.StdioFrame, 1)
	s.pendingMu.Lock()
	s.pendingRequests[checkMsgID] = checkCh
	s.pendingMu.Unlock()

	defer func() {
		s.pendingMu.Lock()
		delete(s.pendingRequests, checkMsgID)
		s.pendingMu.Unlock()
	}()

	if err := s.writeFrame(checkBytes); err == nil {
		select {
		case checkFrameResp := <-checkCh:
			if resp := checkFrameResp.GetRpcResponse(); resp != nil {
				if resp.Success && resp.ResultJson != nil {
					var resMap map[string]interface{}
					if err := json.Unmarshal(resp.ResultJson, &resMap); err == nil {
						if cmpVal, exists := resMap["compiled"].(bool); exists {
							isCompiled = cmpVal
						}
					}
				}
			}
		case <-time.After(3 * time.Second):
			// Timeout checking compiled state - default to false
		}
	}

	// Filter referenced data to optimize transfer size and maintain synchronous execution safety
	referencedIncomes := []domain.Income{}
	incomeRegex := regexp.MustCompile(`income\(['"\x60]([^'"\x60]+)['"\x60]\)`)
	for _, match := range incomeRegex.FindAllStringSubmatch(plan.Code, -1) {
		name := match[1]
		for _, inc := range incomes {
			if inc.Name == name {
				referencedIncomes = append(referencedIncomes, inc)
				break
			}
		}
	}

	referencedAssets := []domain.Asset{}
	assetRegex := regexp.MustCompile(`asset\(['"\x60]([^'"\x60]+)['"\x60]\)`)
	for _, match := range assetRegex.FindAllStringSubmatch(plan.Code, -1) {
		name := match[1]
		for _, ast := range assets {
			if ast.Name == name {
				referencedAssets = append(referencedAssets, ast)
				break
			}
		}
	}

	referencedLoans := []domain.Loan{}
	loanRegex := regexp.MustCompile(`loan\(['"\x60]([^'"\x60]+)['"\x60]\)`)
	for _, match := range loanRegex.FindAllStringSubmatch(plan.Code, -1) {
		name := match[1]
		for _, ln := range loans {
			if ln.Name == name {
				referencedLoans = append(referencedLoans, ln)
				break
			}
		}
	}

	referencedRealtimeAccounts := make(map[string]interface{})
	accountRegex := regexp.MustCompile(`account\(['"\x60]([^'"\x60]+)['"\x60]\)`)
	for _, match := range accountRegex.FindAllStringSubmatch(plan.Code, -1) {
		key := match[1]
		if acc, ok := realtimeAccounts[key]; ok {
			referencedRealtimeAccounts[key] = acc
		}
	}

	// Find all secrets referenced in the code to preload ONLY the needed ones
	referencedSecrets := make(map[string]string)
	secretsRegex := regexp.MustCompile(`secrets\.([a-zA-Z0-9_]+)`)
	for _, match := range secretsRegex.FindAllStringSubmatch(plan.Code, -1) {
		secretKey := match[1]
		if val, ok := secretsMap[secretKey]; ok {
			referencedSecrets[secretKey] = val
		}
	}

	// 4. Construct multiplexed StdioRequest for execution
	ch := make(chan *executionpb.StdioFrame, 1)
	s.pendingMu.Lock()
	s.pendingRequests[correlationID] = ch
	s.pendingMu.Unlock()

	defer func() {
		s.pendingMu.Lock()
		delete(s.pendingRequests, correlationID)
		s.pendingMu.Unlock()
	}()

	// Send code only if NOT already compiled on the daemon
	codeToSend := ""
	if !isCompiled {
		codeToSend = plan.Code
	}

	// Send minimal preloaded state map (only referenced items, token, and api_base)
	minimalStateMap := map[string]interface{}{
		"api_base":          "http://localhost:8080",
		"execution_token":   token,
		"incomes":           referencedIncomes,
		"assets":            referencedAssets,
		"loans":             referencedLoans,
		"realtime_accounts": referencedRealtimeAccounts,
	}

	stateJSON, _ := json.Marshal(minimalStateMap)
	triggerJSON, _ := json.Marshal(triggerPayload)

	reqFrame := &executionpb.StdioFrame{
		Message: &executionpb.StdioFrame_ExecuteRequest{
			ExecuteRequest: &executionpb.ExecuteRequest{
				CorrelationId: correlationID,
				PlanId:        plan.ID,
				Code:          codeToSend,
				CodeHash:      codeHash,
				StateJson:     stateJSON,
				Secrets:       referencedSecrets,
				Trigger: &executionpb.ExecuteRequest_Trigger{
					Type:     triggerPayload["type"].(string),
					DataJson: triggerJSON,
				},
			},
		},
	}

	reqBytes, err := proto.Marshal(reqFrame)
	if err != nil {
		logEntry.Status = "FAILED"
		logEntry.ExitCode = 1
		logEntry.Stderr = fmt.Sprintf("[ExecutionEngine] Failed to marshal Protobuf request: %v", err)
		_ = s.planRepo.LogExecution(&logEntry)
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Write the frame to the persistent stdin pipe
	if err := s.writeFrame(reqBytes); err != nil {
		logEntry.Status = "FAILED"
		logEntry.ExitCode = 1
		logEntry.Stderr = fmt.Sprintf("[ExecutionEngine] Failed to write binary frame to runner stdin: %v", err)
		_ = s.planRepo.LogExecution(&logEntry)
		return fmt.Errorf("failed to write execution frame: %w", err)
	}

	// Block until response arrives or timeout (15 seconds)
	var resp *executionpb.ExecuteResponse
	select {
	case frame := <-ch:
		resp = frame.GetExecuteResponse()
		if resp == nil {
			logEntry.Status = "FAILED"
			logEntry.ExitCode = 1
			logEntry.Stderr = "[ExecutionEngine Error] Received invalid frame type for EXECUTE response."
			_ = s.planRepo.LogExecution(&logEntry)
			return fmt.Errorf("invalid response frame")
		}
	case <-time.After(15 * time.Second):
		logEntry.Status = "FAILED"
		logEntry.ExitCode = 1
		logEntry.Stderr = "[ExecutionEngine Error] Script execution timed out waiting for stdio multiplex response."
		_ = s.planRepo.LogExecution(&logEntry)
		return fmt.Errorf("plan execution timed out")
	}

	finishedAt := time.Now()
	logEntry.FinishedAt = &finishedAt

	// Write execution log entries
	logEntry.Stdout = resp.Stdout
	logEntry.Stderr = resp.Stderr
	logEntry.ExitCode = int(resp.ExitCode)
	if resp.Success {
		logEntry.Status = "SUCCESS"
		s.compiledMu.Lock()
		s.compiledPlans[plan.ID] = codeHash
		s.compiledMu.Unlock()
	} else {
		logEntry.Status = "FAILED"
	}

	_ = s.planRepo.LogExecution(&logEntry)

	if !resp.Success {
		return fmt.Errorf("plan execution failed: %s", resp.Stderr)
	}
	return nil
}

func (s *ExecutionService) StartCronScheduler() {
	ticker := time.NewTicker(60 * time.Second)
	go func() {
		for range ticker.C {
			now := time.Now()
			users, err := s.userRepo.ListAll()
			if err != nil {
				continue
			}

			for _, u := range users {
				plans, err := s.planRepo.List(u.ID)
				if err != nil {
					continue
				}

				for _, p := range plans {
					if !p.IsEnabled || p.TriggerType != "CRON" || p.TriggerValue == "" {
						continue
					}

					if MatchCron(p.TriggerValue, now) {
						go func(planID string) {
							_ = s.ExecutePlan(u.ID, planID, nil)
						}(p.ID)
					}
				}
			}
		}
	}()
}

func (s *ExecutionService) TriggerPlansForSyncFinished(userID string, integrationID string, integrationName string, serviceType string, discoveredCount int) {
	plans, err := s.planRepo.List(userID)
	if err != nil {
		return
	}

	for _, p := range plans {
		if !p.IsEnabled {
			continue
		}

		// Trigger if TriggerType matches OR legacy TopicSyncFinished annotation is present
		if p.TriggerType == "SYNC_FINISHED" || strings.Contains(p.Code, "TopicSyncFinished") {
			// If it's a specific integration trigger, verify ID
			if p.TriggerType == "SYNC_FINISHED" && p.TriggerValue != "" && p.TriggerValue != "ALL" {
				if p.TriggerValue != integrationID {
					continue
				}
			}

			go func(planID string) {
				triggerMap := map[string]interface{}{
					"type": "SYNC_FINISHED",
					"data": map[string]interface{}{
						"integration_id":          integrationID,
						"integration_name":        integrationName,
						"service_type":            serviceType,
						"discovered_transactions": discoveredCount,
						"timestamp":               time.Now().Format(time.RFC3339),
					},
				}

				_ = s.ExecutePlan(userID, planID, triggerMap)
			}(p.ID)
		}
	}
}

func (s *ExecutionService) TriggerPlansForTransaction(userID string, tx domain.BankTransaction, amount float64, receiver string, description string) {
	plans, err := s.planRepo.List(userID)
	if err != nil {
		return
	}

	for _, p := range plans {
		if !p.IsEnabled {
			continue
		}

		// Trigger if TriggerType matches OR legacy TopicTransactionDiscovered annotation is present
		if p.TriggerType == "TRANSACTION_NEW" || strings.Contains(p.Code, "TopicTransactionDiscovered") {
			go func(planID string) {
				triggerMap := map[string]interface{}{
					"type": "TRANSACTION",
					"data": map[string]interface{}{
						"id":             tx.ID,
						"amount":         amount,
						"receiver":       receiver,
						"description":    description,
						"integration_id": tx.IntegrationID,
						"account_id":     tx.AccountID,
						"timestamp":      time.Now().Format(time.RFC3339),
					},
				}

				_ = s.ExecutePlan(userID, planID, triggerMap)
			}(p.ID)
		}
	}
}

func MatchCron(expression string, t time.Time) bool {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return false
	}

	minuteMatches := matchCronField(fields[0], t.Minute(), 0, 59)
	hourMatches := matchCronField(fields[1], t.Hour(), 0, 23)
	domMatches := matchCronField(fields[2], t.Day(), 1, 31)
	monthMatches := matchCronField(fields[3], int(t.Month()), 1, 12)
	dowMatches := matchCronField(fields[4], int(t.Weekday()), 0, 6)

	return minuteMatches && hourMatches && domMatches && monthMatches && dowMatches
}

func matchCronField(field string, value int, min int, max int) bool {
	if field == "*" {
		return true
	}

	if strings.Contains(field, ",") {
		parts := strings.Split(field, ",")
		for _, part := range parts {
			if matchCronField(part, value, min, max) {
				return true
			}
		}
		return false
	}

	if strings.Contains(field, "/") {
		parts := strings.Split(field, "/")
		if len(parts) != 2 {
			return false
		}
		stepVal := 0
		_, _ = fmt.Sscanf(parts[1], "%d", &stepVal)
		if stepVal <= 0 {
			return false
		}

		rangeField := parts[0]
		if rangeField == "*" {
			return value%stepVal == 0
		}

		var start, end int
		if strings.Contains(rangeField, "-") {
			_, _ = fmt.Sscanf(rangeField, "%d-%d", &start, &end)
		} else {
			_, _ = fmt.Sscanf(rangeField, "%d", &start)
			end = max
		}

		if value >= start && value <= end {
			return (value-start)%stepVal == 0
		}
		return false
	}

	if strings.Contains(field, "-") {
		var start, end int
		_, err := fmt.Sscanf(field, "%d-%d", &start, &end)
		if err != nil {
			return false
		}
		return value >= start && value <= end
	}

	var exact int
	_, err := fmt.Sscanf(field, "%d", &exact)
	if err != nil {
		return false
	}
	if exact == 7 && min == 0 && max == 6 {
		exact = 0
	}
	return exact == value
}
