// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodePowerEstimator_InitAndEnergy(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stat"), []byte("cpu  1 0 0 0 0 0 0 0 0 0\n"), 0o644))

	m, err := NewNodePowerEstimator(dir, WithEstimatorMaxPlatformWatts(100))
	require.NoError(t, err)
	require.NoError(t, m.Init())

	e0, err := m.zone.Energy()
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	e1, err := m.zone.Energy()
	require.NoError(t, err)
	assert.Greater(t, e1.MicroJoules(), e0.MicroJoules())

	zones, err := m.Zones()
	require.NoError(t, err)
	assert.Len(t, zones, 1)

	pz, err := m.PrimaryEnergyZone()
	require.NoError(t, err)
	assert.Equal(t, "platform", pz.Name())
	assert.Equal(t, "node-power-estimator", m.Name())
}
