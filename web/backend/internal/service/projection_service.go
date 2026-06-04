package service

import (
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/crypto"
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
	"github.com/genazt/my-budget-script/web/backend/internal/repository"
	"github.com/google/uuid"
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
}

func NewProjectionService(sr *repository.ScenarioRepository, ir *repository.IncomeRepository, br *repository.BillRepository, er *repository.ExpenseRepository, ar *repository.AssetRepository, mds *MarketDataService) *ProjectionService {

	return &ProjectionService{
		scenarioRepo: sr,
		incomeRepo:   ir,
		billRepo:     br,
		expenseRepo:  er,
		assetRepo:    ar,
		marketData:   mds,
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

		meta, err := provider.ParseTransaction(decrypted)
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

type etfLot struct {
	id           string
	createdAt    time.Time
	principal    float64
	currentValue float64
}

type subAssetState struct {
	id                  string
	name                string
	currentBalance      float64
	targetValue         string
	amountPerMonth      float64
	isRemainderConsumer bool
	remainderStartDate  *time.Time
	dumpingLoanID       *string
	startDate           time.Time
	endDate             *time.Time
	earliestDumpDate    *time.Time
	isClosed            bool
}

type assetState struct {
	asset                domain.Asset
	currentBalance       float64
	accruedInterest      float64
	isClosed             bool
	contributionsStopped bool
	etfHistory           []map[string]float64
	simulatedYield       float64
	lots                 []etfLot
	subAssets            []*subAssetState
	trackerBalances      map[string]float64
	trackerYields        map[string]float64
	activeFlows          map[string]float64
	activeSubAssetFlows  map[string]float64

	lotCounter      *int
	penaltyAnalysis *[]domain.PenaltyEvent
	currentMonth    time.Time
}

func (as *assetState) addLot(amount float64) {
	if amount <= 0 {
		return
	}

	lotID := "INITIAL"
	if as.asset.ActiveVersion.Type == domain.AssetTypeStatic {
		lotID = "STATIC"
	} else if as.lotCounter != nil {
		*as.lotCounter++
		lotID = fmt.Sprintf("LOT-%06d", *as.lotCounter)
	}

	if as.asset.ActiveVersion.Type == domain.AssetTypeETF {
		as.lots = append(as.lots, etfLot{
			id:           lotID,
			createdAt:    as.currentMonth,
			principal:    amount,
			currentValue: amount,
		})
	}

	if as.penaltyAnalysis != nil {
		*as.penaltyAnalysis = append(*as.penaltyAnalysis, domain.PenaltyEvent{
			Type:         "BUY",
			Date:         as.currentMonth,
			AssetName:    as.asset.Name,
			LotID:        lotID,
			LotCreatedAt: as.currentMonth,
			Amount:       amount,
		})
	}
}

func depositToTrackers(as *assetState, amount float64) {
	if as.asset.ActiveVersion.Type != domain.AssetTypeETF || amount <= 0 {
		return
	}
	if as.trackerBalances == nil {
		as.trackerBalances = make(map[string]float64)
	}
	if as.activeFlows == nil {
		as.activeFlows = make(map[string]float64)
	}

	totalBalBefore := 0.0
	for _, bal := range as.trackerBalances {
		totalBalBefore += bal
	}

	// If there is no existing balance, we just deposit proportionally to target percentages
	if totalBalBefore <= 0.01 {
		for _, t := range as.asset.ActiveVersion.ETFConfig {
			dep := amount * t.Percentage
			as.trackerBalances[t.Tracker] += dep
			as.activeFlows[t.Tracker] += dep
		}
		return
	}

	totalBalAfter := totalBalBefore + amount

	// Calculate how much each tracker needs to catch up to its target
	neededMap := make(map[string]float64)
	totalNeeded := 0.0
	for _, t := range as.asset.ActiveVersion.ETFConfig {
		targetVal := totalBalAfter * t.Percentage
		currentVal := as.trackerBalances[t.Tracker]
		diff := targetVal - currentVal
		if diff > 0 {
			neededMap[t.Tracker] = diff
			totalNeeded += diff
		} else {
			neededMap[t.Tracker] = 0.0
		}
	}

	// If some trackers need to catch up, we distribute the deposit amount proportionally to what is needed
	if totalNeeded > 0.0001 {
		if amount <= totalNeeded {
			// Not enough to fully catch up, distribute entire amount proportionally to deficits
			leftover := amount
			for _, t := range as.asset.ActiveVersion.ETFConfig {
				needed := neededMap[t.Tracker]
				if needed > 0 {
					dep := amount * (needed / totalNeeded)
					if dep > leftover {
						dep = leftover
					}
					as.trackerBalances[t.Tracker] += dep
					as.activeFlows[t.Tracker] += dep
					leftover -= dep
				}
			}
			// If there is any minor floating point leftover, put it in the first tracker that needed catch up
			if leftover > 0.0001 {
				for _, t := range as.asset.ActiveVersion.ETFConfig {
					if neededMap[t.Tracker] > 0 {
						as.trackerBalances[t.Tracker] += leftover
						as.activeFlows[t.Tracker] += leftover
						break
					}
				}
			}
		} else {
			// We have more than enough to catch up!
			// Bring every tracker exactly to its target percentage for the new total balance (totalBalAfter)
			for _, t := range as.asset.ActiveVersion.ETFConfig {
				targetVal := totalBalAfter * t.Percentage
				dep := targetVal - as.trackerBalances[t.Tracker]
				if dep < 0 {
					dep = 0
				}
				as.trackerBalances[t.Tracker] = targetVal
				as.activeFlows[t.Tracker] += dep
			}
		}
	} else {
		// If everyone is already at or above target (or totalNeeded is 0), split using target percentages
		for _, t := range as.asset.ActiveVersion.ETFConfig {
			dep := amount * t.Percentage
			as.trackerBalances[t.Tracker] += dep
			as.activeFlows[t.Tracker] += dep
		}
	}
}

func withdrawFromTrackers(as *assetState, grossSold float64) {
	if as.asset.ActiveVersion.Type != domain.AssetTypeETF || grossSold <= 0 {
		return
	}
	if as.trackerBalances == nil {
		return
	}
	if as.activeFlows == nil {
		as.activeFlows = make(map[string]float64)
	}

	totalBal := 0.0
	for _, bal := range as.trackerBalances {
		totalBal += bal
	}
	if totalBal > 0 {
		remaining := grossSold
		for _, t := range as.asset.ActiveVersion.ETFConfig {
			tracker := t.Tracker
			bal := as.trackerBalances[tracker]
			share := bal / totalBal
			toSub := grossSold * share
			if toSub > bal {
				toSub = bal
			}
			as.trackerBalances[tracker] -= toSub
			as.activeFlows[tracker] -= toSub
			remaining -= toSub
		}
		if math.Abs(remaining) > 0.00001 {
			for _, t := range as.asset.ActiveVersion.ETFConfig {
				tracker := t.Tracker
				bal := as.trackerBalances[tracker]
				if bal >= remaining {
					as.trackerBalances[tracker] -= remaining
					as.activeFlows[tracker] -= remaining
					break
				}
			}
		}
	}
	for tracker, bal := range as.trackerBalances {
		if bal < 0.01 {
			as.trackerBalances[tracker] = 0
		}
	}
}

func buildAssetBreakdownEntry(as *assetState, name string, amount float64, interest float64, penalty float64) domain.EntryBreakdown {
	entry := domain.EntryBreakdown{
		Name:       name,
		EntityName: as.asset.Name,
		Amount:     amount,
		Interest:   interest,
		Penalty:    penalty,
		Balance:    as.currentBalance,
		AccountIDs: as.asset.AccountIDs,
		PoolID:     as.asset.PoolID,
	}
	if as.asset.ActiveVersion.Type == domain.AssetTypeETF && len(as.trackerBalances) > 0 && as.currentBalance > 0 {
		splits := make(map[string]float64)
		for tracker, bal := range as.trackerBalances {
			splits[tracker] = bal / as.currentBalance
		}
		entry.RealSplit = splits
	}
	if len(as.activeFlows) > 0 {
		entry.TrackerFlows = as.activeFlows
		as.activeFlows = nil // Clear consumed flows
	}
	if len(as.activeSubAssetFlows) > 0 {
		entry.SubAssetFlows = as.activeSubAssetFlows
		as.activeSubAssetFlows = nil
	}
	return entry
}

func depositToSubAsset(as *assetState, subID string, amount float64) {
	if as.activeSubAssetFlows == nil {
		as.activeSubAssetFlows = make(map[string]float64)
	}

	for _, sa := range as.subAssets {
		if sa.id == subID {
			sa.currentBalance += amount
			as.activeSubAssetFlows[sa.name] += amount
			break
		}
	}
	as.currentBalance += amount
	if as.asset.ActiveVersion.Type == domain.AssetTypeETF && amount > 0 {
		as.addLot(amount)
		depositToTrackers(as, amount)
	}
}

func getSubAssetTarget(sa *subAssetState, loanByID map[string]*loanState) float64 {
	if sa.targetValue != "" && sa.targetValue != "0" {
		target, _ := strconv.ParseFloat(sa.targetValue, 64)
		return target
	}
	if sa.dumpingLoanID != nil {
		if ls, ok := loanByID[*sa.dumpingLoanID]; ok {
			penalty := ls.loan.ActiveVersion.EarlyPayoffPenalty / 100.0
			if penalty < 0 {
				penalty = 0.01
			}
			return ls.currentBalance / (1.0 - penalty)
		}
	}
	return -1 // Infinite
}

func depositAsset(as *assetState, amount float64, loanByID map[string]*loanState) float64 {
	if len(as.subAssets) > 0 {
		if as.activeSubAssetFlows == nil {
			as.activeSubAssetFlows = make(map[string]float64)
		}

		remaining := amount
		hasInfinite := false
		for _, sa := range as.subAssets {
			if sa.isClosed {
				continue
			}

			target := getSubAssetTarget(sa, loanByID)
			if target < 0 {
				hasInfinite = true
				continue
			}

			room := math.Max(0, target-sa.currentBalance)
			toDep := math.Min(remaining, room)
			if toDep > 0 {
				sa.currentBalance += toDep
				as.activeSubAssetFlows[sa.name] += toDep
				remaining -= toDep
			}
		}

		// Overflow to first infinite sub-asset if any, otherwise return remaining
		if remaining > 0.00001 {
			if hasInfinite {
				for _, sa := range as.subAssets {
					if !sa.isClosed && getSubAssetTarget(sa, loanByID) < 0 {
						sa.currentBalance += remaining
						as.activeSubAssetFlows[sa.name] += remaining
						remaining = 0
						break
					}
				}
			}
		}

		deposited := amount - remaining
		as.currentBalance += deposited
		if as.asset.ActiveVersion.Type == domain.AssetTypeETF && deposited > 0 {
			as.addLot(deposited)
			depositToTrackers(as, deposited)
		}
		return deposited
	} else {
		as.currentBalance += amount
		if amount > 0 {
			as.addLot(amount)
			if as.asset.ActiveVersion.Type == domain.AssetTypeETF {
				depositToTrackers(as, amount)
			}
		}
		return amount
	}
}

func depositAssetProportionally(as *assetState, amount float64, loanByID map[string]*loanState) float64 {
	if len(as.subAssets) > 0 {
		if as.activeSubAssetFlows == nil {
			as.activeSubAssetFlows = make(map[string]float64)
		}

		totalBal := 0.0
		var activeSubAssets []*subAssetState
		for _, sa := range as.subAssets {
			if !sa.isClosed {
				totalBal += sa.currentBalance
				activeSubAssets = append(activeSubAssets, sa)
			}
		}

		if len(activeSubAssets) == 0 {
			return 0
		}

		remaining := amount
		if totalBal > 0 {
			for _, sa := range activeSubAssets {
				share := sa.currentBalance / totalBal
				toDep := amount * share

				// Check target
				target := getSubAssetTarget(sa, loanByID)
				if target >= 0 {
					room := math.Max(0, target-sa.currentBalance)
					toDep = math.Min(toDep, room)
				}

				if toDep > 0 {
					sa.currentBalance += toDep
					as.activeSubAssetFlows[sa.name] += toDep
					remaining -= toDep
				}
			}
		} else {
			// No existing balance, try sequential till we hit an infinite or run out
			return depositAsset(as, amount, loanByID)
		}

		deposited := amount - remaining
		as.currentBalance += deposited
		if as.asset.ActiveVersion.Type == domain.AssetTypeETF && deposited > 0 {
			as.addLot(deposited)
			depositToTrackers(as, deposited)
		}
		return deposited
	} else {
		as.currentBalance += amount
		if amount > 0 {
			as.addLot(amount)
			if as.asset.ActiveVersion.Type == domain.AssetTypeETF {
				depositToTrackers(as, amount)
			}
		}
		return amount
	}
}

func getWithdrawalPenaltyRate(v *domain.AssetVersion) float64 {
	rate := 0.0
	for _, p := range v.Penalties {
		if p.TriggerType == domain.PenaltyTriggerWithdrawal {
			rate += p.Percentage
		}
	}
	return rate / 100.0
}

func getInterestPenaltyRate(v *domain.AssetVersion) float64 {
	rate := 0.0
	for _, p := range v.Penalties {
		if p.TriggerType == domain.PenaltyTriggerInterest {
			rate += p.Percentage
		}
	}
	return rate / 100.0
}

func calculateMaxNet(as *assetState) float64 {
	if as.currentBalance <= 0 {
		return 0
	}
	penalty := getWithdrawalPenaltyRate(as.asset.ActiveVersion)
	if as.asset.ActiveVersion.Type == "STATIC" {
		return as.currentBalance * (1.0 - penalty)
	}

	var maxNet float64
	for _, lot := range as.lots {
		if lot.currentValue <= 0 {
			continue
		}
		profitMargin := 0.0
		if lot.currentValue > lot.principal {
			profitMargin = (lot.currentValue - lot.principal) / lot.currentValue
		}
		maxNet += lot.currentValue * (1.0 - (profitMargin * penalty))
	}
	return maxNet
}

func calculateMaxNetForSubAsset(as *assetState, sa *subAssetState) float64 {
	if sa.currentBalance <= 0 {
		return 0
	}
	penalty := getWithdrawalPenaltyRate(as.asset.ActiveVersion)
	if as.asset.ActiveVersion.Type == "STATIC" {
		return sa.currentBalance * (1.0 - penalty)
	}

	var maxNet float64
	remainingSA := sa.currentBalance
	for _, lot := range as.lots {
		if lot.currentValue <= 0 || remainingSA <= 0 {
			continue
		}
		profitMargin := 0.0
		if lot.currentValue > lot.principal {
			profitMargin = (lot.currentValue - lot.principal) / lot.currentValue
		}
		maxGrossFromLot := math.Min(lot.currentValue, remainingSA)
		maxNet += maxGrossFromLot * (1.0 - (profitMargin * penalty))
		remainingSA -= maxGrossFromLot
	}
	return maxNet
}

func withdrawFromSubAsset(as *assetState, subID string, requestedNet float64) (grossSold float64, netFulfilled float64) {
	var targetSA *subAssetState
	for _, sa := range as.subAssets {
		if sa.id == subID {
			targetSA = sa
			break
		}
	}
	if targetSA == nil || targetSA.currentBalance <= 0 || requestedNet <= 0 {
		return 0, 0
	}

	penalty := getWithdrawalPenaltyRate(as.asset.ActiveVersion)

	if as.asset.ActiveVersion.Type == "STATIC" {
		maxNetPossible := targetSA.currentBalance * (1.0 - penalty)

		if requestedNet >= maxNetPossible {
			grossSold = targetSA.currentBalance
			netFulfilled = maxNetPossible
			targetSA.currentBalance = 0
		} else {
			grossSold = requestedNet / (1.0 - penalty)
			netFulfilled = requestedNet
			targetSA.currentBalance -= grossSold
		}

		as.currentBalance -= grossSold

		if as.penaltyAnalysis != nil {
			*as.penaltyAnalysis = append(*as.penaltyAnalysis, domain.PenaltyEvent{
				Type:          "SELL",
				Date:          as.currentMonth,
				AssetName:     as.asset.Name,
				LotID:             "STATIC",
				LotCreatedAt:      as.currentMonth,
				Amount:            grossSold,
				PrincipalSold:     grossSold,
				PenaltyPaid:       grossSold - netFulfilled,
				MonthsHeld:        0,
				InterestGenerated: 0,
			})
		}

		return grossSold, netFulfilled
	}

	// ETF FIFO withdrawal capped at sub-asset's current balance
	remainingNet := requestedNet
	var newLots []etfLot

	for _, lot := range as.lots {
		if lot.currentValue <= 0 {
			continue
		}
		if remainingNet <= 0 || targetSA.currentBalance <= 0 {
			newLots = append(newLots, lot)
			continue
		}

		profitMargin := 0.0
		if lot.currentValue > lot.principal {
			profitMargin = (lot.currentValue - lot.principal) / lot.currentValue
		}

		maxGrossFromLot := math.Min(lot.currentValue, targetSA.currentBalance)
		if maxGrossFromLot <= 0 {
			newLots = append(newLots, lot)
			continue
		}

		grossNeeded := remainingNet / (1.0 - (profitMargin * penalty))

		if grossNeeded <= maxGrossFromLot {
			grossSold += grossNeeded
			netFulfilled += remainingNet
			targetSA.currentBalance -= grossNeeded

			fractionSold := grossNeeded / lot.currentValue
			principalSold := lot.principal * fractionSold
			penaltyPaid := grossNeeded - remainingNet

			if as.penaltyAnalysis != nil {
				*as.penaltyAnalysis = append(*as.penaltyAnalysis, domain.PenaltyEvent{
					Type:              "SELL",
					Date:              as.currentMonth,
					AssetName:         as.asset.Name,
					LotID:             lot.id,
					LotCreatedAt:      lot.createdAt,
					Amount:            grossNeeded,
					PrincipalSold:     principalSold,
					PenaltyPaid:       penaltyPaid,
					MonthsHeld:        diffMonths(lot.createdAt, as.currentMonth),
					InterestGenerated: grossNeeded - principalSold,
				})
			}

			lot.currentValue -= grossNeeded
			lot.principal -= principalSold

			remainingNet = 0
			newLots = append(newLots, lot)
		} else {
			grossSold += maxGrossFromLot
			netFromLot := maxGrossFromLot * (1.0 - (profitMargin * penalty))
			netFulfilled += netFromLot
			remainingNet -= netFromLot
			targetSA.currentBalance -= maxGrossFromLot

			penaltyPaid := maxGrossFromLot - netFromLot
			if as.penaltyAnalysis != nil {
				*as.penaltyAnalysis = append(*as.penaltyAnalysis, domain.PenaltyEvent{
					Type:              "SELL",
					Date:              as.currentMonth,
					AssetName:         as.asset.Name,
					LotID:             lot.id,
					LotCreatedAt:      lot.createdAt,
					Amount:            maxGrossFromLot,
					PrincipalSold:     lot.principal,
					PenaltyPaid:       penaltyPaid,
					MonthsHeld:        diffMonths(lot.createdAt, as.currentMonth),
					InterestGenerated: maxGrossFromLot - lot.principal,
				})
			}

			fractionSold := maxGrossFromLot / lot.currentValue
			lot.currentValue -= maxGrossFromLot
			lot.principal -= lot.principal * fractionSold

			if lot.currentValue > 0 {
				newLots = append(newLots, lot)
			}
		}
	}
	as.lots = newLots
	as.currentBalance -= grossSold
	if as.asset.ActiveVersion.Type == domain.AssetTypeETF {
		withdrawFromTrackers(as, grossSold)
	}
	if as.currentBalance < 0.01 {
		as.currentBalance = 0
		as.lots = nil
	}
	if targetSA.currentBalance < 0.01 {
		targetSA.currentBalance = 0
	}
	return grossSold, netFulfilled
}

func withdrawAsset(as *assetState, requestedNet float64) (grossSold float64, netFulfilled float64) {
	if requestedNet <= 0 || as.currentBalance <= 0 {
		return 0, 0
	}

	penalty := getWithdrawalPenaltyRate(as.asset.ActiveVersion)

	if as.asset.ActiveVersion.Type == "STATIC" {
		maxNetPossible := as.currentBalance * (1.0 - penalty)
		if requestedNet >= maxNetPossible {
			grossSold = as.currentBalance
			netFulfilled = maxNetPossible
			as.currentBalance = 0
			if len(as.subAssets) > 0 {
				for _, sa := range as.subAssets {
					sa.currentBalance = 0
				}
			}
		} else {
			grossSold = requestedNet / (1.0 - penalty)
			netFulfilled = requestedNet
			as.currentBalance -= grossSold
			if len(as.subAssets) > 0 {
				totalBal := 0.0
				for _, sa := range as.subAssets {
					if !sa.isClosed {
						totalBal += sa.currentBalance
					}
				}
				if totalBal > 0 {
					remainingGross := grossSold
					for _, sa := range as.subAssets {
						if sa.isClosed {
							continue
						}
						share := sa.currentBalance / totalBal
						saGross := grossSold * share
						if saGross > sa.currentBalance {
							saGross = sa.currentBalance
						}
						sa.currentBalance -= saGross
						remainingGross -= saGross
					}
					if remainingGross > 0 {
						for _, sa := range as.subAssets {
							if !sa.isClosed && sa.currentBalance >= remainingGross {
								sa.currentBalance -= remainingGross
								break
							}
						}
					}
				}
			}
		}

		if as.penaltyAnalysis != nil {
			*as.penaltyAnalysis = append(*as.penaltyAnalysis, domain.PenaltyEvent{
				Type:          "SELL",
				Date:          as.currentMonth,
				AssetName:     as.asset.Name,
				LotID:             "STATIC",
				LotCreatedAt:      as.currentMonth,
				Amount:            grossSold,
				PrincipalSold:     grossSold,
				PenaltyPaid:       grossSold - netFulfilled,
				MonthsHeld:        0,
				InterestGenerated: 0,
			})
		}

		return grossSold, netFulfilled
	}

	// ETF FIFO
	remainingNet := requestedNet
	var newLots []etfLot

	for _, lot := range as.lots {
		if lot.currentValue <= 0 {
			continue
		}
		if remainingNet <= 0 {
			newLots = append(newLots, lot)
			continue
		}

		profitMargin := 0.0
		if lot.currentValue > lot.principal {
			profitMargin = (lot.currentValue - lot.principal) / lot.currentValue
		}

		grossNeeded := remainingNet / (1.0 - (profitMargin * penalty))

		if grossNeeded <= lot.currentValue {
			grossSold += grossNeeded
			netFulfilled += remainingNet

			fractionSold := grossNeeded / lot.currentValue
			principalSold := lot.principal * fractionSold
			penaltyPaid := grossNeeded - remainingNet

			if as.penaltyAnalysis != nil {
				*as.penaltyAnalysis = append(*as.penaltyAnalysis, domain.PenaltyEvent{
					Type:              "SELL",
					Date:              as.currentMonth,
					AssetName:         as.asset.Name,
					LotID:             lot.id,
					LotCreatedAt:      lot.createdAt,
					Amount:            grossNeeded,
					PrincipalSold:     principalSold,
					PenaltyPaid:       penaltyPaid,
					MonthsHeld:        diffMonths(lot.createdAt, as.currentMonth),
					InterestGenerated: grossNeeded - principalSold,
				})
			}

			lot.currentValue -= grossNeeded
			lot.principal -= principalSold

			remainingNet = 0
			newLots = append(newLots, lot)
		} else {
			grossSold += lot.currentValue
			netFromLot := lot.currentValue * (1.0 - (profitMargin * penalty))
			netFulfilled += netFromLot
			remainingNet -= netFromLot

			penaltyPaid := lot.currentValue - netFromLot
			if as.penaltyAnalysis != nil {
				*as.penaltyAnalysis = append(*as.penaltyAnalysis, domain.PenaltyEvent{
					Type:              "SELL",
					Date:              as.currentMonth,
					AssetName:         as.asset.Name,
					LotID:             lot.id,
					LotCreatedAt:      lot.createdAt,
					Amount:            lot.currentValue,
					PrincipalSold:     lot.principal,
					PenaltyPaid:       penaltyPaid,
					MonthsHeld:        diffMonths(lot.createdAt, as.currentMonth),
					InterestGenerated: lot.currentValue - lot.principal,
				})
			}
		}
	}
	as.lots = newLots
	// Address floating point inaccuracies
	as.currentBalance -= grossSold
	if as.asset.ActiveVersion.Type == domain.AssetTypeETF {
		withdrawFromTrackers(as, grossSold)
	}
	if as.currentBalance < 0.01 {
		as.currentBalance = 0
		as.lots = nil
	}
	return grossSold, netFulfilled
}

type loanState struct {
	loan                 domain.Loan
	currentBalance       float64
	isClosed             bool
	monthlyPayment       float64
	monthsElapsed        int
	chainIndex           int
	activeInterest       float64
	activeRuntime        int
	activeBalloon        float64
	activeIsInterestOnly bool
	isRolloverTarget     bool
}

func (s *ProjectionService) getActiveSlice(slices []domain.TimeSlice, date time.Time) *domain.TimeSlice {
	var best *domain.TimeSlice
	for i := range slices {
		slice := &slices[i]
		if (slice.StartDate.Before(date) || slice.StartDate.Equal(date)) &&
			(slice.EndDate == nil || slice.EndDate.After(date) || slice.EndDate.Equal(date)) {
			if best == nil || slice.StartDate.After(best.StartDate) {
				best = slice
			}
		}
	}
	return best
}

func (s *ProjectionService) Run(userID string, scenarioID string, onMonth func(domain.ProjectionMonth)) (*domain.ProjectionResult, error) {

	return s.RunWithLimit(userID, scenarioID, 0, onMonth)
}

func (s *ProjectionService) RunWithLimit(userID string, scenarioID string, limit int, onMonth func(domain.ProjectionMonth)) (*domain.ProjectionResult, error) {

	totalStartTime := time.Now()
	metrics := &domain.PerformanceMetrics{
		PerAssetMCMS: make(map[string]int64),
	}

	scenario, err := s.scenarioRepo.GetFull(userID, scenarioID)
	if err != nil {
		return nil, err
	}

	if limit > 0 {
		scenario.ProjectionMonths = limit
	}

	monthStartDay := scenario.MonthStartDay
	if monthStartDay <= 0 || monthStartDay > 28 {
		monthStartDay = 1
	}

	loc := s.getUserLocation(userID)

	incomes, _ := s.resolveIncomes(userID, scenario)
	bills, _ := s.resolveBills(userID, scenario)
	expenses, _ := s.resolveExpenses(userID, scenario)
	assets, _ := s.resolveAssets(userID, scenario)
	loans, _ := s.resolveLoans(userID, scenario)
	mods, _ := s.resolveModifications(userID, scenario)
	realtimeBalances, _ := s.loadRealtimeBalances(userID, monthStartDay)

	var virtualAccounts []domain.VirtualAccount
	if s.virtualAccountRepo != nil {
		virtualAccounts, _ = s.virtualAccountRepo.List(userID)
	}

	vaRunningBalances := make(map[string]float64)
	for _, va := range virtualAccounts {
		if va.ActiveVersion != nil {
			vaRunningBalances[va.ID] = va.ActiveVersion.StartingBalance
		}
	}

	metrics.ResolutionDurationMS = time.Since(totalStartTime).Milliseconds()
	mcStartTime := time.Now()

	triggeredMods := make(map[string]bool)
	simulatedYields := make(map[string]float64)
	lotCounter := 0
	penaltyAnalysis := []domain.PenaltyEvent{}

	now := time.Now().UTC()
	if scenario.StartDate != nil {
		scenarioStart := scenario.StartDate.UTC()
		if scenarioStart.After(now) {
			now = scenarioStart
		}
	}
	startDate := projectionMonthForDate(now, monthStartDay, loc)

	// Setup file logging
	logFile, err := os.OpenFile(fmt.Sprintf("logs/scenarios/%s.log", scenario.ID), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err == nil {
		defer logFile.Close()
		log.SetOutput(logFile)
		// Reset to default on exit
		defer log.SetOutput(os.Stderr)
	}

	log.Printf("[PROJECTION] Starting projection for scenario: %s (%s)", scenario.Name, scenarioID)

	// Set defaults if not configured
	if scenario.Simulations <= 0 {
		scenario.Simulations = 50000
	}
	if scenario.SimYears <= 0 {
		scenario.SimYears = 10
	}
	if scenario.SimPercent <= 0 {
		scenario.SimPercent = 50
	}

	// Initialize asset states
	assetStates := make([]*assetState, len(assets))
	for i, a := range assets {
		initialBalance := 0.0
		for _, vacctID := range a.AccountIDs {
			initialBalance += vaRunningBalances[vacctID]
		}
		state := &assetState{
			asset:           a,
			currentBalance:  initialBalance,
			lotCounter:      &lotCounter,
			penaltyAnalysis: &penaltyAnalysis,
			currentMonth:    startDate,
		}
		if a.ActiveVersion.Type == domain.AssetTypeETF && initialBalance > 0 {
			state.addLot(initialBalance)
		}
		if a.ActiveVersion.Type == domain.AssetTypeETF {
			assetMCStart := time.Now()
			for _, t := range a.ActiveVersion.ETFConfig {
				fetchTicker := t.HistoricalTracker
				if fetchTicker == "" {
					fetchTicker = t.Tracker
				}

				returns, err := s.marketData.GetHistoricalWeeklyReturns(t)
				if err != nil {
					log.Printf("[PROJECTION] ERROR: Failed to fetch historical returns for %s: %v", t.Tracker, err)
				}
				if returns != nil {
					log.Printf("[PROJECTION] Fetched %d weeks of history for %s", len(returns), fetchTicker)
					state.etfHistory = append(state.etfHistory, returns)
				}
			}
			simulations := scenario.Simulations
			simYears := scenario.SimYears
			simPercent := scenario.SimPercent
			lookbackYears := scenario.LookbackYears
			if params, ok := scenario.ETFParams[a.ID]; ok {
				if params.Simulations > 0 {
					simulations = params.Simulations
				}
				if params.SimYears > 0 {
					simYears = params.SimYears
				}
				if params.SimPercent > 0 {
					simPercent = params.SimPercent
				}
				if params.LookbackYears > 0 {
					lookbackYears = params.LookbackYears
				}
			}

			// Find Common Dates for Correlation
			var commonWeeks []string
			if len(state.etfHistory) > 0 {
				// Count occurrences of each week
				weekCounts := make(map[string]int)
				for _, returns := range state.etfHistory {
					for week := range returns {
						weekCounts[week]++
					}
				}
				
				// Keep only weeks that exist in ALL trackers
				numTrackers := len(state.etfHistory)
				for week, count := range weekCounts {
					if count == numTrackers {
						commonWeeks = append(commonWeeks, week)
					}
				}
				
				// Sort to ensure consistent ordering, mostly for lookback truncating (newest first like before)
				// We can just sort descending as ISO string e.g. 2023-W42 sorts correctly
				sort.Sort(sort.Reverse(sort.StringSlice(commonWeeks)))
				
				log.Printf("[PROJECTION] Found %d common weeks across %d trackers for %s", len(commonWeeks), numTrackers, a.Name)
			}

			var histories [][]float64
			for _, returns := range state.etfHistory {
				var alignedReturns []float64
				for _, week := range commonWeeks {
					alignedReturns = append(alignedReturns, returns[week])
				}

				if lookbackYears > 0 && len(alignedReturns) > lookbackYears*52 {
					histories = append(histories, alignedReturns[:lookbackYears*52])
				} else {
					histories = append(histories, alignedReturns)
				}
			}

			mcImplementation := scenario.MonteCarloImplementation
			if mcImplementation == "" {
				mcImplementation = "STANDARD"
			}

			log.Printf("[PROJECTION] Running Monte Carlo for asset %s (%s) with implementation %s (%d simulations, %d years, %.2f%% percent, %d history entries)...", a.Name, a.ID, mcImplementation, simulations, simYears, simPercent, len(histories))
			if mcImplementation == "SIMD" {
				state.simulatedYield = s.runMonteCarloSIMD(a.ActiveVersion, histories, simulations, simYears, simPercent)
			} else if mcImplementation == "PARALLEL" {
				state.simulatedYield = s.runMonteCarloParallel(a.ActiveVersion, histories, simulations, simYears, simPercent)
			} else {
				state.simulatedYield = s.runMonteCarlo(a.ActiveVersion, histories, simulations, simYears, simPercent)
			}
			simulatedYields[a.ID] = state.simulatedYield * 100
			log.Printf("[PROJECTION] Monte Carlo result for %s: %.2f%%", a.Name, simulatedYields[a.ID])

			state.trackerBalances = make(map[string]float64)
			state.trackerYields = make(map[string]float64)
			for idx, t := range a.ActiveVersion.ETFConfig {
				state.trackerBalances[t.Tracker] = state.currentBalance * t.Percentage
				var history []float64
				if idx < len(histories) {
					history = histories[idx]
				}

				var yield float64
				if mcImplementation == "SIMD" {
					yield = s.runTrackerMonteCarloSIMD(history, t.TER, simulations, simYears, simPercent)
				} else if mcImplementation == "PARALLEL" {
					yield = s.runTrackerMonteCarloParallel(history, t.TER, simulations, simYears, simPercent)
				} else {
					yield = s.runTrackerMonteCarlo(history, t.TER, simulations, simYears, simPercent)
				}

				state.trackerYields[t.Tracker] = yield
				simulatedYields[a.ID+"_"+t.Tracker] = yield * 100
				log.Printf("[PROJECTION]   - Tracker %s: %.2f%%", t.Tracker, simulatedYields[a.ID+"_"+t.Tracker])
			}
			metrics.PerAssetMCMS[a.ID] = time.Since(assetMCStart).Milliseconds()
		}

		if len(a.ActiveVersion.SubAssets) > 0 {
			state.subAssets = make([]*subAssetState, len(a.ActiveVersion.SubAssets))
			for idx, sa := range a.ActiveVersion.SubAssets {
				saRate := sa.AmountPerMonth
				if saRate <= 0 && sa.EndDate != nil {
					interestRateUsed := a.ActiveVersion.InterestRate
					if a.ActiveVersion.Type == domain.AssetTypeETF {
						interestRateUsed = state.simulatedYield * 100
					}
					saRate = s.calculateSubAssetRequiredRate(sa, interestRateUsed, loans)
				}
				state.subAssets[idx] = &subAssetState{
					id:                  sa.ID,
					name:                sa.Name,
					currentBalance:      0,
					targetValue:         sa.TargetValue,
					amountPerMonth:      sa.AmountPerMonth,
					isRemainderConsumer: sa.IsRemainderConsumer,
					remainderStartDate:  sa.RemainderStartDate,
					dumpingLoanID:       sa.DumpingLoanID,
					startDate:           sa.StartDate,
					endDate:             sa.EndDate,
					earliestDumpDate:    sa.EarliestDumpDate,
				}
			}
		}
		log.Printf("[PROJECTION] Asset: %s, Target: %s, DumpingLoan: %v, Rate: %.2f", a.Name, a.ActiveVersion.TargetValue, a.ActiveVersion.DumpingLoanID, a.ActiveVersion.AmountPerMonth)
		if a.ActiveVersion.AmountPerMonth <= 0 && a.ActiveVersion.EndDate != nil && len(a.ActiveVersion.SubAssets) == 0 {
			rateUsed := a.ActiveVersion.InterestRate
			if a.ActiveVersion.Type == domain.AssetTypeETF {
				rateUsed = state.simulatedYield * 100
			}
			a.ActiveVersion.AmountPerMonth = s.calculateRequiredRate(a.ActiveVersion, rateUsed, loans)
		}
		assetStates[i] = state
	}

	metrics.MonteCarloDurationMS = time.Since(mcStartTime).Milliseconds()
	catchupStartTime := time.Now()

	// Initialize loan states
	isRolloverTarget := make(map[string]bool)
	for _, l := range loans {
		if l.ActiveVersion.NextLoanID != nil {
			isRolloverTarget[*l.ActiveVersion.NextLoanID] = true
		}
	}

	loanStates := make([]*loanState, len(loans))
	loanByID := make(map[string]*loanState)
	for i, l := range loans {
		v := l.ActiveVersion
		initialBalance := v.AmountLent
		isClosed := false
		if isRolloverTarget[l.ID] {
			log.Printf("[PROJECTION] Loan %s identified as Rollover Target. Ignoring placeholder principal %.2f", l.Name, initialBalance)
			initialBalance = 0
			isClosed = true
		}

		state := &loanState{
			loan:                 l,
			currentBalance:       initialBalance,
			isClosed:             isClosed,
			activeInterest:       v.InterestRate,
			activeRuntime:        v.RuntimeMonths,
			activeBalloon:        v.BalloonLeftover,
			activeIsInterestOnly: v.IsInterestOnly,
			isRolloverTarget:     isRolloverTarget[l.ID],
		}
		state.monthlyPayment = s.calculateLoanPayment(state.currentBalance, state.activeInterest, state.activeRuntime, state.activeBalloon, state.activeIsInterestOnly)
		loanStates[i] = state
		loanByID[l.ID] = state
	}

	// --- CATCH UP PHASE ---
	// Calculate true balances by simulating from original StartDate up to startDate
	log.Printf("[PROJECTION] Catching up historical data from entity start dates to %s", startDate.Format("01/2006"))
	for curr := s.findEarliestStart(assets, loans, monthStartDay, loc); curr.Before(startDate); curr = curr.AddDate(0, 1, 0) {
		for _, as := range assetStates {
			as.currentMonth = curr
		}
		// 1. Process Asset Interest/Returns
		for _, as := range assetStates {
			if as.isClosed {
				continue
			}

			v := as.asset.ActiveVersion
			if !s.isDateActive(v.StartDate, v.EndDate, curr) {
				continue
			}

			// Interest (Non-ETF only, ETF processed at end of month)
			interestPenaltyRate := getInterestPenaltyRate(v)

			if v.Type != domain.AssetTypeETF {
				monthlyRate := (v.InterestRate / 100.0) / 12.0
				grossInterestEarned := as.currentBalance * monthlyRate
				interestPenaltyPaid := 0.0
				if grossInterestEarned > 0 {
					interestPenaltyPaid = grossInterestEarned * interestPenaltyRate
				}
				netInterestEarned := grossInterestEarned - interestPenaltyPaid

				if v.InterestInterval == "Monthly" {
					depositAssetProportionally(as, netInterestEarned, nil)
				} else {
					as.accruedInterest += netInterestEarned
					if curr.Month() == 12 {
						depositAssetProportionally(as, as.accruedInterest, nil)
						as.accruedInterest = 0
					}
				}
			}
		}

		// 1.2 Process Asset Modifications (Aggregate Aware)
		for _, m := range mods {
			if !s.isActiveAt(m.ActiveVersion.StartDate, m.ActiveVersion.EndDate, curr, m.ActiveVersion.IntervalMonths) {
				continue
			}

			if m.TargetType == "ASSET" {
				var targets []*assetState
				totalBalance := 0.0
				for _, as := range assetStates {
					if as.isClosed {
						continue
					}
					isTarget := m.TargetID == as.asset.ID
					if !isTarget {
						for _, tid := range m.TargetIDs {
							if tid == as.asset.ID {
								isTarget = true
								break
							}
						}
					}
					if isTarget {
						targets = append(targets, as)
						totalBalance += as.currentBalance
					}
				}

				if len(targets) == 0 {
					continue
				}

				if m.ActiveVersion.WithdrawalPercentage > 0 {
					// Dynamic aggregate withdrawal
					swrAmt := totalBalance * (m.ActiveVersion.WithdrawalPercentage / 100.0 / 12.0)
					if swrAmt >= m.ActiveVersion.Amount {
						triggeredMods[m.ID] = true
						toWithdrawTotal := m.ActiveVersion.Amount
						if toWithdrawTotal <= 0 {
							toWithdrawTotal = swrAmt
						}

						// Distribute proportionally across targets
						remainingToWithdraw := toWithdrawTotal
						for idx, as := range targets {
							share := 1.0 / float64(len(targets))
							if totalBalance > 0 {
								share = as.currentBalance / totalBalance
							}

							amt := toWithdrawTotal * share
							if idx == len(targets)-1 {
								amt = remainingToWithdraw
							}

							if amt > 0 {
								_, netFulfilled := withdrawAsset(as, amt)
								remainingToWithdraw -= netFulfilled
							}
						}
					}
				} else {
					// Fixed amount mod - apply to each (standard behavior)
					for _, as := range targets {
						amount := m.ActiveVersion.Amount
						if amount > 0 {
							depositAsset(as, amount, loanByID)
							triggeredMods[m.ID] = true
						} else {
							withdrawAsset(as, -amount)
							triggeredMods[m.ID] = true
						}
					}
				}
			}
		}

		// 1.5 Synchronize termination triggers
		for _, as := range assetStates {
			if !as.contributionsStopped && as.asset.ActiveVersion.StopModificationID != nil && triggeredMods[*as.asset.ActiveVersion.StopModificationID] {
				log.Printf("[PROJECTION] [CATCH-UP] Stopping contributions for asset %s: Linked modification %s triggered.", as.asset.Name, *as.asset.ActiveVersion.StopModificationID)
				as.contributionsStopped = true
			}
		}

		// 1.6 Contributions
		for _, as := range assetStates {
			if as.isClosed || as.contributionsStopped {
				continue
			}

			v := as.asset.ActiveVersion
			if !s.isDateActive(v.StartDate, v.EndDate, curr) {
				continue
			}

			if len(as.subAssets) > 0 {
				for _, sa := range as.subAssets {
					if sa.isClosed {
						continue
					}
					if !s.isDateActive(sa.startDate, sa.endDate, curr) {
						continue
					}
					if sa.amountPerMonth > 0 {
						target := getSubAssetTarget(sa, loanByID)
						contribution := sa.amountPerMonth
						if target >= 0 {
							contribution = math.Min(contribution, math.Max(0, target-sa.currentBalance))
						}
						depositToSubAsset(as, sa.id, contribution)
					}
				}
			} else if v.AmountPerMonth > 0 {
				contribution := v.AmountPerMonth
				target, err := strconv.ParseFloat(v.TargetValue, 64)
				if err != nil {
					var ls *loanState
					var ok bool
					if v.DumpingLoanID != nil {
						ls, ok = loanByID[*v.DumpingLoanID]
					}

					if ok {
						penalty := ls.loan.ActiveVersion.EarlyPayoffPenalty / 100.0
						if penalty < 0 {
							penalty = 0.01
						}
						target = ls.currentBalance / (1.0 - penalty)
						err = nil
					}
				}

				if err == nil && target > 0 {
					contribution = math.Min(contribution, math.Max(0, target-as.currentBalance))
				}

				depositAsset(as, contribution, loanByID)
			}
		}

		// 2. Process Loan Payments
		for _, ls := range loanStates {
			v := ls.loan.ActiveVersion
			if !s.isDateActive(v.StartDate, nil, curr) {
				continue
			}

			// Apply modifications
			for _, m := range mods {
				if m.TargetID == ls.loan.ID && m.TargetType == "LOAN" {
					if s.isActiveAt(m.ActiveVersion.StartDate, m.ActiveVersion.EndDate, curr, m.ActiveVersion.IntervalMonths) {
						ls.currentBalance -= m.ActiveVersion.Amount
						triggeredMods[m.ID] = true
					}
				}
			}

			monthlyRate := (ls.activeInterest / 100.0) / 12.0
			interest := ls.currentBalance * monthlyRate
			payment := ls.monthlyPayment
			if ls.activeIsInterestOnly {
				payment = interest
			}

			if ls.currentBalance+interest < payment {
				payment = ls.currentBalance + interest
			}

			principal := payment - interest
			ls.currentBalance -= principal
			ls.monthsElapsed++

			if ls.monthsElapsed >= ls.activeRuntime {
				if ls.activeBalloon > 0 && ls.loan.ActiveVersion.NextLoanID == nil {
					ls.currentBalance = 0
					ls.isClosed = true
				} else if ls.currentBalance <= 0.05 {
					ls.currentBalance = 0
					ls.isClosed = true
				} else {
					// Check for 1:1 NextLoanID first
					if ls.loan.ActiveVersion.NextLoanID != nil {
						if nextLS, ok := loanByID[*ls.loan.ActiveVersion.NextLoanID]; ok {
							vNext := nextLS.loan.ActiveVersion

							// TRANSFER BALANCE TO TARGET STATE (OVERWRITE)
							nextLS.currentBalance = ls.currentBalance
							nextLS.isClosed = false
							nextLS.monthsElapsed = 0

							// Synchronize parameters from target loan config
							nextLS.activeInterest = vNext.InterestRate
							nextLS.activeRuntime = vNext.RuntimeMonths
							nextLS.activeBalloon = vNext.BalloonLeftover
							nextLS.activeIsInterestOnly = vNext.IsInterestOnly

							// Recalculate payment for target loan using its own config but the new balance
							nextLS.monthlyPayment = s.calculateLoanPayment(nextLS.currentBalance, nextLS.activeInterest, nextLS.activeRuntime, nextLS.activeBalloon, nextLS.activeIsInterestOnly)

							log.Printf("[PROJECTION] LOAN ROLLOVER (1:1): %s balance (%.2f) transferred to %s. New Payment: %.2f", ls.loan.Name, ls.currentBalance, nextLS.loan.Name, nextLS.monthlyPayment)
							ls.isClosed = true
							ls.currentBalance = 0
						} else {
							ls.currentBalance = 0
							ls.isClosed = true
						}
					} else {
						ls.currentBalance = 0
						ls.isClosed = true
					}
				}
			}
		}

		// 3. Cleanup orphaned dumping assets
		for _, as := range assetStates {
			if as.isClosed {
				continue
			}
			v := as.asset.ActiveVersion
			if v.DumpingLoanID != nil {
				ls, ok := loanByID[*v.DumpingLoanID]
				if ok && ls.isClosed {
					// Only orphan if it's not a rollover target waiting for its turn
					if ls.isRolloverTarget && ls.monthsElapsed == 0 {
						continue
					}

					as.isClosed = true
					as.currentBalance = 0 // In catch-up we don't have a budget to release to, so we just clear it
				}
			}
		}

		// 3.5 Process ETF Asset Interest/Returns (At the end of the catch-up month after modifications and contributions)
		for _, as := range assetStates {
			if as.isClosed {
				continue
			}

			v := as.asset.ActiveVersion
			if v.Type != domain.AssetTypeETF {
				continue
			}

			if !s.isDateActive(v.StartDate, v.EndDate, curr) {
				continue
			}

			interestPenaltyRate := getInterestPenaltyRate(v)
			monthlyRate := math.Pow(1.0+as.simulatedYield, 1.0/12.0) - 1.0
			var newBal float64

			isDistributing := v.InterestInterval == "Monthly"

			if monthlyRate > 0 {
				for i := range as.lots {
					grossGrowth := as.lots[i].currentValue * monthlyRate
					if !isDistributing {
						netGrowth := grossGrowth * (1.0 - interestPenaltyRate)
						as.lots[i].currentValue += netGrowth
					}
					newBal += as.lots[i].currentValue
				}
			} else {
				for i := range as.lots {
					as.lots[i].currentValue *= (1.0 + monthlyRate)
					newBal += as.lots[i].currentValue
				}
			}

			oldBal := as.currentBalance
			as.currentBalance = newBal
			netGrowth := newBal - oldBal

			if isDistributing && monthlyRate > 0 {
				totalGrossGrowth := 0.0
				for i := range as.lots {
					totalGrossGrowth += as.lots[i].currentValue * monthlyRate
				}
				interestPenaltyPaid := totalGrossGrowth * interestPenaltyRate

				if as.penaltyAnalysis != nil {
					*as.penaltyAnalysis = append(*as.penaltyAnalysis, domain.PenaltyEvent{
						Type:              "SELL",
						Date:              as.currentMonth,
						AssetName:         as.asset.Name,
						LotID:             "DIVIDEND",
						LotCreatedAt:      as.currentMonth,
						Amount:            totalGrossGrowth,
						PrincipalSold:     0,
						PenaltyPaid:       interestPenaltyPaid,
						MonthsHeld:        0,
						InterestGenerated: totalGrossGrowth,
					})
				}
			}

			totalTrackersNew := 0.0
			for tracker, yield := range as.trackerYields {
				mRate := math.Pow(1.0+yield, 1.0/12.0) - 1.0
				bal := as.trackerBalances[tracker]
				if mRate > 0 {
					if !isDistributing {
						grossGrowth := bal * mRate
						netGrowthVal := grossGrowth * (1.0 - interestPenaltyRate)
						as.trackerBalances[tracker] += netGrowthVal
					}
				} else {
					as.trackerBalances[tracker] *= (1.0 + mRate)
				}
				totalTrackersNew += as.trackerBalances[tracker]
			}
			if totalTrackersNew > 0 && as.currentBalance > 0 {
				scale := as.currentBalance / totalTrackersNew
				for tracker := range as.trackerBalances {
					as.trackerBalances[tracker] *= scale
				}
			} else if as.currentBalance <= 0 {
				for tracker := range as.trackerBalances {
					as.trackerBalances[tracker] = 0
				}
			}

			if !isDistributing && len(as.subAssets) > 0 && netGrowth != 0 {
				totalBal := 0.0
				for _, sa := range as.subAssets {
					if !sa.isClosed {
						totalBal += sa.currentBalance
					}
				}
				if totalBal > 0 {
					remaining := netGrowth
					for _, sa := range as.subAssets {
						if sa.isClosed {
							continue
						}
						share := sa.currentBalance / totalBal
						toDep := netGrowth * share
						sa.currentBalance += toDep
						remaining -= toDep
					}
					if math.Abs(remaining) > 0.00001 && len(as.subAssets) > 0 {
						as.subAssets[0].currentBalance += remaining
					}
				} else {
					as.subAssets[0].currentBalance += netGrowth
				}
			}
		}
	}

	for _, as := range assetStates {
		log.Printf("[PROJECTION] Initial Asset Balance (%s): %.2f", as.asset.Name, as.currentBalance)
	}
	for _, ls := range loanStates {
		log.Printf("[PROJECTION] Initial Loan Balance (%s): %.2f", ls.loan.Name, ls.currentBalance)
	}

	metrics.CatchupDurationMS = time.Since(catchupStartTime).Milliseconds()
	projectionStartTime := time.Now()

	result := &domain.ProjectionResult{
		Months:          make([]domain.ProjectionMonth, scenario.ProjectionMonths),
		SimulatedYields: simulatedYields,
	}

	unassignedRunningBalance := 0.0
	currentBalance := 0.0
	for i := 0; i < scenario.ProjectionMonths; i++ {
		currentDate := startDate.AddDate(0, i, 0)
		for _, as := range assetStates {
			as.currentMonth = currentDate
		}
		periodStart, periodEnd := projectionPeriodBounds(currentDate, monthStartDay, loc)
		log.Printf("[PROJECTION] --- MONTH: %s ---", currentDate.Format("01/2006"))
		month := domain.ProjectionMonth{
			Date:        currentDate,
			PeriodStart: periodStart,
			PeriodEnd:   periodEnd,
			Breakdown: domain.MonthBreakdown{
				Incomes:  []domain.EntryBreakdown{},
				Bills:    []domain.EntryBreakdown{},
				Expenses: []domain.EntryBreakdown{},
				Assets:   []domain.EntryBreakdown{},
				Loans:    []domain.EntryBreakdown{},
			},
		}

		// 1. RESOLVE BASE INCOMES
		for _, inc := range incomes {
			v := inc.ActiveVersion
			amount := v.Amount
			interval := v.IntervalMonths
			description := ""

			if slice := s.getActiveSlice(v.Slices, currentDate); slice != nil {
				amount = slice.Value
				interval = slice.IntervalMonths
				description = slice.Description
			}

			if s.isActiveAt(v.StartDate, v.EndDate, currentDate, interval) {
				if v.StopModificationID != nil && triggeredMods[*v.StopModificationID] {
					log.Printf("[PROJECTION] Skipping income %s: Linked modification %s triggered.", inc.Name, *v.StopModificationID)
					continue
				}

				// Apply interval increase
				if v.IntervalIncreasePercentage > 0 && v.IntervalIncreaseMonths > 0 && v.IntervalIncreaseStartDate != nil {
					if !currentDate.Before(*v.IntervalIncreaseStartDate) {
						y1, m1, _ := v.IntervalIncreaseStartDate.Date()
						y2, m2, _ := currentDate.Date()
						totalMonths := int(y2-y1)*12 + int(m2-m1)

						intervals := totalMonths / v.IntervalIncreaseMonths
						if intervals > 0 {
							amount = amount * math.Pow(1.0+(v.IntervalIncreasePercentage/100.0), float64(intervals))
						}
					}
				}

				month.Income += amount
				log.Printf("[PROJECTION] Income: %s, Amount: %.2f", inc.Name, amount)

				displayName := inc.Name
				if description != "" {
					displayName = fmt.Sprintf("%s (%s)", inc.Name, description)
				}

				month.Breakdown.Incomes = append(month.Breakdown.Incomes, domain.EntryBreakdown{
					Name:       displayName,
					Amount:     amount,
					AccountIDs: inc.AccountIDs,
					PoolID:     inc.PoolID,
				})
			}
		}

		// 2. RESOLVE BILLS & ONE-TIME EXPENSES
		for _, b := range bills {
			v := b.ActiveVersion
			amount := v.Amount
			interval := v.IntervalMonths
			description := ""

			if slice := s.getActiveSlice(v.Slices, currentDate); slice != nil {
				amount = slice.Value
				interval = slice.IntervalMonths
				description = slice.Description
			}

			if s.isActiveAt(v.StartDate, v.EndDate, currentDate, interval) {
				month.Bills += amount
				log.Printf("[PROJECTION] Bill: %s, Amount: %.2f", b.Name, amount)

				displayName := b.Name
				if description != "" {
					displayName = fmt.Sprintf("%s (%s)", b.Name, description)
				}

				month.Breakdown.Bills = append(month.Breakdown.Bills, domain.EntryBreakdown{
					Name:       displayName,
					Amount:     amount,
					AccountIDs: b.AccountIDs,
					PoolID:     b.PoolID,
				})
			}
		}

		for _, e := range expenses {
			v := e.ActiveVersion
			if len(v.Slices) > 0 {
				if slice := s.getActiveSlice(v.Slices, currentDate); slice != nil {
					if s.isActiveAt(slice.StartDate, slice.EndDate, currentDate, slice.IntervalMonths) {
						month.Expenses += slice.Value
						log.Printf("[PROJECTION] Expense (Slice): %s, Amount: %.2f", e.Name, slice.Value)

						displayName := e.Name
						if slice.Description != "" {
							displayName = fmt.Sprintf("%s (%s)", e.Name, slice.Description)
						}

						month.Breakdown.Expenses = append(month.Breakdown.Expenses, domain.EntryBreakdown{
							Name:       displayName,
							Amount:     slice.Value,
							AccountIDs: e.AccountIDs,
							PoolID:     e.PoolID,
						})
					}
				}
			} else {
				uDue := v.DueDate.UTC()
				start, end := projectionPeriodBounds(currentDate, scenario.MonthStartDay, loc)
				if (uDue.Equal(start) || uDue.After(start)) && uDue.Before(end) {
					month.Expenses += v.Amount
					log.Printf("[PROJECTION] Expense: %s, Amount: %.2f", e.Name, v.Amount)
					month.Breakdown.Expenses = append(month.Breakdown.Expenses, domain.EntryBreakdown{
						Name:       e.Name,
						Amount:     v.Amount,
						AccountIDs: e.AccountIDs,
						PoolID:     e.PoolID,
					})
				}
			}
		}

		// Initialize available funds
		availableFunds := month.Income - month.Bills - month.Expenses
		log.Printf("[PROJECTION] Available Funds (Initial): %.2f", availableFunds)

		// 3. APPLY INTEREST/RETURNS (Non-ETF only, ETF processed at end of month)
		for _, as := range assetStates {
			if as.isClosed {
				continue
			}

			v := as.asset.ActiveVersion

			// Interest/Returns
			interestEarned := 0.0
			interestPenaltyPaid := 0.0
			interestPenaltyRate := getInterestPenaltyRate(v)

			if v.Type != domain.AssetTypeETF {
				monthlyRate := (v.InterestRate / 100.0) / 12.0
				grossInterestEarned := as.currentBalance * monthlyRate

				if grossInterestEarned > 0 {
					interestPenaltyPaid = grossInterestEarned * interestPenaltyRate
				}
				netInterestEarned := grossInterestEarned - interestPenaltyPaid

				interestEarned = grossInterestEarned

				if v.InterestInterval == "Monthly" {
					depositAssetProportionally(as, netInterestEarned, nil)
				} else {
					as.accruedInterest += netInterestEarned
					if currentDate.Month() == 12 {
						depositAssetProportionally(as, as.accruedInterest, nil)
						log.Printf("[PROJECTION] Yearly Interest for %s: %.2f", as.asset.Name, as.accruedInterest)
						as.accruedInterest = 0
					}
				}
			}

			if interestEarned != 0 || interestPenaltyPaid != 0 {
				month.Breakdown.Assets = append(month.Breakdown.Assets, buildAssetBreakdownEntry(as, as.asset.Name, 0, interestEarned, interestPenaltyPaid))
			}
		}

		// 3.2 APPLY MODIFICATIONS (Aggregate Aware)
		for _, m := range mods {
			if !s.isActiveAt(m.ActiveVersion.StartDate, m.ActiveVersion.EndDate, currentDate, m.ActiveVersion.IntervalMonths) {
				continue
			}

			if m.TargetType == "ASSET" {
				var targets []*assetState
				totalBalance := 0.0
				for _, as := range assetStates {
					if as.isClosed {
						continue
					}
					isTarget := m.TargetID == as.asset.ID
					if !isTarget {
						for _, tid := range m.TargetIDs {
							if tid == as.asset.ID {
								isTarget = true
								break
							}
						}
					}
					if isTarget {
						targets = append(targets, as)
						totalBalance += as.currentBalance
					}
				}

				if len(targets) == 0 {
					continue
				}

				if m.ActiveVersion.WithdrawalPercentage > 0 {
					// Dynamic aggregate withdrawal
					swrAmt := totalBalance * (m.ActiveVersion.WithdrawalPercentage / 100.0 / 12.0)
					if swrAmt >= m.ActiveVersion.Amount {
						triggeredMods[m.ID] = true
						toWithdrawTotal := m.ActiveVersion.Amount
						if toWithdrawTotal <= 0 {
							toWithdrawTotal = swrAmt
						}

						// Distribute proportionally across targets
						remainingToWithdraw := toWithdrawTotal
						for idx, as := range targets {
							share := 1.0 / float64(len(targets))
							if totalBalance > 0 {
								share = as.currentBalance / totalBalance
							}

							amt := toWithdrawTotal * share
							if idx == len(targets)-1 {
								amt = remainingToWithdraw
							}

							if amt > 0 {
								grossSold, netFulfilled := withdrawAsset(as, amt)
								impactOnFunds := -netFulfilled
								penaltyPaid := grossSold - netFulfilled

								month.Assets += impactOnFunds
								availableFunds -= impactOnFunds
								description := fmt.Sprintf("%s (%.2f%% Dynamic Agg.)", m.Description, m.ActiveVersion.WithdrawalPercentage)
								log.Printf("[PROJECTION] Asset Mod: %s -> %s, Funds Impact: %.2f, New Balance: %.2f", description, as.asset.Name, impactOnFunds, as.currentBalance)
								month.Breakdown.Assets = append(month.Breakdown.Assets, buildAssetBreakdownEntry(as, description+" (Mod)", impactOnFunds, 0, penaltyPaid))

								remainingToWithdraw -= netFulfilled
							}
						}
					}
				} else {
					// Fixed amount mod - apply to each (standard behavior)
					for _, as := range targets {
						amount := m.ActiveVersion.Amount
						impactOnFunds := 0.0
						penaltyPaid := 0.0

						if amount < 0 {
							grossSold, netFulfilled := withdrawAsset(as, -amount)
							impactOnFunds = -netFulfilled
							penaltyPaid = grossSold - netFulfilled
						} else {
							impactOnFunds = depositAsset(as, amount, loanByID)
						}

						if impactOnFunds != 0 || penaltyPaid != 0 {
							month.Assets += impactOnFunds
							availableFunds -= impactOnFunds
							log.Printf("[PROJECTION] Asset Mod: %s -> %s, Funds Impact: %.2f, New Balance: %.2f", m.Description, as.asset.Name, impactOnFunds, as.currentBalance)
							month.Breakdown.Assets = append(month.Breakdown.Assets, buildAssetBreakdownEntry(as, m.Description+" (Mod)", impactOnFunds, 0, penaltyPaid))
							triggeredMods[m.ID] = true
						}
					}
				}
			} else if m.TargetType == "LOAN" {
				ls, ok := loanByID[m.TargetID]
				if ok && !ls.isClosed {
					if s.isActiveAt(m.ActiveVersion.StartDate, m.ActiveVersion.EndDate, currentDate, m.ActiveVersion.IntervalMonths) {
						ls.currentBalance -= m.ActiveVersion.Amount
						triggeredMods[m.ID] = true
						log.Printf("[PROJECTION] Loan Mod: %s -> %s, Amount: %.2f, New Balance: %.2f", m.Description, ls.loan.Name, m.ActiveVersion.Amount, ls.currentBalance)

						// Recalculate if not closed
						if ls.currentBalance <= 0.05 {
							ls.currentBalance = 0
							ls.isClosed = true
						} else {
							ls.monthlyPayment = s.calculateLoanPayment(ls.currentBalance, ls.activeInterest, ls.activeRuntime-ls.monthsElapsed, ls.activeBalloon, ls.activeIsInterestOnly)
						}
					}
				}
			}
		}

		// 3.5 Synchronize termination triggers
		for _, as := range assetStates {
			if !as.contributionsStopped && as.asset.ActiveVersion.StopModificationID != nil && triggeredMods[*as.asset.ActiveVersion.StopModificationID] {
				log.Printf("[PROJECTION] Stopping contributions for asset %s: Linked modification %s triggered.", as.asset.Name, *as.asset.ActiveVersion.StopModificationID)
				as.contributionsStopped = true
			}
		}

		// 4. LOAN DUMPING (Aggregate-Aware)
		for _, as := range assetStates {
			if as.isClosed {
				continue
			}
			v := as.asset.ActiveVersion

			// 4.1 Collect all unique loans targeted for dumping by this asset or its sub-assets
			targets := make(map[string]*loanState)
			if v.DumpingLoanID != nil {
				if ls, ok := loanByID[*v.DumpingLoanID]; ok && !ls.isClosed {
					targets[ls.loan.ID] = ls
				}
			}
			for _, sa := range as.subAssets {
				if !sa.isClosed && sa.dumpingLoanID != nil && s.isActiveAt(sa.startDate, sa.endDate, currentDate, 1) {
					if sa.earliestDumpDate == nil || !currentDate.Before(*sa.earliestDumpDate) {
						if ls, ok := loanByID[*sa.dumpingLoanID]; ok && !ls.isClosed {
							targets[ls.loan.ID] = ls
						}
					}
				}
			}

			// 4.2 Check if any target loan can be fully paid off by the total asset balance
			for _, ls := range targets {
				loanPenalty := ls.loan.ActiveVersion.EarlyPayoffPenalty / 100.0
				if loanPenalty < 0 {
					loanPenalty = 0.01
				}

				cashNeeded := ls.currentBalance / (1.0 - loanPenalty)
				totalMaxNet := calculateMaxNet(as)

				if totalMaxNet >= cashNeeded {
					// We can kill this loan!
					// Prioritize withdrawal from sub-assets that were saving for THIS loan
					remainingNetToWithdraw := cashNeeded
					totalPenaltyPaid := 0.0

					// Pass 1: Sub-assets linked to THIS loan
					borrowedFrom := make(map[string]float64)
					for _, sa := range as.subAssets {
						if !sa.isClosed && sa.dumpingLoanID != nil && *sa.dumpingLoanID == ls.loan.ID && s.isActiveAt(sa.startDate, sa.endDate, currentDate, 1) {
							if sa.earliestDumpDate == nil || !currentDate.Before(*sa.earliestDumpDate) {
								maxNetSA := calculateMaxNetForSubAsset(as, sa)
								toWithdraw := math.Min(remainingNetToWithdraw, maxNetSA)
								if toWithdraw > 0 {
									gross, net := withdrawFromSubAsset(as, sa.id, toWithdraw)
									totalPenaltyPaid += (gross - net)
									remainingNetToWithdraw -= net
								}
							}
						}
					}

					// Pass 2: Any other active sub-assets (rebalance to prio dump)
					if remainingNetToWithdraw > 0.01 {
						for _, sa := range as.subAssets {
							if !sa.isClosed && (sa.dumpingLoanID == nil || *sa.dumpingLoanID != ls.loan.ID) && s.isActiveAt(sa.startDate, sa.endDate, currentDate, 1) {
								if sa.earliestDumpDate == nil || !currentDate.Before(*sa.earliestDumpDate) {
									maxNetSA := calculateMaxNetForSubAsset(as, sa)
									toWithdraw := math.Min(remainingNetToWithdraw, maxNetSA)
									if toWithdraw > 0 {
										gross, net := withdrawFromSubAsset(as, sa.id, toWithdraw)
										totalPenaltyPaid += (gross - net)
										remainingNetToWithdraw -= net
										borrowedFrom[sa.name] += net
									}
								}
							}
						}
					}

					// Pass 3: Asset balance (no sub-assets) if still needed
					if remainingNetToWithdraw > 0.01 {
						gross, net := withdrawAsset(as, remainingNetToWithdraw)
						totalPenaltyPaid += (gross - net)
						remainingNetToWithdraw -= net
					}

					// Finalize Loan Payoff
					effectivePayoff := (cashNeeded - remainingNetToWithdraw) * (1.0 - loanPenalty)
					ls.currentBalance -= effectivePayoff
					netWithdrawn := cashNeeded - remainingNetToWithdraw

					log.Printf("[PROJECTION] AGGREGATE LOAN DUMP: %s killed by %s, Effective Payoff: %.2f, Net Withdrawn: %.2f", ls.loan.Name, as.asset.Name, effectivePayoff, netWithdrawn)

					month.Loans += netWithdrawn
					month.Assets -= netWithdrawn

					month.Breakdown.Loans = append(month.Breakdown.Loans, domain.EntryBreakdown{
						Name:       ls.loan.Name + " (Dump)",
						EntityName: ls.loan.Name,
						Amount:     netWithdrawn,
						Balance:    ls.currentBalance,
						AccountIDs: ls.loan.AccountIDs,
						PoolID:     ls.loan.PoolID,
					})
					month.Breakdown.Assets = append(month.Breakdown.Assets, buildAssetBreakdownEntry(as, as.asset.Name+" (Aggregate Dump)", -netWithdrawn, 0, totalPenaltyPaid))

					// Generate Rebalance IOUs for borrowed sub-asset funds
					for sourceName, amt := range borrowedFrom {
						// Find the source sub-asset to copy its EndDate
						var sourceEndDate *time.Time
						for _, ssa := range as.subAssets {
							if ssa.name == sourceName {
								sourceEndDate = ssa.endDate
								break
							}
						}

						newSA := &subAssetState{
							id:                  uuid.New().String(),
							name:                sourceName + " rebalance",
							targetValue:         fmt.Sprintf("%.2f", amt),
							isRemainderConsumer: true,
							startDate:           currentDate,
							endDate:             sourceEndDate,
						}
						as.subAssets = append(as.subAssets, newSA)
						log.Printf("[PROJECTION] Generated rebalance IOU for %s: %.2f (Ends: %v)", sourceName, amt, sourceEndDate)
					}

					if ls.currentBalance <= 0.05 {
						log.Printf("[PROJECTION] LOAN CLOSED by DUMP: %s", ls.loan.Name)
						ls.currentBalance = 0
						ls.isClosed = true
					}

					// Special behavior: If the parent asset has a main DumpingLoanID and THAT loan was closed,
					// release the whole asset to budget.
					if v.DumpingLoanID != nil && *v.DumpingLoanID == ls.loan.ID {
						if as.currentBalance > 0 {
							leftoverGross, leftoverNet := withdrawAsset(as, calculateMaxNet(as))
							penaltyPaid := leftoverGross - leftoverNet

							month.Income += leftoverNet
							availableFunds += leftoverNet
							month.Breakdown.Incomes = append(month.Breakdown.Incomes, domain.EntryBreakdown{
								Name:       as.asset.Name + " (Leftover after Aggregate Dump)",
								Amount:     leftoverNet,
								Penalty:    penaltyPaid,
								AccountIDs: as.asset.AccountIDs,
								PoolID:     as.asset.PoolID,
							})
						}
						as.isClosed = true
						break // Asset is dead
					}

					// If it was a sub-asset link, just close the affected sub-assets
					for _, sa := range as.subAssets {
						if !sa.isClosed && sa.dumpingLoanID != nil && *sa.dumpingLoanID == ls.loan.ID {
							if sa.currentBalance > 0 {
								maxNetLeftover := calculateMaxNetForSubAsset(as, sa)
								leftoverGross, leftoverNet := withdrawFromSubAsset(as, sa.id, maxNetLeftover)
								penaltyPaidLeftover := leftoverGross - leftoverNet

								month.Income += leftoverNet
								availableFunds += leftoverNet
								month.Breakdown.Incomes = append(month.Breakdown.Incomes, domain.EntryBreakdown{
									Name:       as.asset.Name + " (" + sa.name + " Leftover after Dump)",
									Amount:     leftoverNet,
									Penalty:    penaltyPaidLeftover,
									AccountIDs: as.asset.AccountIDs,
									PoolID:     as.asset.PoolID,
								})
							}
							sa.isClosed = true
						}
					}
				}
			}
		}

		// 5. ASSET PAYOUTS (Assets that reach EndDate payout into budget)
		for _, as := range assetStates {
			if as.isClosed {
				continue
			}
			v := as.asset.ActiveVersion

			// Check individual sub-asset EndDates
			if len(as.subAssets) > 0 {
				for _, sa := range as.subAssets {
					if sa.isClosed || sa.endDate == nil {
						continue
					}
					saEnd := sa.endDate.UTC()
					if currentDate.Year() == saEnd.Year() && currentDate.Month() == saEnd.Month() {
						log.Printf("[PROJECTION] SUB-ASSET PAYOUT DATE REACHED: %s (%s)", as.asset.Name, sa.name)

						// Pay off dumping loan if configured and not closed
						if sa.dumpingLoanID != nil && (sa.earliestDumpDate == nil || !currentDate.Before(*sa.earliestDumpDate)) {
							ls, ok := loanByID[*sa.dumpingLoanID]
							if ok && !ls.isClosed {
								loanPenalty := ls.loan.ActiveVersion.EarlyPayoffPenalty / 100.0
								if loanPenalty < 0 {
									loanPenalty = 0.01
								}

								cashNeeded := ls.currentBalance / (1.0 - loanPenalty)
								maxNetPossible := calculateMaxNetForSubAsset(as, sa)

								cashObtained := math.Min(maxNetPossible, cashNeeded)
								if cashObtained > 0 {
									grossSold, netFulfilled := withdrawFromSubAsset(as, sa.id, cashObtained)
									effectivePayoff := netFulfilled * (1.0 - loanPenalty)
									penaltyPaid := grossSold - netFulfilled

									log.Printf("[PROJECTION] SUB-ASSET LOAN DUMP AT PAYOUT: %s (%.2f) reduced by sub-asset %s of %s, Withdrawn Gross: %.2f, Net Obtained: %.2f, Effective: %.2f", ls.loan.Name, ls.currentBalance, sa.name, as.asset.Name, grossSold, netFulfilled, effectivePayoff)

									ls.currentBalance -= effectivePayoff
									month.Loans += netFulfilled
									month.Assets -= netFulfilled

									month.Breakdown.Loans = append(month.Breakdown.Loans, domain.EntryBreakdown{
										Name:       ls.loan.Name + " (Dump at Payout)",
										EntityName: ls.loan.Name,
										Amount:     netFulfilled,
										Balance:    ls.currentBalance,
										AccountIDs: ls.loan.AccountIDs,
										PoolID:     ls.loan.PoolID,
									})
									month.Breakdown.Assets = append(month.Breakdown.Assets, buildAssetBreakdownEntry(as, as.asset.Name+" ("+sa.name+" Dump at Payout)", -netFulfilled, 0, penaltyPaid))

									if ls.currentBalance <= 0.05 {
										log.Printf("[PROJECTION] LOAN CLOSED by DUMP at PAYOUT: %s", ls.loan.Name)
										ls.currentBalance = 0
										ls.isClosed = true
									} else {
										remainingMonths := ls.activeRuntime - ls.monthsElapsed
										if remainingMonths > 0 {
											ls.monthlyPayment = s.calculateLoanPayment(ls.currentBalance, ls.activeInterest, remainingMonths, ls.activeBalloon, ls.activeIsInterestOnly)
										}
									}
								}
							}
						}

						// Payout remainder of the sub-asset as income
						if sa.currentBalance > 0 {
							maxNetLeftover := calculateMaxNetForSubAsset(as, sa)
							grossPayout, netPayout := withdrawFromSubAsset(as, sa.id, maxNetLeftover)
							penaltyPaid := grossPayout - netPayout

							log.Printf("[PROJECTION] Sub-Asset Payout: %s (%s), Gross: %.2f, Net: %.2f", as.asset.Name, sa.name, grossPayout, netPayout)
							month.Income += netPayout
							availableFunds += netPayout
							month.Breakdown.Incomes = append(month.Breakdown.Incomes, domain.EntryBreakdown{
								Name:       as.asset.Name + " (" + sa.name + " Payout)",
								Amount:     netPayout,
								Penalty:    penaltyPaid,
								AccountIDs: as.asset.AccountIDs,
							})
						}
						sa.isClosed = true
					}
				}
			}

			if v.EndDate != nil {
				uEnd := v.EndDate.UTC()
				if currentDate.Year() == uEnd.Year() && currentDate.Month() == uEnd.Month() {
					if len(as.subAssets) > 0 {
						// First, check final dump for each sub-asset
						for _, sa := range as.subAssets {
							if sa.isClosed {
								continue
							}
							if sa.dumpingLoanID != nil && (sa.earliestDumpDate == nil || !currentDate.Before(*sa.earliestDumpDate)) {
								ls, ok := loanByID[*sa.dumpingLoanID]
								if ok && !ls.isClosed {
									loanPenalty := ls.loan.ActiveVersion.EarlyPayoffPenalty / 100.0
									if loanPenalty < 0 {
										loanPenalty = 0.01
									}

									cashNeeded := ls.currentBalance / (1.0 - loanPenalty)
									maxNetPossible := calculateMaxNetForSubAsset(as, sa)

									cashObtained := math.Min(maxNetPossible, cashNeeded)
									grossSold, netFulfilled := withdrawFromSubAsset(as, sa.id, cashObtained)
									effectivePayoff := netFulfilled * (1.0 - loanPenalty)
									penaltyPaid := grossSold - netFulfilled

									log.Printf("[PROJECTION] FINAL LOAN DUMP from SUB-ASSET: %s (%.2f) reduced by sub-asset %s of %s, Withdrawn Gross: %.2f, Net Obtained: %.2f, Effective: %.2f", ls.loan.Name, ls.currentBalance, sa.name, as.asset.Name, grossSold, netFulfilled, effectivePayoff)

									ls.currentBalance -= effectivePayoff
									month.Loans += netFulfilled
									month.Assets -= netFulfilled

									month.Breakdown.Loans = append(month.Breakdown.Loans, domain.EntryBreakdown{
										Name:       ls.loan.Name + " (Dump at Payout)",
										EntityName: ls.loan.Name,
										Amount:     netFulfilled,
										Balance:    ls.currentBalance,
										AccountIDs: ls.loan.AccountIDs,
										PoolID:     ls.loan.PoolID,
									})
									month.Breakdown.Assets = append(month.Breakdown.Assets, buildAssetBreakdownEntry(as, as.asset.Name+" ("+sa.name+" Dump at Payout)", -netFulfilled, 0, penaltyPaid))

									if ls.currentBalance <= 0.05 {
										log.Printf("[PROJECTION] LOAN CLOSED by FINAL DUMP: %s", ls.loan.Name)
										ls.currentBalance = 0
										ls.isClosed = true
									} else {
										remainingMonths := ls.activeRuntime - ls.monthsElapsed
										if remainingMonths > 0 {
											ls.monthlyPayment = s.calculateLoanPayment(ls.currentBalance, ls.activeInterest, remainingMonths, ls.activeBalloon, ls.activeIsInterestOnly)
										}
									}
								}
							}
						}

						// Payout remainder of all active sub-assets as income
						for _, sa := range as.subAssets {
							if sa.isClosed {
								continue
							}
							if sa.currentBalance > 0 {
								maxNetLeftover := calculateMaxNetForSubAsset(as, sa)
								grossPayout, netPayout := withdrawFromSubAsset(as, sa.id, maxNetLeftover)
								penaltyPaid := grossPayout - netPayout

								log.Printf("[PROJECTION] Sub-Asset Payout: %s (%s), Gross: %.2f, Net: %.2f", as.asset.Name, sa.name, grossPayout, netPayout)
								month.Income += netPayout
								availableFunds += netPayout
								month.Breakdown.Incomes = append(month.Breakdown.Incomes, domain.EntryBreakdown{
									Name:       as.asset.Name + " (" + sa.name + " Payout)",
									Amount:     netPayout,
									Penalty:    penaltyPaid,
									AccountIDs: as.asset.AccountIDs,
									PoolID:     as.asset.PoolID,
								})
							}
							sa.isClosed = true
						}
					} else {
						// Final check: Can we dump into a loan before paying out?
						var ls *loanState
						var ok bool
						if v.DumpingLoanID != nil {
							ls, ok = loanByID[*v.DumpingLoanID]
						}

						if ok && !ls.isClosed {
							loanPenalty := ls.loan.ActiveVersion.EarlyPayoffPenalty / 100.0
							if loanPenalty < 0 {
								loanPenalty = 0.01
							}
							cashNeeded := ls.currentBalance / (1.0 - loanPenalty)
							maxNetAsset := calculateMaxNet(as)
							requestedNet := math.Min(maxNetAsset, cashNeeded)

							grossSold, netFulfilled := withdrawAsset(as, requestedNet)
							effectivePayoff := netFulfilled * (1.0 - loanPenalty)

							log.Printf("[PROJECTION] FINAL LOAN DUMP: %s (%.2f) reduced by %s, Asset Withdrawn Gross: %.2f, Cash Obtained Net: %.2f, Effective: %.2f", ls.loan.Name, ls.currentBalance, as.asset.Name, grossSold, netFulfilled, effectivePayoff)

							ls.currentBalance -= effectivePayoff
							month.Loans += netFulfilled
							month.Assets -= netFulfilled // Offset the loan payoff with an asset withdrawal to keep budget sum consistent

							month.Breakdown.Loans = append(month.Breakdown.Loans, domain.EntryBreakdown{
								Name:       ls.loan.Name + " (Dump at Payout)",
								EntityName: ls.loan.Name,
								Amount:     netFulfilled,
								Balance:    ls.currentBalance,
								AccountIDs: ls.loan.AccountIDs,
								PoolID:     ls.loan.PoolID,
							})
							month.Breakdown.Assets = append(month.Breakdown.Assets, buildAssetBreakdownEntry(as, as.asset.Name+" (Dump at Payout)", -netFulfilled, 0, grossSold-netFulfilled))

							if ls.currentBalance <= 0.05 {
								log.Printf("[PROJECTION] LOAN CLOSED by FINAL DUMP: %s", ls.loan.Name)
								ls.currentBalance = 0
								ls.isClosed = true
							} else {
								// Recalculate monthly payment for the remaining balance and runtime
								remainingMonths := ls.activeRuntime - ls.monthsElapsed
								if remainingMonths > 0 {
									ls.monthlyPayment = s.calculateLoanPayment(ls.currentBalance, ls.activeInterest, remainingMonths, ls.activeBalloon, ls.activeIsInterestOnly)
									log.Printf("[PROJECTION] LOAN REDUCED by FINAL DUMP: %s. New Balance: %.2f, New Payment: %.2f", ls.loan.Name, ls.currentBalance, ls.monthlyPayment)
								}
							}
						}

						// Remaining balance after potential final dump becomes income
						if as.currentBalance > 0 {
							grossPayout, netPayout := withdrawAsset(as, calculateMaxNet(as))
							penaltyPaid := grossPayout - netPayout

							log.Printf("[PROJECTION] Asset Payout: %s, Gross: %.2f, Net: %.2f", as.asset.Name, grossPayout, netPayout)
							month.Income += netPayout
							availableFunds += netPayout
							month.Breakdown.Incomes = append(month.Breakdown.Incomes, domain.EntryBreakdown{
								Name:       as.asset.Name + " (Payout)",
								Amount:     netPayout,
								Penalty:    penaltyPaid,
								AccountIDs: as.asset.AccountIDs,
								PoolID:     as.asset.PoolID,
							})
						}
					}
					as.isClosed = true
				}
			}
		}

		// 6. PROCESS LOANS (Mandatory Payments)
		for _, ls := range loanStates {
			if ls.isClosed {
				continue
			}
			v := ls.loan.ActiveVersion

			// Apply modifications
			for _, m := range mods {
				if m.TargetID == ls.loan.ID && m.TargetType == "LOAN" {
					if s.isActiveAt(m.ActiveVersion.StartDate, m.ActiveVersion.EndDate, currentDate, m.ActiveVersion.IntervalMonths) {
						ls.currentBalance -= m.ActiveVersion.Amount
						triggeredMods[m.ID] = true
						log.Printf("[PROJECTION] Loan Mod: %s -> %s, Amount: %.2f, New Balance: %.2f", m.Description, ls.loan.Name, m.ActiveVersion.Amount, ls.currentBalance)

						// Recalculate if not closed
						if ls.currentBalance <= 0.05 {
							ls.currentBalance = 0
							ls.isClosed = true
						} else {
							remainingMonths := ls.activeRuntime - ls.monthsElapsed
							if remainingMonths > 0 {
								ls.monthlyPayment = s.calculateLoanPayment(ls.currentBalance, ls.activeInterest, remainingMonths, ls.activeBalloon, ls.activeIsInterestOnly)
								log.Printf("[PROJECTION] LOAN REDUCED by MOD: %s. New Payment: %.2f", ls.loan.Name, ls.monthlyPayment)
							}
						}

						month.Breakdown.Loans = append(month.Breakdown.Loans, domain.EntryBreakdown{
							Name:       m.Description + " (Mod)",
							EntityName: ls.loan.Name,
							Amount:     m.ActiveVersion.Amount,
							Balance:    ls.currentBalance,
							AccountIDs: ls.loan.AccountIDs,
							PoolID:     ls.loan.PoolID,
						})
					}
				}
			}

			if s.isActiveAt(v.StartDate, nil, currentDate, 1) {
				monthlyRate := (ls.activeInterest / 100.0) / 12.0
				interest := ls.currentBalance * monthlyRate

				payment := ls.monthlyPayment
				if ls.activeIsInterestOnly {
					payment = interest
				}

				if ls.currentBalance+interest < payment {
					payment = ls.currentBalance + interest
				}

				principal := payment - interest
				ls.currentBalance -= principal
				ls.monthsElapsed++

				month.Loans += payment
				availableFunds -= payment
				log.Printf("[PROJECTION] Loan Payment: %s, Amount: %.2f, Principal: %.2f, Interest: %.2f, Balance: %.2f", ls.loan.Name, payment, principal, interest, ls.currentBalance)
				month.Breakdown.Loans = append(month.Breakdown.Loans, domain.EntryBreakdown{
					Name:       ls.loan.Name,
					EntityName: ls.loan.Name,
					Amount:     payment,
					Interest:   interest,
					Balance:    ls.currentBalance,
					AccountIDs: ls.loan.AccountIDs,
					PoolID:     ls.loan.PoolID,
				})

				if ls.currentBalance <= 0.05 {
					log.Printf("[PROJECTION] LOAN CLOSED by FINAL PAYMENT: %s", ls.loan.Name)
					ls.currentBalance = 0
					ls.isClosed = true
					continue
				}

				if ls.monthsElapsed >= ls.activeRuntime {
					if ls.activeBalloon > 0 && ls.loan.ActiveVersion.NextLoanID == nil {
						month.Expenses += ls.activeBalloon
						availableFunds -= ls.activeBalloon
						ls.currentBalance = 0
						ls.isClosed = true
					} else if ls.currentBalance <= 0.05 {
						log.Printf("[PROJECTION] LOAN CLOSED by RUNTIME/PAYMENT: %s", ls.loan.Name)
						ls.currentBalance = 0
						ls.isClosed = true
					} else {
						// Check for 1:1 NextLoanID first
						if ls.loan.ActiveVersion.NextLoanID != nil {
							if nextLS, ok := loanByID[*ls.loan.ActiveVersion.NextLoanID]; ok {
								vNext := nextLS.loan.ActiveVersion

								// TRANSFER BALANCE TO TARGET STATE (OVERWRITE)
								nextLS.currentBalance = ls.currentBalance
								nextLS.isClosed = false
								nextLS.monthsElapsed = 0

								// Synchronize parameters from target loan config
								nextLS.activeInterest = vNext.InterestRate
								nextLS.activeRuntime = vNext.RuntimeMonths
								nextLS.activeBalloon = vNext.BalloonLeftover
								nextLS.activeIsInterestOnly = vNext.IsInterestOnly

								// Recalculate payment for target loan using its own config but the new balance
								nextLS.monthlyPayment = s.calculateLoanPayment(nextLS.currentBalance, nextLS.activeInterest, nextLS.activeRuntime, nextLS.activeBalloon, nextLS.activeIsInterestOnly)

								log.Printf("[PROJECTION] LOAN ROLLOVER (1:1): %s balance (%.2f) transferred to %s. New Payment: %.2f", ls.loan.Name, ls.currentBalance, nextLS.loan.Name, nextLS.monthlyPayment)

								ls.isClosed = true
								ls.currentBalance = 0
							} else {
								month.Expenses += ls.currentBalance
								availableFunds -= ls.currentBalance
								ls.currentBalance = 0
								ls.isClosed = true
							}
						} else {
							month.Expenses += ls.currentBalance
							availableFunds -= ls.currentBalance
							ls.currentBalance = 0
							ls.isClosed = true
						}
					}
				}
			}
		}

		// 6.5 CLEANUP ORPHANED DUMPING ASSETS (Loans closed by regular payments or rollover failures)
		for _, as := range assetStates {
			if as.isClosed {
				continue
			}
			v := as.asset.ActiveVersion

			if len(as.subAssets) > 0 {
				for _, sa := range as.subAssets {
					if sa.isClosed {
						continue
					}
					if sa.dumpingLoanID != nil {
						ls, ok := loanByID[*sa.dumpingLoanID]
						if ok && ls.isClosed {
							// Only orphan if it's not a rollover target waiting for its turn
							if ls.isRolloverTarget && ls.monthsElapsed == 0 {
								continue
							}

							if sa.currentBalance > 0 {
								maxNetSA := calculateMaxNetForSubAsset(as, sa)
								grossPayout, netPayout := withdrawFromSubAsset(as, sa.id, maxNetSA)
								penaltyPaid := grossPayout - netPayout

								log.Printf("[PROJECTION] Dumping Sub-Asset Orphaned (Loan %s closed): %s (%s), Gross: %.2f, Net: %.2f", ls.loan.Name, as.asset.Name, sa.name, grossPayout, netPayout)
								month.Income += netPayout
								availableFunds += netPayout
								month.Breakdown.Incomes = append(month.Breakdown.Incomes, domain.EntryBreakdown{
									Name:       as.asset.Name + " (" + sa.name + " Orphaned Dump Payout)",
									Amount:     netPayout,
									Penalty:    penaltyPaid,
									AccountIDs: as.asset.AccountIDs,
									PoolID:     as.asset.PoolID,
								})
							}
							sa.isClosed = true
						}
					}
				}
			} else if v.DumpingLoanID != nil {
				ls, ok := loanByID[*v.DumpingLoanID]
				if ok && ls.isClosed {
					// Only orphan if it's not a rollover target waiting for its turn
					if ls.isRolloverTarget && ls.monthsElapsed == 0 {
						continue
					}

					if as.currentBalance > 0 {
						grossPayout, netPayout := withdrawAsset(as, calculateMaxNet(as))
						penaltyPaid := grossPayout - netPayout

						log.Printf("[PROJECTION] Dumping Asset Orphaned (Loan %s closed): %s, Gross: %.2f, Net: %.2f", ls.loan.Name, as.asset.Name, grossPayout, netPayout)
						month.Income += netPayout
						availableFunds += netPayout
						month.Breakdown.Incomes = append(month.Breakdown.Incomes, domain.EntryBreakdown{
							Name:       as.asset.Name + " (Orphaned Dump Payout)",
							Amount:     netPayout,
							Penalty:    penaltyPaid,
							AccountIDs: as.asset.AccountIDs,
							PoolID:     as.asset.PoolID,
						})
					}
					as.isClosed = true
				}
			}
		}

		// 7. FIXED ASSET CONTRIBUTIONS
		for _, as := range assetStates {
			if as.isClosed || as.contributionsStopped {
				continue
			}
			v := as.asset.ActiveVersion

			// 7.1 Identify active sub-assets
			var activeSubAssets []*subAssetState
			totalSubAssetDemand := 0.0
			for _, sa := range as.subAssets {
				if !sa.isClosed && s.isActiveAt(sa.startDate, sa.endDate, currentDate, 1) {
					activeSubAssets = append(activeSubAssets, sa)
					totalSubAssetDemand += sa.amountPerMonth
				}
			}

			if len(activeSubAssets) > 0 {
				// We have sub-assets. Do NOT scale contributions based on availableFunds.
				scale := 1.0

				for _, sa := range activeSubAssets {
					if sa.amountPerMonth > 0 {
						target := getSubAssetTarget(sa, loanByID)
						contribution := sa.amountPerMonth * scale
						if target >= 0 {
							contribution = math.Min(contribution, math.Max(0, target-sa.currentBalance))
						}

						if contribution > 0 {
							depositToSubAsset(as, sa.id, contribution)
							month.Assets += contribution
							availableFunds -= contribution
							log.Printf("[PROJECTION] Sub-Asset Contribution: %s -> %s, Amount: %.2f, New Balance: %.2f (Total: %.2f)", as.asset.Name, sa.name, contribution, sa.currentBalance, as.currentBalance)
							month.Breakdown.Assets = append(month.Breakdown.Assets, buildAssetBreakdownEntry(as, as.asset.Name+" ("+sa.name+")", contribution, 0, 0))
						}
					}
				}
			} else if s.isActiveAt(v.StartDate, v.EndDate, currentDate, 1) && v.AmountPerMonth > 0 {
				// Fallback to parent rate if no sub-assets or all are closed
				target, err := strconv.ParseFloat(v.TargetValue, 64)
				if err != nil {
					var ls *loanState
					var ok bool
					if v.DumpingLoanID != nil {
						ls, ok = loanByID[*v.DumpingLoanID]
					}
					if ok {
						penalty := ls.loan.ActiveVersion.EarlyPayoffPenalty / 100.0
						if penalty < 0 {
							penalty = 0.01
						}
						target = ls.currentBalance / (1.0 - penalty)
						err = nil
					}
				}

				contribution := v.AmountPerMonth
				if err == nil && target > 0 {
					contribution = math.Min(contribution, math.Max(0, target-as.currentBalance))
				}

				if contribution > 0 {
					actualConsumed := depositAsset(as, contribution, loanByID)
					month.Assets += actualConsumed
					availableFunds -= actualConsumed
					log.Printf("[PROJECTION] Asset Contribution: %s, Amount: %.2f, New Balance: %.2f", as.asset.Name, actualConsumed, as.currentBalance)
					month.Breakdown.Assets = append(month.Breakdown.Assets, buildAssetBreakdownEntry(as, as.asset.Name, actualConsumed, 0, 0))
				}
			}
		}

		// 8. REMAINDER CONSUMPTION WATERFALL
		leftover := availableFunds
		if leftover > 0 {
			for _, entityID := range scenario.RemainderOrder {
				if leftover <= 0 {
					break
				}
				for _, as := range assetStates {
					if as.asset.ID == entityID && !as.isClosed && !as.contributionsStopped {
						v := as.asset.ActiveVersion
						if !s.isActiveAt(v.StartDate, v.EndDate, currentDate, 1) {
							continue
						}

						if v.RemainderStartDate != nil {
							if !s.isActiveAt(*v.RemainderStartDate, nil, currentDate, 1) {
								continue
							}
						}

						// Global Parent Target Check
						parentTarget, err := strconv.ParseFloat(v.TargetValue, 64)
						hasParentTarget := err == nil && parentTarget > 0
						if hasParentTarget && as.currentBalance >= (parentTarget-0.01) {
							continue
						}

						if len(as.subAssets) > 0 {
							// Granular Sub-Asset Waterfall (Even Split + Rebalancing)
							var remainderConsumers []*subAssetState
							for _, sa := range as.subAssets {
								if !sa.isClosed && sa.isRemainderConsumer && sa.amountPerMonth == 0 && s.isActiveAt(sa.startDate, sa.endDate, currentDate, 1) {
									if sa.remainderStartDate != nil {
										if !s.isActiveAt(*sa.remainderStartDate, nil, currentDate, 1) {
											continue
										}
									}

									target := getSubAssetTarget(sa, loanByID)
									if target < 0 || sa.currentBalance < (target-0.01) {
										remainderConsumers = append(remainderConsumers, sa)
									}
								}
							}

							if len(remainderConsumers) > 0 {
								remainingInAssetLoop := leftover
								if hasParentTarget {
									remainingInAssetLoop = math.Min(leftover, parentTarget-as.currentBalance)
								}

								consumedByAssetTotal := 0.0

								// Iterative even-split distribution
								for remainingInAssetLoop > 0.01 && len(remainderConsumers) > 0 {
									evenShare := remainingInAssetLoop / float64(len(remainderConsumers))
									newRemainderConsumers := []*subAssetState{}
									thisRoundConsumed := 0.0

									for _, sa := range remainderConsumers {
										target := getSubAssetTarget(sa, loanByID)
										toDep := evenShare
										if target >= 0 {
											room := math.Max(0, target-sa.currentBalance)
											toDep = math.Min(evenShare, room)
										}

										if toDep > 0 {
											depositToSubAsset(as, sa.id, toDep)
											thisRoundConsumed += toDep
											remainingInAssetLoop -= toDep
											consumedByAssetTotal += toDep
											log.Printf("[PROJECTION] Remainder -> Sub-Asset: %s -> %s, Amount: %.2f, New Balance: %.2f (Total: %.2f)", as.asset.Name, sa.name, toDep, sa.currentBalance, as.currentBalance)
											month.Breakdown.Assets = append(month.Breakdown.Assets, buildAssetBreakdownEntry(as, as.asset.Name+" ("+sa.name+") (Remainder)", toDep, 0, 0))

											// If we still have room (or infinite), keep for next round if needed
											if target < 0 || sa.currentBalance < (target-0.01) {
												newRemainderConsumers = append(newRemainderConsumers, sa)
											}
										}
									}

									remainderConsumers = newRemainderConsumers
									if thisRoundConsumed <= 0.0001 {
										break
									}
								}

								month.Assets += consumedByAssetTotal
								leftover -= consumedByAssetTotal
							}
						} else {
							// Fallback to parent balance ONLY if asset has NO sub-assets
							target, err := strconv.ParseFloat(v.TargetValue, 64)
							hasTarget := err == nil && target > 0
							if !hasTarget && v.DumpingLoanID != nil {
								if ls, ok := loanByID[*v.DumpingLoanID]; ok {
									penalty := ls.loan.ActiveVersion.EarlyPayoffPenalty / 100.0
									if penalty < 0 {
										penalty = 0.01
									}
									target = ls.currentBalance / (1.0 - penalty)
									hasTarget = true
								}
							}

							if !hasTarget || as.currentBalance < (target-0.01) {
								consumed := leftover
								if hasTarget {
									consumed = math.Min(leftover, target-as.currentBalance)
								}
								if consumed > 0 {
									actualConsumed := depositAsset(as, consumed, loanByID)
									leftover -= actualConsumed
									month.Assets += actualConsumed
									log.Printf("[PROJECTION] Remainder -> Asset: %s, Amount: %.2f, New Balance: %.2f", as.asset.Name, actualConsumed, as.currentBalance)
									month.Breakdown.Assets = append(month.Breakdown.Assets, buildAssetBreakdownEntry(as, as.asset.Name+" (Remainder)", actualConsumed, 0, 0))
								}
							}
						}
					}
				}
				for _, ls := range loanStates {
					if ls.loan.ID == entityID && !ls.isClosed {
						v := ls.loan.ActiveVersion
						if !s.isActiveAt(v.StartDate, nil, currentDate, 1) {
							continue
						}

						if v.RemainderStartDate != nil {
							if !s.isActiveAt(*v.RemainderStartDate, nil, currentDate, 1) {
								continue
							}
						}

						penalty := ls.loan.ActiveVersion.EarlyPayoffPenalty / 100.0
						if penalty < 0 {
							penalty = 0.01
						}

						needed := ls.currentBalance / (1.0 - penalty)
						consumed := math.Min(leftover, needed)
						payoff := consumed * (1.0 - penalty)

						ls.currentBalance -= payoff
						leftover -= consumed
						month.Loans += consumed
						month.Breakdown.Loans = append(month.Breakdown.Loans, domain.EntryBreakdown{
							Name:       ls.loan.Name + " (Remainder)",
							EntityName: ls.loan.Name,
							Amount:     consumed,
							Balance:    ls.currentBalance,
							AccountIDs: ls.loan.AccountIDs,
							PoolID:     ls.loan.PoolID,
						})

						if ls.currentBalance <= 0.05 {
							ls.currentBalance = 0
							ls.isClosed = true
						}
					}
				}
			}
		}

		// 8.5 APPLY ETF INTEREST/RETURNS (At the end of the month after all modifications, contributions, and waterfalls)
		for _, as := range assetStates {
			if as.isClosed {
				continue
			}

			v := as.asset.ActiveVersion
			if v.Type != domain.AssetTypeETF {
				continue
			}

			// Interest/Returns
			interestEarned := 0.0
			interestPenaltyPaid := 0.0
			interestPenaltyRate := getInterestPenaltyRate(v)

			monthlyRate := math.Pow(1.0+as.simulatedYield, 1.0/12.0) - 1.0
			var newBal float64
			var totalGrossGrowth float64

			isDistributing := v.InterestInterval == "Monthly"

			if monthlyRate > 0 {
				for i := range as.lots {
					grossGrowth := as.lots[i].currentValue * monthlyRate
					totalGrossGrowth += grossGrowth

					if !isDistributing {
						netGrowth := grossGrowth * (1.0 - interestPenaltyRate)
						as.lots[i].currentValue += netGrowth
					}
					newBal += as.lots[i].currentValue
				}
				interestEarned = totalGrossGrowth
				interestPenaltyPaid = totalGrossGrowth * interestPenaltyRate
			} else {
				for i := range as.lots {
					as.lots[i].currentValue *= (1.0 + monthlyRate)
					newBal += as.lots[i].currentValue
				}
				interestEarned = newBal - as.currentBalance
				interestPenaltyPaid = 0.0
			}

			oldBal := as.currentBalance
			as.currentBalance = newBal
			netGrowth := newBal - oldBal

			if isDistributing && interestEarned > 0 {
				payout := interestEarned - interestPenaltyPaid
				month.Income += payout
				availableFunds += payout
				month.Breakdown.Incomes = append(month.Breakdown.Incomes, domain.EntryBreakdown{
					Name:       as.asset.Name + " (Dividend)",
					Amount:     payout,
					AccountIDs: as.asset.AccountIDs,
					PoolID:     as.asset.PoolID,
				})

				if as.penaltyAnalysis != nil {
					*as.penaltyAnalysis = append(*as.penaltyAnalysis, domain.PenaltyEvent{
						Type:          "SELL",
						Date:          as.currentMonth,
						AssetName:     as.asset.Name,
						LotID:             "DIVIDEND",
						LotCreatedAt:      as.currentMonth,
						Amount:            totalGrossGrowth,
						PrincipalSold:     0, // It's pure profit/growth
						PenaltyPaid:       interestPenaltyPaid,
						MonthsHeld:        0,
						InterestGenerated: totalGrossGrowth,
						})

				}

				// Reset netGrowth to 0 so it doesn't propagate to sub-assets
				netGrowth = 0

				// Ensure interestEarned/PenaltyPaid are NOT processed again at the end of loop
				interestEarned = 0
				interestPenaltyPaid = 0
			}

			totalTrackersNew := 0.0
			for tracker, yield := range as.trackerYields {
				mRate := math.Pow(1.0+yield, 1.0/12.0) - 1.0
				bal := as.trackerBalances[tracker]
				if mRate > 0 {
					if !isDistributing {
						grossGrowth := bal * mRate
						netGrowthVal := grossGrowth * (1.0 - interestPenaltyRate)
						as.trackerBalances[tracker] += netGrowthVal
					}
				} else {
					as.trackerBalances[tracker] *= (1.0 + mRate)
				}
				totalTrackersNew += as.trackerBalances[tracker]
			}
			if totalTrackersNew > 0 && as.currentBalance > 0 {
				scale := as.currentBalance / totalTrackersNew
				for tracker := range as.trackerBalances {
					as.trackerBalances[tracker] *= scale
				}
			} else if as.currentBalance <= 0 {
				for tracker := range as.trackerBalances {
					as.trackerBalances[tracker] = 0
				}
			}

			if !isDistributing && len(as.subAssets) > 0 && netGrowth != 0 {
				totalBal := 0.0
				for _, sa := range as.subAssets {
					if !sa.isClosed {
						totalBal += sa.currentBalance
					}
				}
				if totalBal > 0 {
					remaining := netGrowth
					for _, sa := range as.subAssets {
						if sa.isClosed {
							continue
						}
						share := sa.currentBalance / totalBal
						toDep := netGrowth * share
						sa.currentBalance += toDep
						remaining -= toDep
					}
					if math.Abs(remaining) > 0.00001 && len(as.subAssets) > 0 {
						as.subAssets[0].currentBalance += remaining
					}
				} else {
					as.subAssets[0].currentBalance += netGrowth
				}
			}

			if interestEarned != 0 || interestPenaltyPaid != 0 {
				month.Breakdown.Assets = append(month.Breakdown.Assets, buildAssetBreakdownEntry(as, as.asset.Name, 0, interestEarned, interestPenaltyPaid))
			}
		}

		// Finalize
		month.Remainder = leftover
		currentBalance += month.Remainder
		month.Balance = currentBalance

		etfWorth := 0.0
		for _, as := range assetStates {
			if !as.isClosed {
				month.AssetWorth += as.currentBalance
				if as.asset.ActiveVersion.Type == domain.AssetTypeETF {
					etfWorth += as.currentBalance
				}
			}
		}

		// Calculate Passive Income milestone value (monthly fraction of configured annual withdrawal)
		month.PassiveIncome = etfWorth * (scenario.PassiveIncomePercentage / 100.0 / 12.0)

		for _, ls := range loanStates {
			if !ls.isClosed {
				month.LoanDebt += ls.currentBalance
			}
		}

		// Post-process to link realtime balances by pool using prioritized consumption logic
		monthKey := currentDate.Format("2006-01")
		now := time.Now().UTC()
		currentYearMonth := projectionMonthForDate(now, monthStartDay, loc).Format("2006-01")

		if monthKey <= currentYearMonth {
			type poolGroup struct {
				fixedEntries         []*domain.EntryBreakdown
				variableEntries      []*domain.EntryBreakdown
				totalPlannedVariable float64
			}
			poolGroups := make(map[string]*poolGroup)

			getGroup := func(pid string) *poolGroup {
				if _, ok := poolGroups[pid]; !ok {
					poolGroups[pid] = &poolGroup{}
				}
				return poolGroups[pid]
			}

			// 1. Collect Incomes (Fixed)
			for i := range month.Breakdown.Incomes {
				if pid := month.Breakdown.Incomes[i].PoolID; pid != nil {
					g := getGroup(*pid)
					g.fixedEntries = append(g.fixedEntries, &month.Breakdown.Incomes[i])
				}
			}

			// 2. Collect Bills (Fixed)
			for i := range month.Breakdown.Bills {
				if pid := month.Breakdown.Bills[i].PoolID; pid != nil {
					g := getGroup(*pid)
					g.fixedEntries = append(g.fixedEntries, &month.Breakdown.Bills[i])
				}
			}

			// 3. Collect Expenses (Variable)
			for i := range month.Breakdown.Expenses {
				if pid := month.Breakdown.Expenses[i].PoolID; pid != nil {
					g := getGroup(*pid)
					g.variableEntries = append(g.variableEntries, &month.Breakdown.Expenses[i])
					g.totalPlannedVariable += math.Abs(month.Breakdown.Expenses[i].Amount)
				}
			}

			// 4. Collect Assets (Fixed if monthly rate > 0, else Variable)
			for i := range month.Breakdown.Assets {
				assetName := month.Breakdown.Assets[i].EntityName
				if assetName == "" {
					assetName = month.Breakdown.Assets[i].Name
				}

				if pid := month.Breakdown.Assets[i].PoolID; pid != nil {
					g := getGroup(*pid)
					// Check if this asset has a planned monthly rate (contribution)
					isFixed := false
					for _, as := range assets {
						if as.Name == assetName && as.ActiveVersion != nil {
							if as.ActiveVersion.AmountPerMonth > 0 {
								isFixed = true
								break
							}
							for _, sa := range as.ActiveVersion.SubAssets {
								if sa.AmountPerMonth > 0 {
									isFixed = true
									break
								}
							}
						}
					}

					if isFixed {
						g.fixedEntries = append(g.fixedEntries, &month.Breakdown.Assets[i])
					} else {
						g.variableEntries = append(g.variableEntries, &month.Breakdown.Assets[i])
						g.totalPlannedVariable += math.Abs(month.Breakdown.Assets[i].Amount)
					}
				}
			}

			// 5. Collect Loans (Variable)
			for i := range month.Breakdown.Loans {
				if pid := month.Breakdown.Loans[i].PoolID; pid != nil {
					g := getGroup(*pid)
					g.variableEntries = append(g.variableEntries, &month.Breakdown.Loans[i])
					g.totalPlannedVariable += math.Abs(month.Breakdown.Loans[i].Amount)
				}
			}

			// 6. Distribute Realtime Balances
			for pid, g := range poolGroups {
				actualVal, exists := realtimeBalances[pid][monthKey]
				if !exists {
					continue
				}

				remainingAbs := math.Abs(actualVal)

				// 1. Initial pass for Fixed entries capped at their planned amount
				fixedGiven := make([]float64, len(g.fixedEntries))
				totalPlannedFixed := 0.0
				for i, entry := range g.fixedEntries {
					plannedAbs := math.Abs(entry.Amount)
					totalPlannedFixed += plannedAbs
					take := math.Min(remainingAbs, plannedAbs)
					fixedGiven[i] = take
					remainingAbs -= take
				}

				// 2. Variable share EVERYTHING else
				if remainingAbs > 0 && len(g.variableEntries) > 0 {
					if g.totalPlannedVariable > 0 {
						for _, entry := range g.variableEntries {
							plannedAbs := math.Abs(entry.Amount)
							share := plannedAbs / g.totalPlannedVariable
							take := share * remainingAbs
							val := take * math.Copysign(1, entry.Amount)
							entry.RealtimeBalance = &val
						}
					} else {
						take := remainingAbs / float64(len(g.variableEntries))
						for _, entry := range g.variableEntries {
							val := take * math.Copysign(1, entry.Amount)
							entry.RealtimeBalance = &val
						}
					}
					remainingAbs = 0
				}

				// 3. Residual Overflow back to fixed if no variable entries exist to soak it
				if remainingAbs > 0 && len(g.fixedEntries) > 0 {
					if totalPlannedFixed > 0 {
						for i, entry := range g.fixedEntries {
							plannedAbs := math.Abs(entry.Amount)
							share := plannedAbs / totalPlannedFixed
							fixedGiven[i] += share * remainingAbs
						}
					} else {
						take := remainingAbs / float64(len(g.fixedEntries))
						for i := range g.fixedEntries {
							fixedGiven[i] += take
						}
					}
					remainingAbs = 0
				}

				// Final assignment for Fixed entries with correct sign
				for i, entry := range g.fixedEntries {
					val := fixedGiven[i] * math.Copysign(1, entry.Amount)
					entry.RealtimeBalance = &val
				}
			}
		}

		// Calculate Virtual Account planned monthly changes and ending balances
		month.VirtualAccounts = make([]domain.VirtualAccountMonthBalance, 0)

		// Reset running balances of virtual accounts to 0 at the start of each month
		for _, va := range virtualAccounts {
			vaRunningBalances[va.ID] = 0.0
		}
		unassignedRunningBalance = 0.0

		// Map of active virtual accounts to easily check assignments
		activeVAMap := make(map[string]bool)
		for _, va := range virtualAccounts {
			if va.ActiveVersion != nil {
				activeVAMap[va.ID] = true
			}
		}

		// Helper to check if a virtual account ID is assigned, and return the split portion factor.
		// Returns 0.0 if not assigned to this va.ID.
		getAssignmentFactor := func(accountIDs []string, vaID string) float64 {
			if len(accountIDs) == 0 {
				return 0.0
			}
			hasVA := false
			validCount := 0
			for _, id := range accountIDs {
				if activeVAMap[id] {
					validCount++
					if id == vaID {
						hasVA = true
					}
				}
			}
			if hasVA && validCount > 0 {
				return 1.0 / float64(validCount)
			}
			return 0.0
		}

		// Helper to check if an entry is unassigned (i.e. has no active virtual accounts assigned to it)
		isUnassigned := func(accountIDs []string) bool {
			if len(accountIDs) == 0 {
				return true
			}
			for _, id := range accountIDs {
				if activeVAMap[id] {
					return false
				}
			}
			return true
		}

		for _, va := range virtualAccounts {
			if va.ActiveVersion == nil {
				continue
			}

			vBal := domain.VirtualAccountMonthBalance{
				AccountID:       va.ID,
				Name:            va.Name,
				Color:           va.ActiveVersion.Color,
				StartingBalance: vaRunningBalances[va.ID],
			}

			// Add incomes
			for _, entry := range month.Breakdown.Incomes {
				if factor := getAssignmentFactor(entry.AccountIDs, va.ID); factor > 0 {
					vBal.Inflow += entry.Amount * factor
				}
			}

			// Add bills
			for _, entry := range month.Breakdown.Bills {
				if factor := getAssignmentFactor(entry.AccountIDs, va.ID); factor > 0 {
					vBal.Outflow += entry.Amount * factor
				}
			}

			// Add expenses
			for _, entry := range month.Breakdown.Expenses {
				if factor := getAssignmentFactor(entry.AccountIDs, va.ID); factor > 0 {
					vBal.Outflow += entry.Amount * factor
				}
			}

			// Add assets contributions and payouts
			for _, entry := range month.Breakdown.Assets {
				if factor := getAssignmentFactor(entry.AccountIDs, va.ID); factor > 0 {
					if entry.Amount >= 0 {
						vBal.Outflow += entry.Amount * factor
					} else {
						vBal.Inflow += math.Abs(entry.Amount) * factor
					}
				}
			}

			// Add loans payments
			for _, entry := range month.Breakdown.Loans {
				if factor := getAssignmentFactor(entry.AccountIDs, va.ID); factor > 0 {
					vBal.Outflow += entry.Amount * factor
				}
			}

			// Calculate ending balance
			vBal.Balance = vBal.StartingBalance + vBal.Inflow - vBal.Outflow
			vaRunningBalances[va.ID] = vBal.Balance

			// Compute asset worth for this account (split across assigned accounts)
			for _, as := range assetStates {
				if factor := getAssignmentFactor(as.asset.AccountIDs, va.ID); factor > 0 {
					vBal.AssetWorth += as.currentBalance * factor
				}
			}

			// Compute loan debt for this account (split across assigned accounts)
			for _, ls := range loanStates {
				if factor := getAssignmentFactor(ls.loan.AccountIDs, va.ID); factor > 0 {
					vBal.LoanDebt += ls.currentBalance * factor
				}
			}

			month.VirtualAccounts = append(month.VirtualAccounts, vBal)
		}

		// Add default unassigned catch-all virtual account if virtual accounts exist
		if len(virtualAccounts) > 0 {
			unassignedBal := domain.VirtualAccountMonthBalance{
				AccountID:       "unassigned",
				Name:            "unassigned",
				Color:           "#64748b", // elegant slate gray
				StartingBalance: unassignedRunningBalance,
			}

			// Add unassigned incomes
			for _, entry := range month.Breakdown.Incomes {
				if isUnassigned(entry.AccountIDs) {
					unassignedBal.Inflow += entry.Amount
				}
			}

			// Add unassigned bills
			for _, entry := range month.Breakdown.Bills {
				if isUnassigned(entry.AccountIDs) {
					unassignedBal.Outflow += entry.Amount
				}
			}

			// Add unassigned expenses
			for _, entry := range month.Breakdown.Expenses {
				if isUnassigned(entry.AccountIDs) {
					unassignedBal.Outflow += entry.Amount
				}
			}

			// Add unassigned assets contributions and payouts
			for _, entry := range month.Breakdown.Assets {
				if isUnassigned(entry.AccountIDs) {
					if entry.Amount >= 0 {
						unassignedBal.Outflow += entry.Amount
					} else {
						unassignedBal.Inflow += math.Abs(entry.Amount)
					}
				}
			}

			// Add unassigned loans payments
			for _, entry := range month.Breakdown.Loans {
				if isUnassigned(entry.AccountIDs) {
					unassignedBal.Outflow += entry.Amount
				}
			}

			// Calculate ending balance for unassigned
			unassignedBal.Balance = unassignedBal.StartingBalance + unassignedBal.Inflow - unassignedBal.Outflow
			unassignedRunningBalance = unassignedBal.Balance

			// Compute unassigned asset worth
			for _, as := range assetStates {
				if isUnassigned(as.asset.AccountIDs) {
					unassignedBal.AssetWorth += as.currentBalance
				}
			}

			// Compute unassigned loan debt
			for _, ls := range loanStates {
				if isUnassigned(ls.loan.AccountIDs) {
					unassignedBal.LoanDebt += ls.currentBalance
				}
			}

			month.VirtualAccounts = append(month.VirtualAccounts, unassignedBal)
		}

		result.Months[i] = month
		if onMonth != nil {
			onMonth(month)
		}
	}

	metrics.ProjectionDurationMS = time.Since(projectionStartTime).Milliseconds()
	metrics.TotalDurationMS = time.Since(totalStartTime).Milliseconds()
	result.Metrics = metrics
	result.SimulatedYields = simulatedYields

	result.TotalRemainder = currentBalance
	result.PenaltyAnalysis = penaltyAnalysis
	return result, nil
}

func (s *ProjectionService) calculateLoanPayment(principal float64, rate float64, months int, balloon float64, interestOnly bool) float64 {
	if principal <= 0 || months <= 0 {
		return 0
	}
	r := (rate / 100.0) / 12.0
	if interestOnly {
		return principal * r
	}
	if r == 0 {
		return (principal - balloon) / float64(months)
	}
	pow := math.Pow(1+r, float64(months))
	return (principal*pow - balloon) * r / (pow - 1)
}

func (s *ProjectionService) calculateRequiredRate(v *domain.AssetVersion, interestRate float64, loans []domain.Loan) float64 {
	target, err := strconv.ParseFloat(v.TargetValue, 64)
	if err != nil {
		if v.DumpingLoanID != nil {
			for _, l := range loans {
				if l.ID == *v.DumpingLoanID {
					penalty := l.ActiveVersion.EarlyPayoffPenalty / 100.0
					if penalty < 0 {
						penalty = 0.01
					}
					target = l.ActiveVersion.AmountLent / (1.0 - penalty)
					err = nil
					break
				}
			}
		}
	}

	if err != nil || target <= 0 || v.EndDate == nil {
		return 0
	}
	runtime := float64((v.EndDate.Year()-v.StartDate.Year())*12 + int(v.EndDate.Month()-v.StartDate.Month()))
	if runtime <= 0 {
		return target
	}
	r := (interestRate / 100.0) / 12.0
	if r > 0 {
		return (target * r) / (math.Pow(1+r, runtime) - 1)
	}
	return target / runtime
}

func (s *ProjectionService) calculateSubAssetRequiredRate(sa domain.SubAsset, interestRate float64, loans []domain.Loan) float64 {
	target, err := strconv.ParseFloat(sa.TargetValue, 64)
	if err != nil {
		if sa.DumpingLoanID != nil {
			for _, l := range loans {
				if l.ID == *sa.DumpingLoanID {
					penalty := l.ActiveVersion.EarlyPayoffPenalty / 100.0
					if penalty < 0 {
						penalty = 0.01
					}
					target = l.ActiveVersion.AmountLent / (1.0 - penalty)
					err = nil
					break
				}
			}
		}
	}

	if err != nil || target <= 0 || sa.EndDate == nil {
		return 0
	}
	runtime := float64((sa.EndDate.Year()-sa.StartDate.Year())*12 + int(sa.EndDate.Month()-sa.StartDate.Month()))
	if runtime <= 0 {
		return target
	}
	r := (interestRate / 100.0) / 12.0
	if r > 0 {
		return (target * r) / (math.Pow(1+r, runtime) - 1)
	}
	return target / runtime
}

func (s *ProjectionService) isActiveAt(start time.Time, end *time.Time, current time.Time, interval int) bool {
	start = start.UTC()
	current = current.UTC()

	if current.Before(time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)) {
		return false
	}
	if end != nil {
		uEnd := end.UTC()
		currentTotal := current.Year()*12 + int(current.Month())
		endTotal := uEnd.Year()*12 + int(uEnd.Month())
		if currentTotal > endTotal {
			return false
		}
	}
	if interval == 0 {
		return current.Year() == start.Year() && current.Month() == start.Month()
	}
	monthsDiff := (current.Year()-start.Year())*12 + int(current.Month()-start.Month())
	return monthsDiff%interval == 0
}

func (s *ProjectionService) GetETFHistory(userID string, scenarioID string) (map[string][]float64, error) {

	scenario, err := s.scenarioRepo.GetFull(userID, scenarioID)
	if err != nil {
		return nil, err
	}

	assets, err := s.resolveAssets(userID, scenario)
	if err != nil {
		return nil, err
	}

	history := make(map[string][]float64)
	for _, a := range assets {
		if a.ActiveVersion.Type == domain.AssetTypeETF {
			for _, t := range a.ActiveVersion.ETFConfig {
				if returns, ok := s.marketData.GetCachedReturns(t.Tracker); ok {
					history[t.Tracker] = returns
				}
			}
		}
	}

	return history, nil
}

func (s *ProjectionService) resolveIncomes(userID string, scenario *domain.Scenario) ([]domain.Income, error) {
	all, err := s.incomeRepo.List(userID)
	if err != nil {
		return nil, err
	}
	if len(scenario.Entities) == 0 {
		return all, nil
	}
	var filtered []domain.Income
	for _, item := range all {
		for _, e := range scenario.Entities {
			if e.EntityType == "INCOME" && e.EntityID == item.ID {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered, nil
}

func (s *ProjectionService) resolveBills(userID string, scenario *domain.Scenario) ([]domain.Bill, error) {
	all, err := s.billRepo.List(userID)
	if err != nil {
		return nil, err
	}
	if len(scenario.Entities) == 0 {
		return all, nil
	}
	var filtered []domain.Bill
	for _, item := range all {
		for _, e := range scenario.Entities {
			if e.EntityType == "BILL" && e.EntityID == item.ID {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered, nil
}

func (s *ProjectionService) resolveExpenses(userID string, scenario *domain.Scenario) ([]domain.Expense, error) {
	all, err := s.expenseRepo.List(userID)
	if err != nil {
		return nil, err
	}
	if len(scenario.Entities) == 0 {
		return all, nil
	}
	var filtered []domain.Expense
	for _, item := range all {
		for _, e := range scenario.Entities {
			if e.EntityType == "EXPENSE" && e.EntityID == item.ID {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered, nil
}

func (s *ProjectionService) resolveAssets(userID string, scenario *domain.Scenario) ([]domain.Asset, error) {
	all, err := s.assetRepo.List(userID)
	if err != nil {
		return nil, err
	}
	if len(scenario.Entities) == 0 {
		return all, nil
	}
	var filtered []domain.Asset
	for _, item := range all {
		for _, e := range scenario.Entities {
			if e.EntityType == "ASSET" && e.EntityID == item.ID {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered, nil
}

func (s *ProjectionService) resolveLoans(userID string, scenario *domain.Scenario) ([]domain.Loan, error) {
	all, err := s.loanRepo.List(userID)
	if err != nil {
		return nil, err
	}
	if len(scenario.Entities) == 0 {
		return all, nil
	}
	var filtered []domain.Loan
	for _, item := range all {
		for _, e := range scenario.Entities {
			if e.EntityType == "LOAN" && e.EntityID == item.ID {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered, nil
}

func (s *ProjectionService) resolveModifications(userID string, scenario *domain.Scenario) ([]domain.Modification, error) {
	all, err := s.modRepo.List(userID)
	if err != nil {
		return nil, err
	}

	if len(scenario.Entities) == 0 {
		return all, nil
	}
	var filtered []domain.Modification
	for _, item := range all {
		for _, e := range scenario.Entities {
			if e.EntityType == "MODIFICATION" && e.EntityID == item.ID {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered, nil
}

func (s *ProjectionService) findEarliestStart(assets []domain.Asset, loans []domain.Loan, monthStartDay int, loc *time.Location) time.Time {
	earliest := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	found := false
	for _, a := range assets {
		if a.ActiveVersion.StartDate.Before(earliest) {
			earliest = a.ActiveVersion.StartDate
			found = true
		}
	}
	for _, l := range loans {
		if l.ActiveVersion.StartDate.Before(earliest) {
			earliest = l.ActiveVersion.StartDate
			found = true
		}
	}
	if !found {
		now := time.Now().UTC()
		return projectionMonthForDate(now, monthStartDay, loc)
	}
	return projectionMonthForDate(earliest, monthStartDay, loc)
}

func projectionMonthForDate(date time.Time, monthStartDay int, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	localDate := date.In(loc)

	if monthStartDay <= 1 {
		return time.Date(localDate.Year(), localDate.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	if monthStartDay > 28 {
		monthStartDay = 28
	}

	label := time.Date(localDate.Year(), localDate.Month(), monthStartDay, 0, 0, 0, 0, time.UTC)
	if localDate.Day() >= monthStartDay {
		label = label.AddDate(0, 1, 0)
	}
	return label
}

func projectionPeriodBounds(labelDate time.Time, monthStartDay int, loc *time.Location) (time.Time, time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	localDate := labelDate.In(loc)

	if monthStartDay <= 1 {
		start := time.Date(localDate.Year(), localDate.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0)
	}
	if monthStartDay > 28 {
		monthStartDay = 28
	}

	end := time.Date(localDate.Year(), localDate.Month(), monthStartDay, 0, 0, 0, 0, time.UTC)
	return end.AddDate(0, -1, 0), end
}

func (s *ProjectionService) isDateActive(start time.Time, end *time.Time, current time.Time) bool {
	start = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	current = time.Date(current.Year(), current.Month(), 1, 0, 0, 0, 0, time.UTC)
	if current.Before(start) {
		return false
	}
	if end != nil {
		uEnd := time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, time.UTC)
		if current.After(uEnd) {
			return false
		}
	}
	return true
}

func diffMonths(start, end time.Time) int {
	y1, m1, _ := start.Date()
	y2, m2, _ := end.Date()
	return int(y2-y1)*12 + int(m2-m1)
}
