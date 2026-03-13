/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package stats

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sustainable-computing-io/kepler/pkg/bpf"
	"github.com/sustainable-computing-io/kepler/pkg/collector/stats/types"
	"github.com/sustainable-computing-io/kepler/pkg/config"
	"github.com/sustainable-computing-io/kepler/pkg/sensors/accelerator/gpu"
	"k8s.io/klog/v2"
)

var (
	// AvailableAbsEnergyMetrics holds a list of absolute energy metrics
	AvailableAbsEnergyMetrics []string
	// AvailableDynEnergyMetrics holds a list of dynamic energy metrics
	AvailableDynEnergyMetrics []string
	// AvailableIdleEnergyMetrics holds a list of idle energy metrics
	AvailableIdleEnergyMetrics []string
)

type Stats struct {
	ResourceUsage map[string]types.UInt64StatCollection
	EnergyUsage   map[string]types.UInt64StatCollection
}

// NewStats creates a new Stats instance
func NewStats(bpfSupportedMetrics bpf.SupportedMetrics) *Stats {
	m := &Stats{
		ResourceUsage: make(map[string]types.UInt64StatCollection),
		EnergyUsage:   make(map[string]types.UInt64StatCollection),
	}

	// initialize the energy metrics in the map
	energyMetrics := []string{}
	energyMetrics = append(energyMetrics, AvailableDynEnergyMetrics...)
	energyMetrics = append(energyMetrics, AvailableAbsEnergyMetrics...)
	energyMetrics = append(energyMetrics, AvailableIdleEnergyMetrics...)
	for _, metricName := range energyMetrics {
		m.EnergyUsage[metricName] = types.NewUInt64StatCollection()
	}

	// initialize the resource utilization metrics in the map
	resMetrics := []string{}
	for metricName := range bpfSupportedMetrics.HardwareCounters {
		resMetrics = append(resMetrics, metricName)
	}
	for metricName := range bpfSupportedMetrics.SoftwareCounters {
		resMetrics = append(resMetrics, metricName)
	}
	for _, metricName := range resMetrics {
		m.ResourceUsage[metricName] = types.NewUInt64StatCollection()
	}
	// Disk metrics can be collected directly from cgroup/procfs without process-level BPF counters.
	m.ResourceUsage[config.DiskRead] = types.NewUInt64StatCollection()
	m.ResourceUsage[config.DiskWrite] = types.NewUInt64StatCollection()
	// Network metrics can be collected directly from /proc without process-level BPF counters.
	m.ResourceUsage[config.NetRX] = types.NewUInt64StatCollection()
	m.ResourceUsage[config.NetTX] = types.NewUInt64StatCollection()
	// Node memory bandwidth metrics are collected from procfs and exported as cumulative bytes.
	m.ResourceUsage[config.MemRead] = types.NewUInt64StatCollection()
	m.ResourceUsage[config.MemWrite] = types.NewUInt64StatCollection()

	if config.EnabledGPU && gpu.IsGPUCollectionSupported() {
		m.ResourceUsage[config.GPUComputeUtilization] = types.NewUInt64StatCollection()
		m.ResourceUsage[config.GPUMemUtilization] = types.NewUInt64StatCollection()
	}

	if config.IsExposeQATMetricsEnabled() {
		m.ResourceUsage[config.QATUtilization] = types.NewUInt64StatCollection()
	}

	return m
}

// ResetDeltaValues reset all current value to 0
func (m *Stats) ResetDeltaValues() {
	for _, stat := range m.ResourceUsage {
		stat.ResetDeltaValues()
	}
	for metric, stat := range m.EnergyUsage {
		if strings.Contains(metric, "idle") {
			continue // do not reset the idle power metrics
		}
		stat.ResetDeltaValues()
	}
}

func (m *Stats) String() string {
	return fmt.Sprintf(
		"\tDyn ePkg (mJ): %s (eCore: %s eDram: %s eUncore: %s) eGPU (mJ): %s eOther (mJ): %s platform (mJ): %s \n"+
			"\tIdle ePkg (mJ): %s (eCore: %s eDram: %s eUncore: %s) eGPU (mJ): %s eOther (mJ): %s platform (mJ): %s \n"+
			"\tResUsage: %v\n",
		m.EnergyUsage[config.DynEnergyInPkg],
		m.EnergyUsage[config.DynEnergyInCore],
		m.EnergyUsage[config.DynEnergyInDRAM],
		m.EnergyUsage[config.DynEnergyInUnCore],
		m.EnergyUsage[config.DynEnergyInGPU],
		m.EnergyUsage[config.DynEnergyInOther],
		m.EnergyUsage[config.DynEnergyInPlatform],
		m.EnergyUsage[config.IdleEnergyInPkg],
		m.EnergyUsage[config.IdleEnergyInCore],
		m.EnergyUsage[config.IdleEnergyInDRAM],
		m.EnergyUsage[config.IdleEnergyInUnCore],
		m.EnergyUsage[config.IdleEnergyInGPU],
		m.EnergyUsage[config.IdleEnergyInOther],
		m.EnergyUsage[config.IdleEnergyInPlatform],
		m.ResourceUsage)
}

