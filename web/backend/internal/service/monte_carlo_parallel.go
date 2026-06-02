package service

import (
	"math"
	"math/rand/v2"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
)

func (s *ProjectionService) runMonteCarloParallel(v *domain.AssetVersion, history [][]float64, simulations, years int, percent float64) float64 {
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
	baseSeed := uint64(time.Now().UnixNano())
	for w := 0; w < numCPUs; w++ {
		start := w * simsPerWorker
		end := start + simsPerWorker
		if w == numCPUs-1 {
			end = simulations
		}

		go func(startIndex, endIndex int) {
			defer wg.Done()
			r := rand.New(rand.NewPCG(baseSeed, uint64(startIndex)))

			// Pre-calculate weights and TERs
			weights := make([]float64, numTrackers)
			terPerStep := make([]float64, numTrackers)
			for i := 0; i < numTrackers; i++ {
				weights[i] = v.ETFConfig[i].Percentage
				terPerStep[i] = (v.ETFConfig[i].TER / 100.0) / float64(stepsPerYear)
			}

			// Batching for auto-vectorization (8 per round)
			const batchSize = 8
			logReturns := make([]float64, batchSize)

			// Correct batch loop: only go as far as we can complete a FULL batch within this worker's range
			batchEnd := startIndex + ((endIndex-startIndex)/batchSize)*batchSize

			for sim := startIndex; sim < batchEnd; sim += batchSize {
				// Initialize batch log returns
				for b := 0; b < batchSize; b++ {
					logReturns[b] = 0
				}

				// The tight loop: process batchSize simulations in log-space
				for step := 0; step < totalSteps; step++ {
					for b := 0; b < batchSize; b++ {
						// Correlated sampling
						weekIndex := r.IntN(poolSize)
						stepReturn := 0.0
						// Inner loop across trackers
						for t := 0; t < numTrackers; t++ {
							stepReturn += (history[t][weekIndex] - terPerStep[t]) * weights[t]
						}
						logReturns[b] += math.Log(1.0 + stepReturn)
					}
				}

				// Finalize batch
				for b := 0; b < batchSize; b++ {
					results[sim+b] = math.Exp(logReturns[b]/float64(years)) - 1.0
				}
			}

			// Handle remainder of simulations for this worker
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

func (s *ProjectionService) runTrackerMonteCarloParallel(history []float64, ter float64, simulations, years int, percent float64) float64 {
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
	baseSeed := uint64(time.Now().UnixNano())
	for w := 0; w < numCPUs; w++ {
		start := w * simsPerWorker
		end := start + simsPerWorker
		if w == numCPUs-1 {
			end = simulations
		}

		go func(startIndex, endIndex int) {
			defer wg.Done()
			r := rand.New(rand.NewPCG(baseSeed, uint64(startIndex)))

			const batchSize = 8
			logReturns := make([]float64, batchSize)
			terPerStep := (ter / 100.0) / float64(stepsPerYear)

			batchEnd := startIndex + ((endIndex-startIndex)/batchSize)*batchSize

			for sim := startIndex; sim < batchEnd; sim += batchSize {
				for b := 0; b < batchSize; b++ {
					logReturns[b] = 0
				}
				for step := 0; step < totalSteps; step++ {
					for b := 0; b < batchSize; b++ {
						logReturns[b] += math.Log(1.0 + history[r.IntN(poolSize)] - terPerStep)
					}
				}
				for b := 0; b < batchSize; b++ {
					results[sim+b] = math.Exp(logReturns[b]/float64(years)) - 1.0
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
