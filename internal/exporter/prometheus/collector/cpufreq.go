// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	prom "github.com/prometheus/client_golang/prometheus"

	"github.com/sustainable-computing-io/kepler/internal/device"
)

type cpufreqReader interface {
	Policies() ([]device.CpufreqPolicy, error)
}

type realCpufreqReader struct {
	reader device.CpufreqReader
}

func (r *realCpufreqReader) Policies() ([]device.CpufreqPolicy, error) {
	return r.reader.Policies()
}

func newCpufreqReader(sysfsPath string) (cpufreqReader, error) {
	if sysfsPath == "" {
		return nil, fmt.Errorf("sysfs path is required")
	}
	return &realCpufreqReader{reader: device.NewCpufreqReader(sysfsPath)}, nil
}

// cpuFreqCollector collects CPU scaling frequency metrics from sysfs cpufreq policies.
type cpuFreqCollector struct {
	sync.Mutex

	logger *slog.Logger
	reader cpufreqReader
	desc   *prom.Desc
}

// NewCPUFreqCollector creates a CPU frequency collector using a sysfs mount path.
func NewCPUFreqCollector(sysfsPath string) (*cpuFreqCollector, error) {
	return NewCPUFreqCollectorWithLogger(sysfsPath, slog.Default())
}

// NewCPUFreqCollectorWithLogger creates a CPU frequency collector with a logger.
func NewCPUFreqCollectorWithLogger(sysfsPath string, logger *slog.Logger) (*cpuFreqCollector, error) {
	reader, err := newCpufreqReader(sysfsPath)
	if err != nil {
		return nil, fmt.Errorf("creating cpufreq reader failed: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return newCPUFreqCollectorWithReader(reader, logger), nil
}

func newCPUFreqCollectorWithReader(reader cpufreqReader, logger *slog.Logger) *cpuFreqCollector {
	return &cpuFreqCollector{
		logger: logger.With("collector", "cpufreq"),
		reader: reader,
		desc: prom.NewDesc(
			prom.BuildFQName(keplerNS, "node", "cpu_scaling_frequency_hertz"),
			"Current CPU scaling frequency in hertz from sysfs cpufreq policy scaling_cur_freq",
			[]string{"cpu", "policy"},
			nil,
		),
	}
}

func (c *cpuFreqCollector) Describe(ch chan<- *prom.Desc) {
	ch <- c.desc
}

func (c *cpuFreqCollector) Collect(ch chan<- prom.Metric) {
	c.Lock()
	defer c.Unlock()

	policies, err := c.reader.Policies()
	if err != nil {
		c.logger.Debug("Failed to read CPU frequency policies", "error", err)
		return
	}

	for _, policy := range policies {
		freqHz := float64(policy.CurFreqKHz * 1000)
		for _, cpu := range policy.AffectedCPUs {
			ch <- prom.MustNewConstMetric(
				c.desc,
				prom.GaugeValue,
				freqHz,
				strconv.Itoa(cpu),
				policy.Name,
			)
		}
	}
}
