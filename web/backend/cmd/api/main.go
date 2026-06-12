package main

import (
	"net/http"
	"os"
	"strings"

	"github.com/genazt/my-budget-script/web/backend/internal/api"
	"github.com/genazt/my-budget-script/web/backend/internal/api/handler"
	"github.com/genazt/my-budget-script/web/backend/internal/bus"
	"github.com/genazt/my-budget-script/web/backend/internal/crypto"
	"github.com/genazt/my-budget-script/web/backend/internal/db"
	"github.com/genazt/my-budget-script/web/backend/internal/integration"
	eb_provider "github.com/genazt/my-budget-script/web/backend/internal/integration/enablebanking"
	gc_provider "github.com/genazt/my-budget-script/web/backend/internal/integration/gocardless"
	t212_provider "github.com/genazt/my-budget-script/web/backend/internal/integration/trading212"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	"github.com/genazt/my-budget-script/web/backend/internal/service"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
)

func main() {
	// Initialize Echo
	e := echo.New()
	e.Logger.SetLevel(log.DEBUG)

	// Environment Configuration
	rpID := os.Getenv("RP_ID")
	if rpID == "" {
		rpID = "localhost"
	}

	origins := []string{"http://localhost:5173", "http://vm-host.lan:5173", "https://budget.genazt.me"}
	if envOrigins := os.Getenv("RP_ORIGINS"); envOrigins != "" {
		origins = strings.Split(envOrigins, ",")
	}

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     origins,
		AllowHeaders:     []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept},
		AllowCredentials: true,
	}))

	// WebAuthn configuration
	wauth, err := webauthn.New(&webauthn.Config{
		RPDisplayName: "WealthEngine",
		RPID:          rpID,
		RPOrigins:     origins,
	})
	if err != nil {
		e.Logger.Fatal(err)
	}
	log.Printf("[AUTH] WebAuthn Initialized with RPID: %s, Origins: %v", rpID, origins)

	// Database initialization
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://budget:budgetpass@localhost:5432/budget?sslmode=disable"
	}

	database, err := db.InitDB(dbURL)
	if err != nil {
		e.Logger.Fatal(err)
	}
	defer database.Close()

	// Data directory configuration
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/app/data"
	}

	// Backup database on startup
	db.BackupDB(dbURL, dataDir)

	userRepo := repository.NewUserRepository(database)
	sessionRepo := repository.NewSessionRepository(database)
	incomeRepo := repository.NewIncomeRepository(database)
	billRepo := repository.NewBillRepository(database)
	expenseRepo := repository.NewExpenseRepository(database)
	assetRepo := repository.NewAssetRepository(database)
	loanRepo := repository.NewLoanRepository(database)
	modRepo := repository.NewModificationRepository(database)
	cacheRepo := repository.NewCacheRepository(database)
	integrationRepo := repository.NewIntegrationRepository(database)
	transactionRepo := repository.NewTransactionRepository(database)
	ruleRepo := repository.NewRuleRepository(database)
	scenarioRepo := repository.NewScenarioRepository(database, incomeRepo, billRepo, expenseRepo)
	virtualAccountRepo := repository.NewVirtualAccountRepository(database)

	executionRepo := repository.NewExecutionRepository(database)
	connectionRepo := repository.NewConnectionRepository(database)

	cryptoService, err := crypto.NewCryptoService(dataDir)
	if err != nil {
		e.Logger.Fatal(err)
	}

	// Initialize Event Bus
	eventBus := bus.NewBus()

	marketDataService := service.NewMarketDataService(cacheRepo, dataDir)
	gcService := service.NewGoCardlessService()
	t212Service := service.NewTrading212Service()
	ebService := service.NewEnableBankingService()
	ruleService := service.NewRuleService(ruleRepo)

	// Initialize Integration Registry
	integrationRegistry := integration.NewRegistry()
	syncService := service.NewSyncService(integrationRepo, transactionRepo, connectionRepo, assetRepo, userRepo, cryptoService, integrationRegistry, ruleService, eventBus)

	integrationRegistry.Register(gc_provider.NewProvider(integrationRepo, transactionRepo, cryptoService, gcService, ruleService, syncService, eventBus))
	integrationRegistry.Register(t212_provider.NewProvider(integrationRepo, transactionRepo, assetRepo, cryptoService, t212Service, ruleService, syncService, eventBus, database))
	integrationRegistry.Register(eb_provider.NewProvider(integrationRepo, transactionRepo, cryptoService, ebService, ruleService, syncService, eventBus))

	projectionService := service.NewProjectionService(scenarioRepo, incomeRepo, billRepo, expenseRepo, assetRepo, marketDataService)
	projectionService.SetUserRepo(userRepo)
	projectionService.SetAdditionalRepos(loanRepo, modRepo)
	projectionService.SetVirtualAccountRepo(virtualAccountRepo)
	projectionService.SetRealtimeData(transactionRepo, cryptoService, syncService)

	executionService := service.NewExecutionService(executionRepo, connectionRepo, userRepo, incomeRepo, assetRepo, loanRepo, cryptoService, eventBus)
	executionService.SetIntegrationRepo(integrationRepo)
	executionService.SetSyncService(syncService)
	executionService.StartCronScheduler()

	// Start Background Sync Worker (1m ticker)
	syncService.StartBackgroundWorker()

	// Ensure all users have recovery tokens and slots
	resetRecovery := strings.ToLower(strings.Trim(os.Getenv("RESET_RECOVERY"), "\""))
	if resetRecovery == "true" || resetRecovery == "1" {
		log.Printf("[RECOVERY] Emergency Reset Triggered: Clearing all recovery hashes...")
		users, _ := userRepo.ListAll()
		for _, u := range users {
			userRepo.UpdateRecoveryHash(u.ID, "")
		}
	}
	syncService.EnsureRecoveryTokens()
	syncService.MigrateTransactionsBetweenChains()
	syncService.WipeAndReimportBankLogs()

	// Reset integration errors on boot to allow them to retry
	if err := integrationRepo.ResetAllErrors(); err != nil {
		log.Printf("[SYNC] Failed to reset integration errors: %v", err)
	} else {
		log.Printf("[SYNC] Successfully reset all integration error states")
	}

	webSocketHandler := api.NewWebSocketHandler(eventBus, projectionService)

	// JWT Secret for authentication
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "internal-secret-key"
		log.Printf("[AUTH] Warning: JWT_SECRET not set, using default development key")
	}

	// 1. Initialize special cases or dependencies
	authAPI := handler.NewAuth(webSocketHandler, userRepo, wauth, sessionRepo, syncService, []byte(jwtSecret))

	// 2. Register everything in a single, readable pass
	webSocketHandler.Register(
		authAPI,
		handler.NewUser(webSocketHandler, userRepo, syncService),
		handler.NewAssets(webSocketHandler, assetRepo, scenarioRepo, marketDataService),
		handler.NewBills(webSocketHandler, billRepo, scenarioRepo),
		handler.NewExpenses(webSocketHandler, expenseRepo, scenarioRepo),
		handler.NewLoans(webSocketHandler, loanRepo, scenarioRepo),
		handler.NewModifications(webSocketHandler, modRepo),
		handler.NewScenarios(webSocketHandler, scenarioRepo),
		handler.NewIncomes(webSocketHandler, incomeRepo, scenarioRepo),
		handler.NewAutomations(webSocketHandler, executionRepo, connectionRepo, executionService),
		handler.NewIntegrations(webSocketHandler, integrationRepo, syncService, gcService, ebService, cryptoService, userRepo, transactionRepo),
		handler.NewRules(webSocketHandler, ruleRepo, syncService),
		handler.NewPools(webSocketHandler, ruleRepo, syncService),
		handler.NewVirtualAccounts(webSocketHandler, virtualAccountRepo),
	)

	// Routes
	e.GET("/health", func(c echo.Context) error {
		return c.String(http.StatusOK, "OK")
	})
	e.GET("/api/ws", webSocketHandler.WebSocketGateway)

	// Dedicated HTTP Routes (Mandatory legacy flows)
	e.GET("/auth/oauth2/callback", api.HandleEnableBankingCallback(integrationRepo, syncService, cryptoService, ebService))

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	e.Logger.Fatal(e.Start(":" + port))
}
