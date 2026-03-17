/*
Copyright 2026.

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

package cpu

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sustainable-computing-io/kepler/pkg/collector/stats"
	"github.com/sustainable-computing-io/kepler/pkg/config"
	"k8s.io/klog/v2"
)

const (
	cpufreqPolicyDir       = "/sys/devices/system/cpu/cpufreq"
	scalingCurFreqFileName = "scaling_cur_freq"
	affectedCPUsFileName   = "affected_cpus"

	cpuFreqUsageIDSeparator = "|"
)

// UpdateNodeCPUFrequencyMetrics reads cpufreq policy current frequency and
// exports one gauge sample per (policy, cpu) tuple in KHz.
func UpdateNodeCPUFrequencyMetrics(nodeStats *stats.NodeStats) {
	policies, err := os.ReadDir(cpufreqPolicyDir)
	if err != nil {
		klog.V(4).Infof("cpufreq policy directory not available: %v", err)
		return
	}

	for _, policy := range policies {
		if !policy.IsDir() || !strings.HasPrefix(policy.Name(), "policy") {
			continue
		}
		policyName := policy.Name()
		policyPath := filepath.Join(cpufreqPolicyDir, policyName)

		freqKHz, err := readUintFromFile(filepath.Join(policyPath, scalingCurFreqFileName))
		if err != nil {
			klog.V(5).Infof("failed reading %s/%s: %v", policyName, scalingCurFreqFileName, err)
			continue
		}
		affectedCPUs, err := readCPUList(filepath.Join(policyPath, affectedCPUsFileName))
		if err != nil {
			klog.V(5).Infof("failed reading %s/%s: %v", policyName, affectedCPUsFileName, err)
			continue
		}

		for _, cpuID := range affectedCPUs {
			usageID := getCPUFrequencyUsageID(policyName, cpuID)
			nodeStats.ResourceUsage[config.CPUFrequency].SetAggrStat(usageID, freqKHz)
		}
	}
}

func readUintFromFile(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid uint in %s: %w", path, err)
	}
	return v, nil
}

func readCPUList(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return expandCPUList(strings.TrimSpace(string(data)))
}

// expandCPUList parses formats like "0-3 8-11" or "0,2,4-6".
func expandCPUList(raw string) ([]string, error) {
	if raw == "" {
		return nil, fmt.Errorf("empty cpu list")
	}
	fields := strings.Fields(strings.ReplaceAll(raw, ",", " "))
	result := make([]string, 0, len(fields))
	for _, token := range fields {
		if strings.Contains(token, "-") {
			bounds := strings.Split(token, "-")
			if len(bounds) != 2 {
				return nil, fmt.Errorf("invalid cpu range %q", token)
			}
			start, err := strconv.Atoi(bounds[0])
			if err != nil {
				return nil, fmt.Errorf("invalid cpu range start %q: %w", token, err)
			}
			end, err := strconv.Atoi(bounds[1])
			if err != nil {
				return nil, fmt.Errorf("invalid cpu range end %q: %w", token, err)
			}
			if end < start {
				return nil, fmt.Errorf("invalid cpu range %q", token)
			}
			for i := start; i <= end; i++ {
				result = append(result, strconv.Itoa(i))
			}
			continue
		}
		if _, err := strconv.Atoi(token); err != nil {
			return nil, fmt.Errorf("invalid cpu id %q: %w", token, err)
		}
		result = append(result, token)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no cpus parsed")
	}
	return result, nil
}

func getCPUFrequencyUsageID(policy, cpu string) string {
	return policy + cpuFreqUsageIDSeparator + cpu
}
