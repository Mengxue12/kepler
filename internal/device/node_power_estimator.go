// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// NodePowerEstimatorOption configures a software node power meter used when RAPL is unavailable.
type NodePowerEstimatorOption func(*nodePowerEstimator)

// WithEstimatorLogger sets the logger for the node power estimator.
func WithEstimatorLogger(logger *slog.Logger) NodePowerEstimatorOption {
	return func(e *nodePowerEstimator) {
		e.logger = logger.With("service", "node-power-estimator")
	}
}

// WithEstimatorMaxPlatformWatts sets the assumed maximum platform power (watts) used to
// integrate a synthetic cumulative energy counter between samples. The monitor still splits
// active vs idle using /proc/stat CPU usage, matching the RAPL code path.
func WithEstimatorMaxPlatformWatts(w float64) NodePowerEstimatorOption {
	return func(e *nodePowerEstimator) {
		if w > 0 {
			e.maxPlatformWatts = w
		}
	}
}

// nodePowerEstimator implements CPUPowerMeter by integrating a constant maximum platform
// power between Energy() samples. This approximates hardware RAPL counters when powercap
// is missing (e.g. non-Intel hosts or restricted sysfs).
type nodePowerEstimator struct {
	logger *slog.Logger

	procPath         string
	maxPlatformWatts float64

	mu sync.Mutex

	cumulative Energy
	lastSample time.Time

	zone *estimatorEnergyZone
}

type estimatorEnergyZone struct {
	m *nodePowerEstimator
}

var _ CPUPowerMeter = (*nodePowerEstimator)(nil)
var _ EnergyZone = (*estimatorEnergyZone)(nil)

// NewNodePowerEstimator constructs a CPUPowerMeter backed by a synthetic platform-energy counter.
func NewNodePowerEstimator(procPath string, opts ...NodePowerEstimatorOption) (*nodePowerEstimator, error) {
	if procPath == "" {
		return nil, fmt.Errorf("procfs path is empty")
	}
	e := &nodePowerEstimator{
		logger:           slog.Default().With("service", "node-power-estimator"),
		procPath:         procPath,
		maxPlatformWatts: 100,
	}
	for _, o := range opts {
		o(e)
	}
	z := &estimatorEnergyZone{m: e}
	e.zone = z
	return e, nil
}

func (e *nodePowerEstimator) Name() string {
	return "node-power-estimator"
}

// Init verifies procfs is readable (required for the monitor's CPU usage split).
func (e *nodePowerEstimator) Init() error {
	statPath := filepath.Join(e.procPath, "stat")
	if _, err := os.ReadFile(statPath); err != nil {
		return fmt.Errorf("node power estimator: read %s: %w", statPath, err)
	}
	e.mu.Lock()
	e.cumulative = 0
	e.lastSample = time.Time{}
	e.mu.Unlock()
	e.logger.Info("Node power estimator initialized (software platform energy counter)",
		"max-platform-watts", e.maxPlatformWatts,
	)
	return nil
}

func (e *nodePowerEstimator) Zones() ([]EnergyZone, error) {
	return []EnergyZone{e.zone}, nil
}

func (e *nodePowerEstimator) PrimaryEnergyZone() (EnergyZone, error) {
	return e.zone, nil
}

func (z *estimatorEnergyZone) Name() string {
	return "platform"
}

func (z *estimatorEnergyZone) Index() int {
	return 0
}

func (z *estimatorEnergyZone) Path() string {
	return "estimator://platform"
}

// MaxEnergy uses a large synthetic ceiling so wrap handling matches typical RAPL magnitudes.
func (z *estimatorEnergyZone) MaxEnergy() Energy {
	return Energy(uint64(1) << 50)
}

func (z *estimatorEnergyZone) Energy() (Energy, error) {
	return z.m.integrateEnergy()
}

func (e *nodePowerEstimator) integrateEnergy() (Energy, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	if e.lastSample.IsZero() {
		e.lastSample = now
		return e.cumulative, nil
	}

	dt := now.Sub(e.lastSample).Seconds()
	if dt <= 0 {
		return e.cumulative, nil
	}

	psyPower := Power(e.maxPlatformWatts * float64(Watt))
	delta := Energy(psyPower.MicroWatts() * dt)
	e.cumulative += delta
	e.lastSample = now

	return e.cumulative, nil
}
