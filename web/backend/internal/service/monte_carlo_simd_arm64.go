//go:build arm64

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

func (s *ProjectionService) runMonteCarloSIMD(v *domain.AssetVersion, history [][]float64, simulations, years int, percent float64) float64 {
	const stepsPerYear = 52
	totalSteps := years * stepsPerYear
	poolSize := 9999
	for _, h := range history {
		if len(h) < poolSize {
			poolSize = len(h)
		}
	}
	if poolSize == 9999 || poolSize == 0 {
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
			// Use math/rand/v2 for better performance
			r := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(startIndex)))

			numTrackers := len(history)
			weights := make([]float64, numTrackers)
			terPerStep := make([]float64, numTrackers)
			for i := 0; i < numTrackers; i++ {
				weights[i] = v.ETFConfig[i].Percentage
				terPerStep[i] = (v.ETFConfig[i].TER / 100.0) / float64(stepsPerYear)
			}

			// 4-way unroll for NEON efficiency
			const batchSize = 4
			batchEnd := startIndex + ((endIndex-startIndex)/batchSize)*batchSize

			for sim := startIndex; sim < batchEnd; sim += batchSize {
				var logReturns [batchSize]float64

				for step := 0; step < totalSteps; step++ {
					// Pre-fetch random indices for the batch
					idx0 := r.IntN(poolSize)
					idx1 := r.IntN(poolSize)
					idx2 := r.IntN(poolSize)
					idx3 := r.IntN(poolSize)

					// Tight loop designed for compiler auto-vectorization
					for t := 0; t < numTrackers; t++ {
						hist := history[t]
						w := weights[t]
						ter := terPerStep[t]

						logReturns[0] += (hist[idx0] - ter) * w
						logReturns[1] += (hist[idx1] - ter) * w
						logReturns[2] += (hist[idx2] - ter) * w
						logReturns[3] += (hist[idx3] - ter) * w
					}
				}

				for b := 0; b < batchSize; b++ {
					results[sim+b] = math.Exp(logReturns[b]/float64(years)) - 1.0
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
					totalLogReturn += stepReturn
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
				var logReturns [batchSize]float64
				for step := 0; step < totalSteps; step++ {
					idx0 := r.IntN(poolSize)
					idx1 := r.IntN(poolSize)
					idx2 := r.IntN(poolSize)
					idx3 := r.IntN(poolSize)

					logReturns[0] += history[idx0] - terPerStep
					logReturns[1] += history[idx1] - terPerStep
					logReturns[2] += history[idx2] - terPerStep
					logReturns[3] += history[idx3] - terPerStep
				}
				for b := 0; b < batchSize; b++ {
					results[sim+b] = math.Exp(logReturns[b]/float64(years)) - 1.0
				}
			}

			for sim := batchEnd; sim < endIndex; sim++ {
				totalLogReturn := 0.0
				for step := 0; step < totalSteps; step++ {
					weekIndex := r.IntN(poolSize)
					totalLogReturn += history[weekIndex] - terPerStep
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
