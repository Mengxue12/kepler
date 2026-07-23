// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

const (
	cpufreqPolicyGlob = "devices/system/cpu/cpufreq/policy*"
	cpufreqCPUGlob    = "devices/system/cpu/cpu[0-9]*/cpufreq"
)

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
// /sys/devices/system/cpu/cpufreq/policy*/. If no policies exist, it falls
// back to per-CPU paths under /sys/devices/system/cpu/cpu*/cpufreq/.
func ReadCpufreqPolicies(sysfsPath string) ([]CpufreqPolicy, error) {
	policies, err := readCpufreqPolicyPaths(sysfsPath, cpufreqPolicyGlob, readCpufreqPolicy)
	if err != nil {
		return nil, err
	}
	if len(policies) > 0 {
		return policies, nil
	}

	perCPU, err := readCpufreqPolicyPaths(sysfsPath, cpufreqCPUGlob, readPerCPUcpufreq)
	if err != nil {
		return nil, err
	}
	perCPU = dedupeCpufreqPolicies(perCPU)
	if len(perCPU) == 0 {
		return nil, fmt.Errorf("no cpufreq policies found under %s", sysfsPath)
	}
	return perCPU, nil
}

func readCpufreqPolicyPaths(
	sysfsPath, globPattern string,
	readFn func(string) (CpufreqPolicy, error),
) ([]CpufreqPolicy, error) {
	policyPaths, err := filepath.Glob(filepath.Join(sysfsPath, globPattern))
	if err != nil {
		return nil, err
	}
	if len(policyPaths) == 0 {
		return nil, nil
	}

	policies := make([]CpufreqPolicy, 0, len(policyPaths))
	for _, policyPath := range policyPaths {
		policy, err := readFn(policyPath)
		if err != nil {
			// Some platforms intermittently return EBUSY for scaling_cur_freq.
			// Skip that policy so other readable CPUs can still be exported.
			if isBusyError(err) {
				continue
			}
			return nil, fmt.Errorf("read cpufreq policy %s: %w", policyPath, err)
		}
		policies = append(policies, policy)
	}

	sort.Slice(policies, func(i, j int) bool {
		return policies[i].Index < policies[j].Index
	})

	return policies, nil
}

func isBusyError(err error) bool {
	return errors.Is(err, syscall.EBUSY)
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

func readPerCPUcpufreq(cpuCpufreqPath string) (CpufreqPolicy, error) {
	cpuName := filepath.Base(filepath.Dir(cpuCpufreqPath))
	index, err := strconv.Atoi(strings.TrimPrefix(cpuName, "cpu"))
	if err != nil {
		return CpufreqPolicy{}, fmt.Errorf("invalid cpu name %q: %w", cpuName, err)
	}

	affectedCPUs := []int{index}
	if affectedRaw, err := os.ReadFile(filepath.Join(cpuCpufreqPath, "affected_cpus")); err == nil {
		if parsed, parseErr := parseCPUList(strings.TrimSpace(string(affectedRaw))); parseErr == nil && len(parsed) > 0 {
			affectedCPUs = parsed
		}
	}

	freqRaw, err := os.ReadFile(filepath.Join(cpuCpufreqPath, "scaling_cur_freq"))
	if err != nil {
		return CpufreqPolicy{}, err
	}

	curFreqKHz, err := strconv.ParseUint(strings.TrimSpace(string(freqRaw)), 10, 64)
	if err != nil {
		return CpufreqPolicy{}, fmt.Errorf("parse scaling_cur_freq: %w", err)
	}

	return CpufreqPolicy{
		Name:         cpuName,
		Index:        index,
		AffectedCPUs: affectedCPUs,
		CurFreqKHz:   curFreqKHz,
	}, nil
}

func dedupeCpufreqPolicies(policies []CpufreqPolicy) []CpufreqPolicy {
	if len(policies) <= 1 {
		return policies
	}

	seen := make(map[string]struct{}, len(policies))
	unique := make([]CpufreqPolicy, 0, len(policies))
	for _, policy := range policies {
		key := cpuListKey(policy.AffectedCPUs)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, policy)
	}
	return unique
}

func cpuListKey(cpus []int) string {
	if len(cpus) == 0 {
		return ""
	}
	sorted := append([]int(nil), cpus...)
	sort.Ints(sorted)
	parts := make([]string, len(sorted))
	for i, cpu := range sorted {
		parts[i] = strconv.Itoa(cpu)
	}
	return strings.Join(parts, ",")
}

func parseCPUList(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	// Kernel exports space- or comma-separated lists (e.g. "0 1 2 3" on RPi, "0-3" on x86).
	parts := strings.FieldsFunc(raw, func(c rune) bool {
		return c == ' ' || c == ',' || c == '\t'
	})

	var cpus []int
	for _, part := range parts {
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
