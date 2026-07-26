// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	acpiDevicePrefix    = "ACPI000D:0"
	batteryDevicePrefix = "PNP0C0A:0"
	acpiPowerFile       = "power1_average"
	batteryVoltageFile  = "voltage_now"
	batteryCurrentFile  = "current_now"

	// batteryCurrentChargingSentinel is reported by some ACPI batteries while charging
	// when instantaneous current is unavailable.
	batteryCurrentChargingSentinel int64 = 1000

	// hwPowerMaxEnergy is a synthetic wrap ceiling for power-integrated energy counters.
	hwPowerMaxEnergy = Energy(uint64(1) << 50)
)

// discoverHardwarePowerZones finds optional ACPI power-meter and battery zones under sysfs.
func discoverHardwarePowerZones(sysfsPath string, logger *slog.Logger) []EnergyZone {
	if logger == nil {
		logger = slog.Default()
	}
	if sysfsPath == "" {
		return nil
	}

	devicesRoot := filepath.Join(sysfsPath, "devices")
	var zones []EnergyZone

	if zone, ok := discoverACPIZone(devicesRoot, logger); ok {
		zones = append(zones, zone)
	}
	if zone, ok := discoverBatteryZone(devicesRoot, logger); ok {
		zones = append(zones, zone)
	}
	return zones
}

func discoverACPIZone(devicesRoot string, logger *slog.Logger) (EnergyZone, bool) {
	powerPath, err := findFileUnderMatchingDirs(devicesRoot, acpiDevicePrefix, acpiPowerFile)
	if err != nil {
		logger.Debug("ACPI power zone not available", "error", err)
		return nil, false
	}
	logger.Info("Discovered ACPI power zone", "path", filepath.Dir(powerPath))
	return newPowerIntegratingZone(ZoneACPI, filepath.Dir(powerPath), logger, func() (Power, error) {
		uw, err := readInt64File(powerPath)
		if err != nil {
			return 0, err
		}
		if uw < 0 {
			return 0, fmt.Errorf("negative %s value: %d", acpiPowerFile, uw)
		}
		// power1_average is microwatts
		return Power(uw) * MicroWatt, nil
	}), true
}

func discoverBatteryZone(devicesRoot string, logger *slog.Logger) (EnergyZone, bool) {
	voltagePath, err := findFileUnderMatchingDirs(devicesRoot, batteryDevicePrefix, batteryVoltageFile)
	if err != nil {
		logger.Debug("Battery power zone not available", "error", err)
		return nil, false
	}
	currentPath := filepath.Join(filepath.Dir(voltagePath), batteryCurrentFile)
	if _, err := os.Stat(currentPath); err != nil {
		logger.Debug("Battery current_now missing beside voltage_now", "path", currentPath, "error", err)
		return nil, false
	}

	path := filepath.Dir(voltagePath)
	logger.Info("Discovered battery power zone", "path", path)

	zone := &batteryEnergyZone{
		powerIntegratingZone: powerIntegratingZone{
			name:   ZoneBattery,
			path:   path,
			logger: logger.With("zone", ZoneBattery),
			max:    hwPowerMaxEnergy,
		},
		voltagePath: voltagePath,
		currentPath: currentPath,
	}
	zone.readPower = zone.batteryPower
	return zone, true
}

// powerIntegratingZone turns instantaneous power samples into a cumulative Energy counter.
type powerIntegratingZone struct {
	name   string
	path   string
	logger *slog.Logger
	max    Energy

	readPower func() (Power, error)

	mu         sync.Mutex
	cumulative Energy
	lastSample time.Time
}

func newPowerIntegratingZone(name, path string, logger *slog.Logger, readPower func() (Power, error)) *powerIntegratingZone {
	return &powerIntegratingZone{
		name:      name,
		path:      path,
		logger:    logger.With("zone", name),
		max:       hwPowerMaxEnergy,
		readPower: readPower,
	}
}

func (z *powerIntegratingZone) Name() string { return z.name }
func (z *powerIntegratingZone) Index() int   { return 0 }
func (z *powerIntegratingZone) Path() string { return z.path }
func (z *powerIntegratingZone) MaxEnergy() Energy {
	return z.max
}

func (z *powerIntegratingZone) Energy() (Energy, error) {
	z.mu.Lock()
	defer z.mu.Unlock()

	now := time.Now()
	if z.lastSample.IsZero() {
		z.lastSample = now
		return z.cumulative, nil
	}

	dt := now.Sub(z.lastSample).Seconds()
	if dt <= 0 {
		return z.cumulative, nil
	}

	power, err := z.readPower()
	if err != nil {
		return z.cumulative, err
	}
	if power > 0 {
		z.cumulative += Energy(power.MicroWatts() * dt)
		if z.max > 0 {
			z.cumulative %= z.max
		}
	}
	z.lastSample = now
	return z.cumulative, nil
}

type batteryEnergyZone struct {
	powerIntegratingZone
	voltagePath string
	currentPath string

	loggedCharging bool
}

func (z *batteryEnergyZone) batteryPower() (Power, error) {
	voltageUV, err := readInt64File(z.voltagePath)
	if err != nil {
		return 0, err
	}
	currentUA, err := readInt64File(z.currentPath)
	if err != nil {
		return 0, err
	}

	if currentUA == batteryCurrentChargingSentinel {
		if !z.loggedCharging {
			z.logger.Info("Device is charging now, no current reported.")
			z.loggedCharging = true
		}
		return 0, nil
	}
	z.loggedCharging = false

	if currentUA <= batteryCurrentChargingSentinel {
		return 0, nil
	}
	if voltageUV <= 0 {
		return 0, fmt.Errorf("non-positive %s value: %d", batteryVoltageFile, voltageUV)
	}

	// voltage_now: µV, current_now: µA → W = µV * µA / 1e12
	watts := (float64(voltageUV) * float64(currentUA)) / 1e12
	return Power(watts) * Watt, nil
}

// findFileUnderMatchingDirs walks devicesRoot for directories whose base name has prefix
// (e.g. ACPI000D:0 / PNP0C0A:0) and returns the first matching filename beneath them.
func findFileUnderMatchingDirs(devicesRoot, dirPrefix, filename string) (string, error) {
	if _, err := os.Stat(devicesRoot); err != nil {
		return "", err
	}

	var match string
	err := filepath.WalkDir(devicesRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Skip unreadable nodes; keep searching.
			return nil
		}
		if match != "" {
			return fs.SkipAll
		}
		if !d.IsDir() || !strings.HasPrefix(d.Name(), dirPrefix) {
			return nil
		}
		found, err := findNamedFile(path, filename)
		if err != nil {
			return nil
		}
		match = found
		return fs.SkipAll
	})
	if match != "" {
		return match, nil
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("no %s under directories matching %s*", filename, dirPrefix)
}

func findNamedFile(root, filename string) (string, error) {
	var match string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if match != "" {
			return fs.SkipAll
		}
		if d.IsDir() || d.Name() != filename {
			return nil
		}
		match = path
		return fs.SkipAll
	})
	if match != "" {
		return match, nil
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s not found under %s", filename, root)
}

func readInt64File(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(b))
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}
	return v, nil
}
