// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/prometheus/procfs/sysfs"
)

// raplPowerMeter implements CPUPowerMeter using sysfs
type raplPowerMeter struct {
	reader      sysfsReader
	sysfsPath   string
	cachedZones []EnergyZone
	logger      *slog.Logger
	zoneFilter  []string
	topZone     EnergyZone
}

type OptionFn func(*raplPowerMeter)

// sysfsReader is an interface for a sysfs filesystem used by raplPowerMeter to mock for testing
type sysfsReader interface {
	Zones() ([]EnergyZone, error)
}

// WithSysFSReader sets the sysfsReader used by raplPowerMeter
func WithSysFSReader(r sysfsReader) OptionFn {
	return func(pm *raplPowerMeter) {
		pm.reader = r
	}
}

// WithRaplLogger sets the logger for raplPowerMeter
func WithRaplLogger(logger *slog.Logger) OptionFn {
	return func(pm *raplPowerMeter) {
		pm.logger = logger.With("service", "rapl")
	}
}

// WithZoneFilter sets zone names to include for monitoring
// If empty, all zones are included
func WithZoneFilter(zones []string) OptionFn {
	return func(pm *raplPowerMeter) {
		pm.zoneFilter = zones
	}
}

// NewCPUPowerMeter creates a new CPU power meter
func NewCPUPowerMeter(sysfsPath string, opts ...OptionFn) (*raplPowerMeter, error) {
	fs, err := sysfs.NewFS(sysfsPath)
	if err != nil {
		return nil, err
	}

	ret := &raplPowerMeter{
		reader:     sysfsRaplReader{fs: fs},
		sysfsPath:  sysfsPath,
		logger:     slog.Default().With("service", "rapl"),
		zoneFilter: []string{},
	}

	for _, opt := range opts {
		opt(ret)
	}

	return ret, nil
}

func (r *raplPowerMeter) Name() string {
	return "rapl"
}

func (r *raplPowerMeter) Init() error {
	// ensure zones can be read but don't cache them
	zones, err := r.reader.Zones()
	if err != nil {
		return err
	} else if len(zones) == 0 {
		return fmt.Errorf("no RAPL zones found")
	}

	// try reading the first zone and return the error
	_, err = zones[0].Energy()
	return err
}

func (r *raplPowerMeter) needsFiltering() bool {
	return len(r.zoneFilter) != 0
}

// filterZones applies the configured zone filter
// If the filter is empty, all zones are returned
func (r *raplPowerMeter) filterZones(zones []EnergyZone) []EnergyZone {
	if !r.needsFiltering() {
		return zones
	}

	wanted := make(map[string]bool, len(r.zoneFilter))
	for _, name := range r.zoneFilter {
		wanted[strings.ToLower(name)] = true
	}
	var included, excluded []string
	filtered := make([]EnergyZone, 0, len(zones))
	for _, zone := range zones {
		if wanted[strings.ToLower(zone.Name())] {
			filtered = append(filtered, zone)
			included = append(included, zone.Name())
		} else {
			excluded = append(excluded, zone.Name())
		}
	}
	r.logger.Debug("Filtered RAPL zones", "included", included, "excluded", excluded)
	return filtered
}

func (r *raplPowerMeter) Zones() ([]EnergyZone, error) {
	// Return cached zones if already initialized
	if len(r.cachedZones) != 0 {
		return r.cachedZones, nil
	}

	zones, err := r.reader.Zones()
	if err != nil {
		return nil, err
	} else if len(zones) == 0 {
		return nil, fmt.Errorf("no RAPL zones found")
	}

	zones = r.filterZones(zones)
	if len(zones) == 0 {
		return nil, fmt.Errorf("no RAPL zones found after filtering")
	}

	// filter out non-standard zones

	stdZoneMap := map[zoneKey]EnergyZone{}
	for _, zone := range zones {
		key := zoneKey{name: zone.Name(), index: zone.Index()}

		// ignore non-standard zones if a standard zone already exists
		if existingZone, exists := stdZoneMap[key]; exists && isStandardRaplPath(existingZone.Path()) {
			continue
		}
		stdZoneMap[key] = zone
	}

	// Preserve each physical RAPL zone. Zones with the same name may have
	// independent counters and wrap boundaries on multi-socket systems.
	r.cachedZones = make([]EnergyZone, 0, len(stdZoneMap))
	for _, zone := range stdZoneMap {
		r.cachedZones = append(r.cachedZones, zone)
	}

	// Append optional ACPI / battery power zones (not subject to RAPL zone filter)
	r.cachedZones = append(r.cachedZones, discoverHardwarePowerZones(r.sysfsPath, r.logger)...)
	return r.cachedZones, nil
}

// PrimaryEnergyZone returns the zone with the highest energy coverage/priority
func (r *raplPowerMeter) PrimaryEnergyZone() (EnergyZone, error) {
	// Return cached zone if already initialized
	if r.topZone != nil {
		return r.topZone, nil
	}

	zones, err := r.Zones()
	if err != nil {
		return nil, err
	}

	if len(zones) == 0 {
		return nil, fmt.Errorf("no energy zones available")
	}

	zoneMap := map[string]EnergyZone{}
	for _, zone := range zones {
		zoneMap[strings.ToLower(zone.Name())] = zone
	}

	// Priority hierarchy for RAPL zones (highest to lowest priority)
	priorityOrder := []string{"psys", "package", "core", "dram", "uncore"}

	// Find highest priority zone available
	for _, p := range priorityOrder {
		if zone, exists := zoneMap[p]; exists {
			r.topZone = zone
			return zone, nil
		}
	}

	// Fallback to first zone if none match our preferences
	r.topZone = zones[0]
	return zones[0], nil
}

// isStandardRaplPath checks if a RAPL zone path is in the standard format
func isStandardRaplPath(path string) bool {
	return strings.Contains(path, "/intel-rapl:")
}

type sysfsRaplReader struct {
	fs sysfs.FS
}

func (r sysfsRaplReader) Zones() ([]EnergyZone, error) {
	raplZones, err := sysfs.GetRaplZones(r.fs)
	if err != nil {
		return nil, fmt.Errorf("failed to read rapl zones: %w", err)
	}

	// convert sysfs.RaplZones to EnergyZones
	energyZones := make([]EnergyZone, 0, len(raplZones))
	for _, zone := range raplZones {
		energyZones = append(energyZones, sysfsRaplZone{zone})
	}

	return energyZones, nil
}

// sysfsRaplZone implements EnergyZone using sysfs.RaplZone.
// It is an adapter for the EnergyZone interface
type sysfsRaplZone struct {
	zone sysfs.RaplZone
}

// Name returns the name of the zone
func (s sysfsRaplZone) Name() string {
	return s.zone.Name
}

// Index returns the index of the zone
func (s sysfsRaplZone) Index() int {
	return s.zone.Index
}

// Path returns the path of the zone
func (s sysfsRaplZone) Path() string {
	return s.zone.Path
}

// Energy returns the current energy value
func (s sysfsRaplZone) Energy() (Energy, error) {
	mj, err := s.zone.GetEnergyMicrojoules()
	return Energy(mj), err
}

// MaxEnergy returns the maximum energy value before wraparound
func (s sysfsRaplZone) MaxEnergy() Energy {
	return Energy(s.zone.MaxMicrojoules)
}
