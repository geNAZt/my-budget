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
			r := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(startIndex)))

			// Pre-calculate weights and TERs
			numTrackers := len(history)
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
				// Initialize zero vector for accumulation
				acc := archsimd.Float64x4{}

				for step := 0; step < totalSteps; step++ {
					// We need 4 random indices
					idx0 := r.IntN(poolSize)
					idx1 := r.IntN(poolSize)
					idx2 := r.IntN(poolSize)
					idx3 := r.IntN(poolSize)

					stepRet := archsimd.Float64x4{}
					for t := 0; t < numTrackers; t++ {
						// Load 4 values from history (Gather-like)
						rets := [4]float64{
							history[t][idx0],
							history[t][idx1],
							history[t][idx2],
							history[t][idx3],
						}
						vRet := archsimd.LoadFloat64x4Slice(rets[:])

						// Create broadcast vectors for TER and Weight
						terSlice := [4]float64{terPerStep[t], terPerStep[t], terPerStep[t], terPerStep[t]}
						vTer := archsimd.LoadFloat64x4Slice(terSlice[:])

						weightSlice := [4]float64{weights[t], weights[t], weights[t], weights[t]}
						vWeight := archsimd.LoadFloat64x4Slice(weightSlice[:])

						// stepRet += (vRet - vTer) * vWeight
						diff := vRet.Sub(vTer)
						weighted := diff.Mul(vWeight)
						stepRet = stepRet.Add(weighted)
					}
					acc = acc.Add(stepRet)
				}

				// Finalize batch
				resSlice := make([]float64, 4)
				acc.StoreSlice(resSlice)
				for b := 0; b < 4; b++ {
					results[sim+b] = math.Exp(resSlice[b]/float64(years)) - 1.0
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
				acc := archsimd.Float64x4{}
				terSlice := [4]float64{terPerStep, terPerStep, terPerStep, terPerStep}
				vTer := archsimd.LoadFloat64x4Slice(terSlice[:])

				for step := 0; step < totalSteps; step++ {
					idx0 := r.IntN(poolSize)
					idx1 := r.IntN(poolSize)
					idx2 := r.IntN(poolSize)
					idx3 := r.IntN(poolSize)

					retSlice := [4]float64{
						history[idx0],
						history[idx1],
						history[idx2],
						history[idx3],
					}
					vRet := archsimd.LoadFloat64x4Slice(retSlice[:])
					acc = acc.Add(vRet.Sub(vTer))
				}

				resSlice := make([]float64, 4)
				acc.StoreSlice(resSlice)
				for b := 0; b < 4; b++ {
					results[sim+b] = math.Exp(resSlice[b]/float64(years)) - 1.0
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
