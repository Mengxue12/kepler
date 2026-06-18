// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"errors"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sustainable-computing-io/kepler/internal/device"
)

type mockCpufreqReader struct {
	policiesFunc func() ([]device.CpufreqPolicy, error)
}

func (m *mockCpufreqReader) Policies() ([]device.CpufreqPolicy, error) {
	return m.policiesFunc()
}

func sampleCpufreqPolicies() []device.CpufreqPolicy {
	return []device.CpufreqPolicy{
		{
			Name:         "policy0",
			Index:        0,
			AffectedCPUs: []int{0, 1},
			CurFreqKHz:   2400000,
		},
		{
			Name:         "policy1",
			Index:        1,
			AffectedCPUs: []int{4},
			CurFreqKHz:   1800000,
		},
	}
}

func TestNewCPUFreqCollector(t *testing.T) {
	collector, err := NewCPUFreqCollector("../../device/testdata/sys")
	require.NoError(t, err)
	assert.NotNil(t, collector)
	assert.NotNil(t, collector.reader)
	assert.NotNil(t, collector.desc)
}

func TestNewCPUFreqCollectorWithReader(t *testing.T) {
	mockReader := &mockCpufreqReader{
		policiesFunc: func() ([]device.CpufreqPolicy, error) {
			return sampleCpufreqPolicies(), nil
		},
	}
	collector := newCPUFreqCollectorWithReader(mockReader)
	assert.NotNil(t, collector)
	assert.Equal(t, mockReader, collector.reader)
	assert.Contains(t, collector.desc.String(), "kepler_node_cpu_scaling_frequency_hertz")
	assert.Contains(t, collector.desc.String(), "variableLabels: {cpu,policy}")
}

func TestCPUFreqCollector_Describe(t *testing.T) {
	mockReader := &mockCpufreqReader{
		policiesFunc: func() ([]device.CpufreqPolicy, error) {
			return sampleCpufreqPolicies(), nil
		},
	}
	collector := newCPUFreqCollectorWithReader(mockReader)

	ch := make(chan *prometheus.Desc, 1)
	collector.Describe(ch)
	close(ch)

	desc := <-ch
	assert.Equal(t, collector.desc, desc)
}

func TestCPUFreqCollector_Collect_Success(t *testing.T) {
	mockReader := &mockCpufreqReader{
		policiesFunc: func() ([]device.CpufreqPolicy, error) {
			return sampleCpufreqPolicies(), nil
		},
	}
	collector := newCPUFreqCollectorWithReader(mockReader)

	ch := make(chan prometheus.Metric, 10)
	collector.Collect(ch)
	close(ch)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}

	assert.Len(t, metrics, 3)

	values := map[string]float64{}
	for _, m := range metrics {
		dtoMetric := &dto.Metric{}
		err := m.Write(dtoMetric)
		require.NoError(t, err)
		require.NotNil(t, dtoMetric.Gauge)
		require.NotNil(t, dtoMetric.Gauge.Value)

		labels := map[string]string{}
		for _, l := range dtoMetric.Label {
			labels[*l.Name] = *l.Value
		}
		key := labels["cpu"] + ":" + labels["policy"]
		values[key] = *dtoMetric.Gauge.Value
	}

	assert.Equal(t, 2400000000.0, values["0:policy0"])
	assert.Equal(t, 2400000000.0, values["1:policy0"])
	assert.Equal(t, 1800000000.0, values["4:policy1"])
}

func TestCPUFreqCollector_Collect_Error(t *testing.T) {
	mockReader := &mockCpufreqReader{
		policiesFunc: func() ([]device.CpufreqPolicy, error) {
			return nil, errors.New("failed to read cpufreq policies")
		},
	}
	collector := newCPUFreqCollectorWithReader(mockReader)

	ch := make(chan prometheus.Metric, 10)
	collector.Collect(ch)
	close(ch)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}

	assert.Len(t, metrics, 0)
}

func TestCPUFreqCollector_Collect_Concurrency(t *testing.T) {
	mockReader := &mockCpufreqReader{
		policiesFunc: func() ([]device.CpufreqPolicy, error) {
			return sampleCpufreqPolicies(), nil
		},
	}
	collector := newCPUFreqCollectorWithReader(mockReader)

	const numGoroutines = 10
	var wg sync.WaitGroup
	ch := make(chan prometheus.Metric, numGoroutines*3)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			collector.Collect(ch)
		}()
	}

	wg.Wait()
	close(ch)

	var metrics []prometheus.Metric
	for m := range ch {
		metrics = append(metrics, m)
	}

	assert.Equal(t, numGoroutines*3, len(metrics))
}
