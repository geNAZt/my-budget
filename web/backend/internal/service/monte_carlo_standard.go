package service

import (
	"math"
	"math/rand/v2"
	"sort"
	"time"

	"github.com/genazt/my-budget-script/web/backend/internal/domain"
)

func (s *ProjectionService) runMonteCarlo(v *domain.AssetVersion, history [][]float64, simulations, years int, percent float64) float64 {
	const stepsPerYear = 52
	poolSize := 9999
	for _, h := range history {
		if len(h) < poolSize {
			poolSize = len(h)
		}
	}
	if poolSize == 9999 || poolSize == 0 {
		return 0.05
	}

	r := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 42))
	results := make([]float64, simulations)

	numTrackers := len(history)
	weights := make([]float64, numTrackers)
	terPerStep := make([]float64, numTrackers)
	for i := 0; i < numTrackers; i++ {
		weights[i] = v.ETFConfig[i].Percentage
		terPerStep[i] = (v.ETFConfig[i].TER / 100.0) / float64(stepsPerYear)
	}

	for sim := 0; sim < simulations; sim++ {
		totalLogReturn := 0.0
		for step := 0; step < years*stepsPerYear; step++ {
			weekIndex := r.IntN(poolSize)
			stepReturn := 0.0
			for t := 0; t < numTrackers; t++ {
				stepReturn += (history[t][weekIndex] - terPerStep[t]) * weights[t]
			}
			totalLogReturn += stepReturn
		}
		results[sim] = math.Exp(totalLogReturn/float64(years)) - 1.0
	}
	sort.Float64s(results)

	idx := int(float64(len(results)) * (percent / 100.0))
	if idx >= len(results) {
		idx = len(results) - 1
	}

	return results[idx]
}

func (s *ProjectionService) runTrackerMonteCarlo(history []float64, ter float64, simulations, years int, percent float64) float64 {
	const stepsPerYear = 52
	poolSize := len(history)
	if poolSize == 0 {
		return 0.05
	}

	r := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 43))
	results := make([]float64, simulations)
	terPerStep := (ter / 100.0) / float64(stepsPerYear)

	for sim := 0; sim < simulations; sim++ {
		totalLogReturn := 0.0
		for step := 0; step < years*stepsPerYear; step++ {
			weekIndex := r.IntN(poolSize)
			totalLogReturn += history[weekIndex] - terPerStep
		}
		results[sim] = math.Exp(totalLogReturn/float64(years)) - 1.0
	}
	sort.Float64s(results)

	idx := int(float64(len(results)) * (percent / 100.0))
	if idx >= len(results) {
		idx = len(results) - 1
	}

	return results[idx]
}