// UpdateDynEnergy calculates the dynamic energy
func (m *Stats) UpdateDynEnergy() {
	for pkgID := range m.EnergyUsage[config.AbsEnergyInPkg] {
		m.CalcDynEnergy(config.AbsEnergyInPkg, config.IdleEnergyInPkg, config.DynEnergyInPkg, pkgID)
		m.CalcDynEnergy(config.AbsEnergyInCore, config.IdleEnergyInCore, config.DynEnergyInCore, pkgID)
		m.CalcDynEnergy(config.AbsEnergyInUnCore, config.IdleEnergyInUnCore, config.DynEnergyInUnCore, pkgID)
		m.CalcDynEnergy(config.AbsEnergyInDRAM, config.IdleEnergyInDRAM, config.DynEnergyInDRAM, pkgID)
	}
	for sensorID := range m.EnergyUsage[config.AbsEnergyInPlatform] {
		m.CalcDynEnergy(config.AbsEnergyInPlatform, config.IdleEnergyInPlatform, config.DynEnergyInPlatform, sensorID)
	}
	// gpu metric
	if config.EnabledGPU && gpu.IsGPUCollectionSupported() {
		for gpuID := range m.EnergyUsage[config.AbsEnergyInGPU] {
			m.CalcDynEnergy(config.AbsEnergyInGPU, config.IdleEnergyInGPU, config.DynEnergyInGPU, gpuID)
		}
	}
}

// CalcDynEnergy calculate the difference between the absolute and idle energy/power
func (m *Stats) CalcDynEnergy(absM, idleM, dynM, id string) {
	if _, exist := m.EnergyUsage[absM][id]; !exist {
		return
	}
	totalPower := m.EnergyUsage[absM][id].GetDelta()
	klog.V(6).Infof("Absolute Energy %s stat: %v, totalPower: %v (%s)", absM, m.EnergyUsage[absM], totalPower, id)
	idlePower := uint64(0)
	if idleStat, found := m.EnergyUsage[idleM][id]; found {
		idlePower = idleStat.GetDelta()
		klog.V(6).Infof("Idle Energy %s stat: %v, idlePower: %v (%s)", idleM, m.EnergyUsage[idleM], idlePower, id)
	}
	dynPower := calcDynEnergy(totalPower, idlePower)
	m.EnergyUsage[dynM].SetDeltaStat(id, dynPower)
	klog.V(6).Infof("Dynamic Energy %s stat: %v, dynPower: %v (%s)", dynM, m.EnergyUsage[dynM], dynPower, id)
}

func calcDynEnergy(totalE, idleE uint64) uint64 {
	if (totalE == 0) || (idleE == 0) {
		klog.V(6).Infof("totalE or idleE is 0: totalE: %d, idleE: %d", totalE, idleE)
		return 0
	} else if totalE < idleE {
		klog.V(6).Infof("Need to be checked: totalE < idleE: totalE: %d, idleE: %d", totalE, idleE)
		return 0
	}
	return totalE - idleE
}

func normalize(val float64, shouldNormalize bool) float64 {
	if shouldNormalize {
		return val / float64(config.SamplePeriodSec)
	}
	return val
}

type linearExprTerm struct {
	coef   float64
	metric string
}

func stripAllSpaces(s string) string {
	// Keep this simple (no unicode categories); metric names here are ascii.
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "\t", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}

// parseLinearExpr parses a simple linear expression like:
//
//	"bpf_cpu_time_ms+2*cpu_cycles"
//
// Grammar:
//
//	expr  := term ("+" term)*
//	term  := (coef "*")? metric
//	coef  := float (e.g., 2, 2.5, -1)
//	metric:= a metric name key in Stats.ResourceUsage
func parseLinearExpr(expr string) ([]linearExprTerm, bool) {
	expr = stripAllSpaces(expr)
	if expr == "" {
		return nil, false
	}
	// Only treat it as an expression if it actually contains operators.
	if !strings.Contains(expr, "+") && !strings.Contains(expr, "*") {
		return nil, false
	}
	parts := strings.Split(expr, "+")
	terms := make([]linearExprTerm, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
		coef := 1.0
		metric := part
		if strings.Contains(part, "*") {
			mul := strings.Split(part, "*")
			if len(mul) != 2 || mul[0] == "" || mul[1] == "" {
				return nil, false
			}
			parsedCoef, err := strconv.ParseFloat(mul[0], 64)
			if err != nil {
				return nil, false
			}
			coef = parsedCoef
			metric = mul[1]
		}
		terms = append(terms, linearExprTerm{coef: coef, metric: metric})
	}
	if len(terms) == 0 {
		return nil, false
	}
	return terms, true
}

