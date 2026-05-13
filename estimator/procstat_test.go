// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestParseAggregateCPULine(t *testing.T) {
	j, err := parseAggregateCPULine("cpu  1 2 3 4 5 6 7 8 9 10")
	if err != nil {
		t.Fatal(err)
	}
	if j.User != 1 || j.Nice != 2 || j.System != 3 || j.Idle != 4 {
		t.Fatalf("unexpected jiffies: %+v", j)
	}
}

func TestSubJiffies(t *testing.T) {
	a := CPUJiffies{User: 100, System: 50}
	b := CPUJiffies{User: 10, System: 5}
	dU, dS := subJiffies(a, b)
	if dU != 90 || dS != 45 {
		t.Fatalf("dUser=%d dSystem=%d", dU, dS)
	}
}
