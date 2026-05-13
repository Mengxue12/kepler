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
	User   uint64
	Nice   uint64
	System uint64
	Idle   uint64
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
	parse := func(i int) (uint64, error) {
		return strconv.ParseUint(fields[i], 10, 64)
	}
	var j CPUJiffies
	var err error
	if j.User, err = parse(1); err != nil {
		return CPUJiffies{}, fmt.Errorf("user: %w", err)
	}
	if j.Nice, err = parse(2); err != nil {
		return CPUJiffies{}, fmt.Errorf("nice: %w", err)
	}
	if j.System, err = parse(3); err != nil {
		return CPUJiffies{}, fmt.Errorf("system: %w", err)
	}
	if j.Idle, err = parse(4); err != nil {
		return CPUJiffies{}, fmt.Errorf("idle: %w", err)
	}
	return j, nil
}

func subJiffies(a, b CPUJiffies) (dUser, dSystem uint64) {
	if a.User >= b.User {
		dUser = a.User - b.User
	}
	if a.System >= b.System {
		dSystem = a.System - b.System
	}
	return dUser, dSystem
}
