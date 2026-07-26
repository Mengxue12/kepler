// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeHWPowerFixtures(t *testing.T, sysfsRoot string, powerUW, voltageUV, currentUA int64) {
	t.Helper()
	acpiDir := filepath.Join(sysfsRoot, "devices", "LNXSYSTM:00", "LNXSYBUS:00", "ACPI000D:00", "power_meter", "ACPI000D:00")
	batDir := filepath.Join(sysfsRoot, "devices", "LNXSYSTM:00", "LNXSYBUS:00", "PNP0C0A:00", "power_supply", "BAT0")
	require.NoError(t, os.MkdirAll(acpiDir, 0o755))
	require.NoError(t, os.MkdirAll(batDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(acpiDir, "power1_average"), []byte(strconv.FormatInt(powerUW, 10)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(batDir, "voltage_now"), []byte(strconv.FormatInt(voltageUV, 10)+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(batDir, "current_now"), []byte(strconv.FormatInt(currentUA, 10)+"\n"), 0o644))
}

func TestDiscoverHardwarePowerZones(t *testing.T) {
	sysfsRoot := t.TempDir()
	writeHWPowerFixtures(t, sysfsRoot, 15_000_000, 11_000_000, 500_000)

	zones := discoverHardwarePowerZones(sysfsRoot, slog.Default())
	require.Len(t, zones, 2)

	names := map[string]EnergyZone{}
	for _, z := range zones {
		names[z.Name()] = z
	}
	require.Contains(t, names, ZoneACPI)
	require.Contains(t, names, ZoneBattery)

	acpi := names[ZoneACPI]
	assert.Equal(t, 0, acpi.Index())
	assert.Contains(t, acpi.Path(), "ACPI000D:00")
	assert.NotContains(t, acpi.Path(), "power1_average")

	battery := names[ZoneBattery]
	assert.Equal(t, 0, battery.Index())
	assert.Contains(t, battery.Path(), filepath.Join("power_supply", "BAT0"))
	assert.NotContains(t, battery.Path(), "voltage_now")
}

func TestACPIZone_IntegratesPower(t *testing.T) {
	sysfsRoot := t.TempDir()
	writeHWPowerFixtures(t, sysfsRoot, 2_000_000, 11_000_000, 500_000) // 2W

	zones := discoverHardwarePowerZones(sysfsRoot, slog.Default())
	var acpi EnergyZone
	for _, z := range zones {
		if z.Name() == ZoneACPI {
			acpi = z
			break
		}
	}
	require.NotNil(t, acpi)

	e0, err := acpi.Energy()
	require.NoError(t, err)

	time.Sleep(20 * time.Millisecond)

	e1, err := acpi.Energy()
	require.NoError(t, err)
	assert.Greater(t, e1.MicroJoules(), e0.MicroJoules())
}

func TestBatteryZone_ExportsWhenCurrentAboveSentinel(t *testing.T) {
	sysfsRoot := t.TempDir()
	// 11V * 0.5A = 5.5W
	writeHWPowerFixtures(t, sysfsRoot, 1_000_000, 11_000_000, 500_000)

	zones := discoverHardwarePowerZones(sysfsRoot, slog.Default())
	var battery EnergyZone
	for _, z := range zones {
		if z.Name() == ZoneBattery {
			battery = z
			break
		}
	}
	require.NotNil(t, battery)

	_, err := battery.Energy()
	require.NoError(t, err)
	time.Sleep(20 * time.Millisecond)
	e1, err := battery.Energy()
	require.NoError(t, err)
	assert.Greater(t, e1.MicroJoules(), uint64(0))
}

func TestBatteryZone_ChargingSentinelOmitsReading(t *testing.T) {
	sysfsRoot := t.TempDir()
	writeHWPowerFixtures(t, sysfsRoot, 1_000_000, 11_000_000, batteryCurrentChargingSentinel)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	zones := discoverHardwarePowerZones(sysfsRoot, logger)
	var battery EnergyZone
	for _, z := range zones {
		if z.Name() == ZoneBattery {
			battery = z
			break
		}
	}
	require.NotNil(t, battery)

	_, err := battery.Energy()
	assert.ErrorIs(t, err, ErrEnergyUnavailable)
	assert.Contains(t, buf.String(), "Device is charging now, no current reported.")
}

func TestRaplPowerMeter_IncludesHardwareZones(t *testing.T) {
	sysfsRoot := t.TempDir()
	writeHWPowerFixtures(t, sysfsRoot, 1_000_000, 11_000_000, 500_000)

	mockReader := &mockRaplReader{}
	pkg := &MockRaplZone{
		name:  "package",
		path:  "/sys/class/powercap/intel-rapl/intel-rapl:0",
		index: 0,
	}
	mockReader.On("Zones").Return([]EnergyZone{pkg}, nil)

	meter, err := NewCPUPowerMeter(validSysFSPath, WithSysFSReader(mockReader))
	require.NoError(t, err)
	meter.sysfsPath = sysfsRoot

	zones, err := meter.Zones()
	require.NoError(t, err)

	names := make([]string, len(zones))
	for i, z := range zones {
		names[i] = z.Name()
	}
	assert.Contains(t, names, "package")
	assert.Contains(t, names, ZoneACPI)
	assert.Contains(t, names, ZoneBattery)
}

func TestNodePowerEstimator_IncludesHardwareZones(t *testing.T) {
	procDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(procDir, "stat"), []byte("cpu  1 0 0 0 0 0 0 0 0\n"), 0o644))

	sysfsRoot := t.TempDir()
	writeHWPowerFixtures(t, sysfsRoot, 1_000_000, 11_000_000, 500_000)

	m, err := NewNodePowerEstimator(procDir,
		WithEstimatorMaxPlatformWatts(100),
		WithEstimatorSysFSPath(sysfsRoot),
	)
	require.NoError(t, err)
	require.NoError(t, m.Init())

	zones, err := m.Zones()
	require.NoError(t, err)
	names := make([]string, len(zones))
	for i, z := range zones {
		names[i] = z.Name()
	}
	assert.Contains(t, names, "platform")
	assert.Contains(t, names, ZoneACPI)
	assert.Contains(t, names, ZoneBattery)
}

func TestDiscoverHardwarePowerZones_Missing(t *testing.T) {
	zones := discoverHardwarePowerZones(t.TempDir(), slog.Default())
	assert.Empty(t, zones)
	assert.Empty(t, discoverHardwarePowerZones("", slog.Default()))
}
