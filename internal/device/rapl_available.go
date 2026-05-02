// SPDX-FileCopyrightText: 2025 The Kepler Authors
// SPDX-License-Identifier: Apache-2.0

package device

import (
	"github.com/prometheus/procfs/sysfs"
)

// RaplPowercapPresent reports whether sysfs exposes at least one RAPL zone
// under the powercap interface (e.g. intel-rapl).
func RaplPowercapPresent(sysfsPath string) bool {
	fs, err := sysfs.NewFS(sysfsPath)
	if err != nil {
		return false
	}
	zones, err := sysfs.GetRaplZones(fs)
	return err == nil && len(zones) > 0
}
