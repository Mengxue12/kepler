// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validCpufreqSysFSPath = "testdata/sys"

func TestReadCpufreqPolicies(t *testing.T) {
	policies, err := ReadCpufreqPolicies(validCpufreqSysFSPath)
	require.NoError(t, err)
	require.Len(t, policies, 2)

	assert.Equal(t, "policy0", policies[0].Name)
	assert.Equal(t, 0, policies[0].Index)
	assert.Equal(t, []int{0, 1, 2, 3}, policies[0].AffectedCPUs)
	assert.Equal(t, uint64(2400000), policies[0].CurFreqKHz)

	assert.Equal(t, "policy1", policies[1].Name)
	assert.Equal(t, 1, policies[1].Index)
	assert.Equal(t, []int{4, 5}, policies[1].AffectedCPUs)
	assert.Equal(t, uint64(1800000), policies[1].CurFreqKHz)
}

func TestReadCpufreqPoliciesPerCPUFallback(t *testing.T) {
	sysfsPath := "testdata/sys-percpu"
	policies, err := ReadCpufreqPolicies(sysfsPath)
	require.NoError(t, err)
	require.Len(t, policies, 1, "RPi-style duplicate per-CPU domains should be deduplicated")

	assert.Equal(t, "cpu0", policies[0].Name)
	assert.Equal(t, 0, policies[0].Index)
	assert.Equal(t, []int{0, 1, 2, 3}, policies[0].AffectedCPUs)
	assert.Equal(t, uint64(1500000), policies[0].CurFreqKHz)
}

func TestDedupeCpufreqPolicies(t *testing.T) {
	policies := []CpufreqPolicy{
		{Name: "cpu0", Index: 0, AffectedCPUs: []int{0, 1, 2, 3}, CurFreqKHz: 1500000},
		{Name: "cpu1", Index: 1, AffectedCPUs: []int{0, 1, 2, 3}, CurFreqKHz: 1500000},
		{Name: "cpu4", Index: 4, AffectedCPUs: []int{4, 5}, CurFreqKHz: 1800000},
	}

	deduped := dedupeCpufreqPolicies(policies)
	require.Len(t, deduped, 2)
	assert.Equal(t, "cpu0", deduped[0].Name)
	assert.Equal(t, "cpu4", deduped[1].Name)
}

func TestCpufreqPresent(t *testing.T) {
	assert.True(t, CpufreqPresent(validCpufreqSysFSPath))
	assert.True(t, CpufreqPresent("testdata/sys-percpu"))
	assert.False(t, CpufreqPresent("testdata/bad_sysfs"))
	assert.False(t, CpufreqPresent("/nonexistent-sysfs-cpufreq-test"))
}

func TestParseCPUList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    []int
		wantErr bool
	}{
		{name: "single cpu", raw: "0", want: []int{0}},
		{name: "range", raw: "0-3", want: []int{0, 1, 2, 3}},
		{name: "space separated", raw: "0 1 2 3", want: []int{0, 1, 2, 3}},
		{name: "mixed", raw: "0,2,4-6", want: []int{0, 2, 4, 5, 6}},
		{name: "empty", raw: "", want: nil},
		{name: "invalid range", raw: "3-1", wantErr: true},
		{name: "invalid token", raw: "foo", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseCPUList(tt.raw)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
