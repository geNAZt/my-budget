//go:build !arm64 && (!amd64 || !experiment_simd)

package service

import (
	"github.com/genazt/my-budget-script/web/backend/internal/domain"
)

func (s *ProjectionService) runMonteCarloSIMD(v *domain.AssetVersion, history [][]float64, simulations, years int, percent float64) float64 {
	return s.runMonteCarloParallel(v, history, simulations, years, percent)
}

func (s *ProjectionService) runTrackerMonteCarloSIMD(history []float64, ter float64, simulations, years int, percent float64) float64 {
	return s.runTrackerMonteCarloParallel(history, ter, simulations, years, percent)
}
