// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"fmt"
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

	reader cpufreqReader
	desc   *prom.Desc
}

// NewCPUFreqCollector creates a CPU frequency collector using a sysfs mount path.
func NewCPUFreqCollector(sysfsPath string) (*cpuFreqCollector, error) {
	reader, err := newCpufreqReader(sysfsPath)
	if err != nil {
		return nil, fmt.Errorf("creating cpufreq reader failed: %w", err)
	}
	return newCPUFreqCollectorWithReader(reader), nil
}

func newCPUFreqCollectorWithReader(reader cpufreqReader) *cpuFreqCollector {
	return &cpuFreqCollector{
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
