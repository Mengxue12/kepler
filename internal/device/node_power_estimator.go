// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// WithEstimatorMaxPlatformWatts sets the assumed maximum platform power (watts) used as a
// fixed-power fallback when no unix estimator is available or when the socket request fails.
// The monitor still splits active vs idle using /proc/stat CPU usage, matching the RAPL code path.
func WithEstimatorMaxPlatformWatts(w float64) NodePowerEstimatorOption {
	return func(e *nodePowerEstimator) {
		if w > 0 {
			e.maxPlatformWatts = w
		}
	}
}

// WithEstimatorSocketPath sets a Unix domain socket path. When non-empty and a socket exists at
// that path, each Energy() sample requests JSON power from the sidecar; otherwise integration
// uses the fixed max-platform-watts value (fake estimator).
func WithEstimatorSocketPath(path string) NodePowerEstimatorOption {
	return func(e *nodePowerEstimator) {
		e.socketPath = strings.TrimSpace(path)
	}
}

// WithEstimatorSocketTimeout sets the dial/read/write deadline for unix estimator requests.
// Non-positive values are replaced by a default when a socket path is configured.
func WithEstimatorSocketTimeout(d time.Duration) NodePowerEstimatorOption {
	return func(e *nodePowerEstimator) {
		e.socketTimeout = d
	}
}

// nodePowerEstimator implements CPUPowerMeter by integrating platform power between Energy()
// samples. Power is either obtained from a unix-socket JSON sidecar when configured and
// available, or from a constant max-platform-watts ceiling (legacy fake estimator behavior).
type nodePowerEstimator struct {
	logger *slog.Logger

	procPath         string
	maxPlatformWatts float64

	socketPath    string
	socketTimeout time.Duration

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
	if e.socketPath != "" && e.socketTimeout <= 0 {
		e.socketTimeout = 2 * time.Second
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

	if e.socketPath != "" {
		e.logger.Info("Node power estimator initialized",
			"mode", "unix-json when socket present, else fixed-watts fallback",
			"socket-path", e.socketPath,
			"socket-timeout", e.socketTimeout,
			"fallback-max-platform-watts", e.maxPlatformWatts,
		)
	} else {
		e.logger.Info("Node power estimator initialized (fixed max platform power)",
			"max-platform-watts", e.maxPlatformWatts,
		)
	}
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

	psyPower := e.platformPowerMicroWatts()
	delta := Energy(psyPower.MicroWatts() * dt)
	e.cumulative += delta
	e.lastSample = now

	return e.cumulative, nil
}

// platformPowerMicroWatts returns instantaneous platform power in MicroWatts: from the unix
// estimator when configured and the path is a live socket, else the fixed max-platform-watts fallback.
func (e *nodePowerEstimator) platformPowerMicroWatts() Power {
	if e.socketPath != "" && isUnixSocket(e.socketPath) {
		p, err := queryEstimatorPowerUnix(e.socketPath, e.socketTimeout)
		if err == nil {
			return p
		}
		e.logger.Debug("unix power estimator unavailable; using fixed max platform watts",
			"path", e.socketPath,
			"error", err,
		)
	}
	return Power(e.maxPlatformWatts * float64(Watt))
}

func isUnixSocket(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.Mode().Type() == fs.ModeSocket
}
