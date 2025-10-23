/*
Copyright 2024.

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

package source

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"k8s.io/klog/v2"
)

const (
	// sysfs path templates for RAPL
	raplPackageNamePathTemplate = "/sys/class/powercap/intel-rapl/intel-rapl:%d/"
	raplEnergyFile              = "energy_uj"
	raplNameFile                = "name"
	psysEvent                   = "psys"
)

// PowerRAPLSysfs implements platform power collection using RAPL psys
type PowerRAPLSysfs struct {
	psysPath string
}

func NewPowerRAPLSysfs() *PowerRAPLSysfs {
	rapl := &PowerRAPLSysfs{}
	if rapl.IsSystemCollectionSupported() {
		klog.V(5).Infof("RAPL psys is available for platform power collection")
		return rapl
	}
	return nil
}

func (r *PowerRAPLSysfs) GetName() string {
	return "rapl-sysfs-psys"
}

func (r *PowerRAPLSysfs) IsSystemCollectionSupported() bool {
	// Try to find psys in RAPL zones
	// psys can be at different indices, so we need to search for it
	maxPackages := 10 // reasonable upper limit
	for i := 0; i < maxPackages; i++ {
		packagePath := fmt.Sprintf(raplPackageNamePathTemplate, i)
		data, err := os.ReadFile(packagePath + raplNameFile)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(string(data))
		if name == psysEvent {
			// Found psys, check if we can read energy
			if _, err := os.ReadFile(packagePath + raplEnergyFile); err == nil {
				r.psysPath = packagePath
				klog.V(5).Infof("Found RAPL psys at: %s", packagePath)
				return true
			}
		}
	}
	return false
}

func (r *PowerRAPLSysfs) GetAbsEnergyFromPlatform() (map[string]float64, error) {
	if r.psysPath == "" {
		return nil, fmt.Errorf("psys path not initialized")
	}

	data, err := os.ReadFile(r.psysPath + raplEnergyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read psys energy: %w", err)
	}

	energyUJ, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse psys energy: %w", err)
	}

	// Convert from microjoules to millijoules
	energyMJ := float64(energyUJ) / 1000.0

	klog.V(6).Infof("RAPL psys energy: %f mJ", energyMJ)

	return map[string]float64{
		"platform": energyMJ,
	}, nil
}

func (r *PowerRAPLSysfs) StopPower() {
	// No cleanup needed
}
