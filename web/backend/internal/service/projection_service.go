package service

import (
	"sync"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/crypto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
)

type ProjectionService struct {
	scenarioRepo       *repository.ScenarioRepository
	userRepo           *repository.UserRepository
	incomeRepo         *repository.IncomeRepository
	billRepo           *repository.BillRepository
	expenseRepo        *repository.ExpenseRepository
	assetRepo          *repository.AssetRepository
	loanRepo           *repository.LoanRepository
	modRepo            *repository.ModificationRepository
	virtualAccountRepo *repository.VirtualAccountRepository
	transactionRepo    *repository.TransactionRepository
	cryptoService      *crypto.CryptoService
	syncService        *SyncService
	marketData         *MarketDataService

	mcCache   map[string]float64
	mcCacheMu sync.RWMutex
}

func NewProjectionService(sr *repository.ScenarioRepository, ir *repository.IncomeRepository, br *repository.BillRepository, er *repository.ExpenseRepository, ar *repository.AssetRepository, mds *MarketDataService) *ProjectionService {
	return &ProjectionService{
		scenarioRepo: sr,
		incomeRepo:   ir,
		billRepo:     br,
		expenseRepo:  er,
		assetRepo:    ar,
		marketData:   mds,
		mcCache:      make(map[string]float64),
	}
}

func (s *ProjectionService) SetVirtualAccountRepo(r *repository.VirtualAccountRepository) {
	s.virtualAccountRepo = r
}

func (s *ProjectionService) SetUserRepo(r *repository.UserRepository) {
	s.userRepo = r
}

func (s *ProjectionService) getUserLocation(userID string) *time.Location {
	if s.userRepo == nil {
		return time.UTC
	}
	user, err := s.userRepo.GetUserByID(userID)
	if err != nil || user == nil || user.Timezone == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(user.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

func (s *ProjectionService) SetRealtimeData(tr *repository.TransactionRepository, cs *crypto.CryptoService, ss *SyncService) {
	s.transactionRepo = tr
	s.cryptoService = cs
	s.syncService = ss
}

func (s *ProjectionService) SetAdditionalRepos(lr *repository.LoanRepository, mr *repository.ModificationRepository) {
	s.loanRepo = lr
	s.modRepo = mr
}

func (s *ProjectionService) loadRealtimeBalances(userID string, monthStartDay int) (map[string]map[string]float64, error) {
	balances := make(map[string]map[string]float64)
	loc := s.getUserLocation(userID)

	if s.transactionRepo == nil || s.cryptoService == nil || s.syncService == nil {
		return balances, nil
	}

	txs, err := s.transactionRepo.List(userID)
	if err != nil {
		return nil, err
	}

	if len(txs) == 0 {
		return balances, nil
	}

	integrations, err := s.syncService.integrationRepo.List(userID)
	if err != nil {
		return nil, err
	}

	intTypeMap := make(map[string]string)
	intKeyMap := make(map[string][]byte)
	for _, i := range integrations {
		intTypeMap[i.ID] = i.ServiceType
		key, err := s.syncService.GetMasterKey(userID, i.ID)
		if err == nil {
			intKeyMap[i.ID] = key
		}
	}

	activeIntegrations, _ := s.syncService.GetAccountActiveIntegrations(userID, intKeyMap, nil)

	// Load pools to build hierarchy map
	pools, err := s.syncService.ruleService.repo.ListPools(userID)
	if err != nil {
		return nil, err
	}
	poolMap := make(map[string]domain.TransactionPool)
	for _, p := range pools {
		poolMap[p.ID] = p
	}

	for i := range txs {
		t := &txs[i]
		if len(t.PoolIDs) == 0 {
			continue
		}

		serviceType, exists := intTypeMap[t.IntegrationID]
		if !exists {
			continue
		}

		decrypted, err := s.syncService.DecryptTransaction(userID, t, intKeyMap, activeIntegrations)
		if err != nil {
			continue
		}

		amount := 0.0
		provider := s.syncService.GetProvider(serviceType)
		if provider == nil {
			continue
		}

		meta, err := provider.ParseTransaction(decrypted, t.AccountID)
		if err != nil {
			continue
		}
		amount = meta.Amount

		monthStr := projectionMonthForDate(t.CreatedAt.UTC(), monthStartDay, loc).Format("2006-01")

		for _, poolID := range t.PoolIDs {
			if poolID == "" {
				continue
			}

			// Aggregate into parent pools
			currPoolID := poolID
			visited := make(map[string]bool)
			for currPoolID != "" && !visited[currPoolID] {
				visited[currPoolID] = true
				if balances[currPoolID] == nil {
					balances[currPoolID] = make(map[string]float64)
				}
				balances[currPoolID][monthStr] += amount

				// Move to parent
				if p, ok := poolMap[currPoolID]; ok && p.ParentID != nil {
					currPoolID = *p.ParentID
				} else {
					currPoolID = ""
				}
			}
		}
	}

	return balances, nil
}
