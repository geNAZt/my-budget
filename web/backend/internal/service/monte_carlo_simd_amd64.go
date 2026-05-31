//go:build amd64 && experiment_simd

package service

import (
	"math"
	"math/rand/v2"
	"runtime"
	"simd/archsimd"
	"sort"
	"sync"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
)

func (s *ProjectionService) runMonteCarloSIMD(v *domain.AssetVersion, history [][]float64, simulations, years int, percent float64) float64 {
	if !archsimd.X86.AVX2() {
		return s.runMonteCarloParallel(v, history, simulations, years, percent)
	}

	const stepsPerYear = 52
	totalSteps := years * stepsPerYear
	numTrackers := len(history)
	if numTrackers == 0 {
		return 0.05
	}

	// Use shortest history for date-aligned correlated sampling
	poolSize := len(history[0])
	for i := 1; i < numTrackers; i++ {
		if len(history[i]) < poolSize {
			poolSize = len(history[i])
		}
	}

	if poolSize == 0 {
		return 0.05
	}

	results := make([]float64, simulations)
	numCPUs := runtime.NumCPU()
	var wg sync.WaitGroup
	wg.Add(numCPUs)

	simsPerWorker := simulations / numCPUs
	for w := 0; w < numCPUs; w++ {
		start := w * simsPerWorker
		end := start + simsPerWorker
		if w == numCPUs-1 {
			end = simulations
		}

		go func(startIndex, endIndex int) {
			defer wg.Done()
			r := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(startIndex)))

			// Pre-calculate weights and TERs
			weights := make([]float64, numTrackers)
			terPerStep := make([]float64, numTrackers)
			for i := 0; i < numTrackers; i++ {
				weights[i] = v.ETFConfig[i].Percentage
				terPerStep[i] = (v.ETFConfig[i].TER / 100.0) / float64(stepsPerYear)
			}

			// AVX2 uses 256-bit registers (4 float64)
			const batchSize = 4
			batchEnd := startIndex + ((endIndex-startIndex)/batchSize)*batchSize

			for sim := startIndex; sim < batchEnd; sim += batchSize {
				accumulatedLogReturns := [4]float64{}

				for step := 0; step < totalSteps; step++ {
					// Correlated sampling: same weekIndex for all trackers in one simulation
					indices := [4]int{
						r.IntN(poolSize),
						r.IntN(poolSize),
						r.IntN(poolSize),
						r.IntN(poolSize),
					}

					stepRet := archsimd.Float64x4{}
					for t := 0; t < numTrackers; t++ {
						hist := history[t]
						// Load 4 values from history (Dependent indices)
						rets := [4]float64{
							hist[indices[0]],
							hist[indices[1]],
							hist[indices[2]],
							hist[indices[3]],
						}
						vRet := archsimd.LoadFloat64x4Slice(rets[:])

						terSlice := [4]float64{terPerStep[t], terPerStep[t], terPerStep[t], terPerStep[t]}
						vTer := archsimd.LoadFloat64x4Slice(terSlice[:])

						weightSlice := [4]float64{weights[t], weights[t], weights[t], weights[t]}
						vWeight := archsimd.LoadFloat64x4Slice(weightSlice[:])

						diff := vRet.Sub(vTer)
						weighted := diff.Mul(vWeight)
						stepRet = stepRet.Add(weighted)
					}

					// Store step returns to apply math.Log
					stepRetSlice := [4]float64{}
					stepRet.StoreSlice(stepRetSlice[:])
					accumulatedLogReturns[0] += math.Log(1.0 + stepRetSlice[0])
					accumulatedLogReturns[1] += math.Log(1.0 + stepRetSlice[1])
					accumulatedLogReturns[2] += math.Log(1.0 + stepRetSlice[2])
					accumulatedLogReturns[3] += math.Log(1.0 + stepRetSlice[3])
				}

				for b := 0; b < 4; b++ {
					results[sim+b] = math.Exp(accumulatedLogReturns[b]/float64(years)) - 1.0
				}
			}

			// Remainder
			for sim := batchEnd; sim < endIndex; sim++ {
				totalLogReturn := 0.0
				for step := 0; step < totalSteps; step++ {
					weekIndex := r.IntN(poolSize)
					stepReturn := 0.0
					for t := 0; t < numTrackers; t++ {
						stepReturn += (history[t][weekIndex] - terPerStep[t]) * weights[t]
					}
					totalLogReturn += math.Log(1.0 + stepReturn)
				}
				results[sim] = math.Exp(totalLogReturn/float64(years)) - 1.0
			}
		}(start, end)
	}

	wg.Wait()
	sort.Float64s(results)
	idx := int(float64(len(results)) * (percent / 100.0))
	if idx >= len(results) {
		idx = len(results) - 1
	}
	return results[idx]
}

func (s *ProjectionService) runTrackerMonteCarloSIMD(history []float64, ter float64, simulations, years int, percent float64) float64 {
	if !archsimd.X86.AVX2() {
		return s.runTrackerMonteCarloParallel(history, ter, simulations, years, percent)
	}

	const stepsPerYear = 52
	totalSteps := years * stepsPerYear
	poolSize := len(history)
	if poolSize == 0 {
		return 0.05
	}

	results := make([]float64, simulations)
	numCPUs := runtime.NumCPU()
	var wg sync.WaitGroup
	wg.Add(numCPUs)

	simsPerWorker := simulations / numCPUs
	for w := 0; w < numCPUs; w++ {
		start := w * simsPerWorker
		end := start + simsPerWorker
		if w == numCPUs-1 {
			end = simulations
		}

		go func(startIndex, endIndex int) {
			defer wg.Done()
			r := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(startIndex)))
			terPerStep := (ter / 100.0) / float64(stepsPerYear)

			const batchSize = 4
			batchEnd := startIndex + ((endIndex-startIndex)/batchSize)*batchSize

			for sim := startIndex; sim < batchEnd; sim += batchSize {
				accumulatedLogReturns := [4]float64{}
				for step := 0; step < totalSteps; step++ {
					accumulatedLogReturns[0] += math.Log(1.0 + history[r.IntN(poolSize)] - terPerStep)
					accumulatedLogReturns[1] += math.Log(1.0 + history[r.IntN(poolSize)] - terPerStep)
					accumulatedLogReturns[2] += math.Log(1.0 + history[r.IntN(poolSize)] - terPerStep)
					accumulatedLogReturns[3] += math.Log(1.0 + history[r.IntN(poolSize)] - terPerStep)
				}
				for b := 0; b < 4; b++ {
					results[sim+b] = math.Exp(accumulatedLogReturns[b]/float64(years)) - 1.0
				}
			}

			for sim := batchEnd; sim < endIndex; sim++ {
				totalLogReturn := 0.0
				for step := 0; step < totalSteps; step++ {
					weekIndex := r.IntN(poolSize)
					totalLogReturn += math.Log(1.0 + history[weekIndex] - terPerStep)
				}
				results[sim] = math.Exp(totalLogReturn/float64(years)) - 1.0
			}
		}(start, end)
	}

	wg.Wait()
	sort.Float64s(results)
	idx := int(float64(len(results)) * (percent / 100.0))
	if idx >= len(results) {
		idx = len(results) - 1
	}
	return results[idx]
}