func (m *Stats) evalResourceUsageExpr(expr string, shouldNormalize bool) (float64, bool) {
	terms, ok := parseLinearExpr(expr)
	if !ok {
		return 0, false
	}
	sum := 0.0
	for _, t := range terms {
		if usage, exists := m.ResourceUsage[t.metric]; exists {
			sum += t.coef * float64(usage.SumAllDeltaValues())
		} else {
			// Unknown metric in expression; treat as 0 to keep behavior predictable.
			klog.V(10).Infof("Unknown resource usage metric in expression: %q (expr=%q), using 0", t.metric, expr)
		}
	}
	return normalize(sum, shouldNormalize), true
}

// ToEstimatorValues return values regarding metricNames.
// The metrics can be related to resource utilization or power consumption.
// Since Kepler collects metrics at intervals of SamplePeriodSec, which is greater than 1 second, and the power models are trained to estimate power in 1 second interval,
// it is necessary to normalize the resource utilization by the SamplePeriodSec. Note that this is important because the power curve can be different for higher or lower resource usage within 1 second interval.
func (m *Stats) ToEstimatorValues(featuresName []string, shouldNormalize bool) []float64 {
	featureValues := []float64{}
	for _, feature := range featuresName {
		// Support linear combinations for resource usage features, e.g.:
		//   "bpf_cpu_time_ms+2*cpu_cycles"
		if value, ok := m.evalResourceUsageExpr(feature, shouldNormalize); ok {
			featureValues = append(featureValues, value)
			continue
		}
		// verify all metrics that are part of the node resource usage metrics
		if value, exists := m.ResourceUsage[feature]; exists {
			featureValues = append(featureValues, normalize(float64(value.SumAllDeltaValues()), shouldNormalize))
			continue
		}
		// some features are not related to resource utilization, such as power metrics
		switch feature {
		case config.GeneralUsageMetric: // is an empty string for UNCORE and OTHER resource usage
			featureValues = append(featureValues, 0)

		case config.DynEnergyInPkg: // for dynamic PKG power consumption
			value := normalize(float64(m.EnergyUsage[config.DynEnergyInPkg].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		case config.DynEnergyInCore: // for dynamic CORE power consumption
			value := normalize(float64(m.EnergyUsage[config.DynEnergyInCore].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		case config.DynEnergyInDRAM: // for dynamic PKG power consumption
			value := normalize(float64(m.EnergyUsage[config.DynEnergyInDRAM].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		case config.DynEnergyInUnCore: // for dynamic UNCORE power consumption
			value := normalize(float64(m.EnergyUsage[config.DynEnergyInUnCore].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		case config.DynEnergyInOther: // for dynamic OTHER power consumption
			value := normalize(float64(m.EnergyUsage[config.DynEnergyInOther].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		case config.DynEnergyInPlatform: // for dynamic PLATFORM power consumption
			value := normalize(float64(m.EnergyUsage[config.DynEnergyInPlatform].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		case config.DynEnergyInGPU: // for dynamic GPU power consumption
			value := normalize(float64(m.EnergyUsage[config.DynEnergyInGPU].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		case config.IdleEnergyInPkg: // for idle PKG power consumption
			value := normalize(float64(m.EnergyUsage[config.IdleEnergyInPkg].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		case config.IdleEnergyInCore: // for idle CORE power consumption
			value := normalize(float64(m.EnergyUsage[config.IdleEnergyInCore].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		case config.IdleEnergyInDRAM: // for idle PKG power consumption
			value := normalize(float64(m.EnergyUsage[config.IdleEnergyInDRAM].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		case config.IdleEnergyInUnCore: // for idle UNCORE power consumption
			value := normalize(float64(m.EnergyUsage[config.IdleEnergyInUnCore].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		case config.IdleEnergyInOther: // for idle OTHER power consumption
			value := normalize(float64(m.EnergyUsage[config.IdleEnergyInOther].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		case config.IdleEnergyInPlatform: // for idle PLATFORM power consumption
			value := normalize(float64(m.EnergyUsage[config.IdleEnergyInPlatform].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		case config.IdleEnergyInGPU: // for idle GPU power consumption
			value := normalize(float64(m.EnergyUsage[config.IdleEnergyInGPU].SumAllDeltaValues()), shouldNormalize)
			featureValues = append(featureValues, value)

		default:
			klog.V(10).Infof("Unknown node feature: %s, adding 0 value", feature)
			featureValues = append(featureValues, 0)
		}
	}
	return featureValues
}
