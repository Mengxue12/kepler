// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodePowerEstimator_InitAndEnergy(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stat"), []byte("cpu  1 0 0 0 0 0 0 0 0\n"), 0o644))

	m, err := NewNodePowerEstimator(dir, WithEstimatorMaxPlatformWatts(100))
	require.NoError(t, err)
	require.NoError(t, m.Init())

	pz, err := m.PrimaryEnergyZone()
	require.NoError(t, err)

	e0, err := pz.Energy()
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	e1, err := pz.Energy()
	require.NoError(t, err)
	assert.Greater(t, e1.MicroJoules(), e0.MicroJoules())

	zones, err := m.Zones()
	require.NoError(t, err)
	assert.Len(t, zones, 1)

	assert.Equal(t, "platform", pz.Name())
	assert.Equal(t, "node-power-estimator", m.Name())
}

func TestNodePowerEstimator_UnixSocketPower(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stat"), []byte("cpu  1 0 0 0 0 0 0 0 0\n"), 0o644))

	sockPath := filepath.Join(dir, "estimator.sock")
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				br := bufio.NewReader(c)
				_, _ = br.ReadBytes('\n')
				_, _ = c.Write([]byte(`{"power_watts":50}` + "\n"))
			}(c)
		}
	}()

	m, err := NewNodePowerEstimator(dir,
		WithEstimatorMaxPlatformWatts(100),
		WithEstimatorSocketPath(sockPath),
		WithEstimatorSocketTimeout(2*time.Second),
	)
	require.NoError(t, err)
	require.NoError(t, m.Init())

	pz, err := m.PrimaryEnergyZone()
	require.NoError(t, err)

	_, err = pz.Energy()
	require.NoError(t, err)

	time.Sleep(20 * time.Millisecond)
	e50, err := pz.Energy()
	require.NoError(t, err)

	time.Sleep(20 * time.Millisecond)
	e50b, err := pz.Energy()
	require.NoError(t, err)

	// 50 W integrates roughly half the microjoules per second of the 100 W fallback.
	dt := 0.020
	maxPerInterval := (100 * Watt).MicroWatts() * dt
	assert.Less(t, float64(e50.MicroJoules()), maxPerInterval*0.85)
	assert.Greater(t, e50b.MicroJoules(), e50.MicroJoules())
}

func TestResponseToPower(t *testing.T) {
	w := 12.5
	p, err := responseToPower(&estimatorSocketResponse{PowerWatts: &w})
	require.NoError(t, err)
	assert.InDelta(t, 12.5, p.Watts(), 1e-9)

	mw := 3400.0
	p2, err := responseToPower(&estimatorSocketResponse{PowerMilliWatts: &mw})
	require.NoError(t, err)
	assert.InDelta(t, 3.4, p2.Watts(), 1e-9)

	w2 := 10.0
	mw2 := 5000.0
	p3, err := responseToPower(&estimatorSocketResponse{PowerWatts: &w2, PowerMilliWatts: &mw2})
	require.NoError(t, err)
	assert.InDelta(t, 10.0, p3.Watts(), 1e-9)

	_, err = responseToPower(&estimatorSocketResponse{})
	require.Error(t, err)
}
