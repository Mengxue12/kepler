// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestParseAggregateCPULine(t *testing.T) {
	j, err := parseAggregateCPULine("cpu  1 2 3 4 5 6 7 8 9 10")
	if err != nil {
		t.Fatal(err)
	}
	if j.Times["user"] != 1 || j.Times["nice"] != 2 || j.Times["system"] != 3 || j.Times["idle"] != 4 {
		t.Fatalf("unexpected jiffies: %+v", j)
	}
	if j.Times["guest_nice"] != 10 {
		t.Fatalf("unexpected guest_nice: %d", j.Times["guest_nice"])
	}
}

func TestSubJiffies(t *testing.T) {
	a := CPUJiffies{Times: map[string]uint64{"user": 100, "system": 50, "iowait": 8}}
	b := CPUJiffies{Times: map[string]uint64{"user": 10, "system": 5, "iowait": 3}}
	deltas := subJiffies(a, b)
	if deltas["user"] != 90 || deltas["system"] != 45 || deltas["iowait"] != 5 {
		t.Fatalf("unexpected deltas=%+v", deltas)
	}
}
