// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const cpufreqPolicyGlob = "devices/system/cpu/cpufreq/policy*"

// CpufreqPolicy holds current frequency data for a cpufreq policy.
type CpufreqPolicy struct {
	Name         string
	Index        int
	AffectedCPUs []int
	CurFreqKHz   uint64
}

// CpufreqReader reads CPU frequency data from sysfs cpufreq policies.
type CpufreqReader interface {
	Policies() ([]CpufreqPolicy, error)
}

type sysfsCpufreqReader struct {
	sysfsPath string
}

// NewCpufreqReader creates a reader for cpufreq policy data under sysfsPath.
func NewCpufreqReader(sysfsPath string) CpufreqReader {
	return &sysfsCpufreqReader{sysfsPath: sysfsPath}
}

func (r *sysfsCpufreqReader) Policies() ([]CpufreqPolicy, error) {
	return ReadCpufreqPolicies(r.sysfsPath)
}

// CpufreqPresent reports whether sysfs exposes at least one cpufreq policy.
func CpufreqPresent(sysfsPath string) bool {
	policies, err := ReadCpufreqPolicies(sysfsPath)
	return err == nil && len(policies) > 0
}

// ReadCpufreqPolicies reads scaling_cur_freq and affected_cpus from
// /sys/devices/system/cpu/cpufreq/policy*/.
func ReadCpufreqPolicies(sysfsPath string) ([]CpufreqPolicy, error) {
	policyPaths, err := filepath.Glob(filepath.Join(sysfsPath, cpufreqPolicyGlob))
	if err != nil {
		return nil, err
	}
	if len(policyPaths) == 0 {
		return nil, fmt.Errorf("no cpufreq policies found under %s", sysfsPath)
	}

	policies := make([]CpufreqPolicy, 0, len(policyPaths))
	for _, policyPath := range policyPaths {
		policy, err := readCpufreqPolicy(policyPath)
		if err != nil {
			return nil, fmt.Errorf("read cpufreq policy %s: %w", policyPath, err)
		}
		policies = append(policies, policy)
	}

	sort.Slice(policies, func(i, j int) bool {
		return policies[i].Index < policies[j].Index
	})

	return policies, nil
}

func readCpufreqPolicy(policyPath string) (CpufreqPolicy, error) {
	name := filepath.Base(policyPath)
	index, err := strconv.Atoi(strings.TrimPrefix(name, "policy"))
	if err != nil {
		return CpufreqPolicy{}, fmt.Errorf("invalid policy name %q: %w", name, err)
	}

	affectedRaw, err := os.ReadFile(filepath.Join(policyPath, "affected_cpus"))
	if err != nil {
		return CpufreqPolicy{}, err
	}

	affectedCPUs, err := parseCPUList(strings.TrimSpace(string(affectedRaw)))
	if err != nil {
		return CpufreqPolicy{}, fmt.Errorf("parse affected_cpus: %w", err)
	}

	freqRaw, err := os.ReadFile(filepath.Join(policyPath, "scaling_cur_freq"))
	if err != nil {
		return CpufreqPolicy{}, err
	}

	curFreqKHz, err := strconv.ParseUint(strings.TrimSpace(string(freqRaw)), 10, 64)
	if err != nil {
		return CpufreqPolicy{}, fmt.Errorf("parse scaling_cur_freq: %w", err)
	}

	return CpufreqPolicy{
		Name:         name,
		Index:        index,
		AffectedCPUs: affectedCPUs,
		CurFreqKHz:   curFreqKHz,
	}, nil
}

func parseCPUList(raw string) ([]int, error) {
	if raw == "" {
		return nil, nil
	}

	var cpus []int
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			if len(bounds) != 2 {
				return nil, fmt.Errorf("invalid cpu range %q", part)
			}

			start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return nil, fmt.Errorf("invalid cpu range start in %q: %w", part, err)
			}
			end, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return nil, fmt.Errorf("invalid cpu range end in %q: %w", part, err)
			}
			if end < start {
				return nil, fmt.Errorf("invalid cpu range %q: end before start", part)
			}

			for cpu := start; cpu <= end; cpu++ {
				cpus = append(cpus, cpu)
			}
			continue
		}

		cpu, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid cpu id %q: %w", part, err)
		}
		cpus = append(cpus, cpu)
	}

	return cpus, nil
}
