// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// CPUJiffies holds jiffies from the aggregate "cpu" line in /proc/stat (see kernel docs).
type CPUJiffies struct {
	Times map[string]uint64
}

var cpuTimeTypes = []string{
	"user",
	"nice",
	"system",
	"idle",
	"iowait",
	"irq",
	"softirq",
	"steal",
	"guest",
	"guest_nice",
}

// ReadCPUStat reads aggregate CPU counters from procRoot/stat (e.g. procRoot="/host/proc").
func ReadCPUStat(procRoot string) (CPUJiffies, error) {
	data, err := os.ReadFile(procRoot + "/stat")
	if err != nil {
		return CPUJiffies{}, err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 {
		return CPUJiffies{}, fmt.Errorf("empty stat file")
	}
	return parseAggregateCPULine(lines[0])
}

func parseAggregateCPULine(line string) (CPUJiffies, error) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return CPUJiffies{}, fmt.Errorf("expected aggregate cpu line, got %q", line)
	}
	j := CPUJiffies{Times: make(map[string]uint64)}
	maxFields := len(cpuTimeTypes)
	if len(fields)-1 < maxFields {
		maxFields = len(fields) - 1
	}
	for i := 0; i < maxFields; i++ {
		v, err := strconv.ParseUint(fields[i+1], 10, 64)
		if err != nil {
			return CPUJiffies{}, fmt.Errorf("%s: %w", cpuTimeTypes[i], err)
		}
		j.Times[cpuTimeTypes[i]] = v
	}
	return j, nil
}

func subJiffies(a, b CPUJiffies) map[string]uint64 {
	delta := make(map[string]uint64, len(a.Times))
	for timeType, cur := range a.Times {
		prev := b.Times[timeType]
		if cur >= prev {
			delta[timeType] = cur - prev
		}
	}
	return delta
}
