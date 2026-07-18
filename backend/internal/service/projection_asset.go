package service

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/genazt/my-budget-script/backend/internal/domain"
)

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
	expenseID           *string
	remainderPriority   int32
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
				Type:              "SELL",
				Date:              as.currentMonth,
				AssetName:         as.asset.Name,
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
				Type:              "SELL",
				Date:              as.currentMonth,
				AssetName:         as.asset.Name,
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
