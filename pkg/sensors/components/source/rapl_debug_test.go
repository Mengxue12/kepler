/*
Copyright 2021.

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
	"testing"
)

// TestRAPLDebug helps diagnose RAPL issues
func TestRAPLDebug(t *testing.T) {
	fmt.Println("\n=== RAPL Debug Information ===")

	// 1. Check numPackages
	fmt.Printf("numPackages from ghw: %d\n", numPackages)

	// 2. Check eventPaths
	fmt.Printf("eventPaths content: %+v\n", eventPaths)
	fmt.Printf("eventPaths length: %d\n", len(eventPaths))

	// 3. Check if RAPL sysfs exists
	raplPath := "/sys/class/powercap/intel-rapl/"
	if _, err := os.Stat(raplPath); err != nil {
		fmt.Printf("RAPL sysfs not found: %v\n", err)
	} else {
		fmt.Printf("RAPL sysfs exists: %s\n", raplPath)
	}

	// 4. Try to read package-0
	for i := 0; i < 3; i++ {
		packagePath := fmt.Sprintf("/sys/class/powercap/intel-rapl/intel-rapl:%d/", i)
		namePath := packagePath + "name"
		energyPath := packagePath + "energy_uj"

		if nameData, err := os.ReadFile(namePath); err == nil {
			fmt.Printf("Package %d name: %s\n", i, string(nameData))

			// Try to read energy
			if energyData, err := os.ReadFile(energyPath); err == nil {
				fmt.Printf("Package %d energy: %s µJ\n", i, string(energyData))
			} else {
				fmt.Printf("Package %d energy read error: %v\n", i, err)
			}
		} else {
			fmt.Printf("Package %d not found: %v\n", i, err)
		}
	}

	// 5. Test PowerSysfs
	raplSysfs := &PowerSysfs{}
	fmt.Printf("\nPowerSysfs name: %s\n", raplSysfs.GetName())
	fmt.Printf("IsSystemCollectionSupported: %v\n", raplSysfs.IsSystemCollectionSupported())

	// 6. Try to read energies
	dramEnergy, dramErr := raplSysfs.GetAbsEnergyFromDram()
	fmt.Printf("GetAbsEnergyFromDram() returned: %d, error: %v\n", dramEnergy, dramErr)

	coreEnergy, coreErr := raplSysfs.GetAbsEnergyFromCore()
	fmt.Printf("GetAbsEnergyFromCore() returned: %d, error: %v\n", coreEnergy, coreErr)

	uncoreEnergy, uncoreErr := raplSysfs.GetAbsEnergyFromUncore()
	fmt.Printf("GetAbsEnergyFromUncore() returned: %d, error: %v\n", uncoreEnergy, uncoreErr)

	pkgEnergy, pkgErr := raplSysfs.GetAbsEnergyFromPackage()
	fmt.Printf("GetAbsEnergyFromPackage() returned: %d, error: %v\n", pkgEnergy, pkgErr)

	// 7. Check hasEvent for each event type
	fmt.Printf("\nhasEvent('dram'): %v\n", hasEvent("dram"))
	fmt.Printf("hasEvent('core'): %v\n", hasEvent("core"))
	fmt.Printf("hasEvent('uncore'): %v\n", hasEvent("uncore"))
	fmt.Printf("hasEvent('package'): %v\n", hasEvent("package"))

	// 8. Test max energy range reading
	fmt.Println("\n--- Max Energy Range Tests ---")
	dramMax, dramMaxErr := raplSysfs.GetMaxEnergyRangeFromDram()
	fmt.Printf("GetMaxEnergyRangeFromDram() returned: %d, error: %v\n", dramMax, dramMaxErr)

	coreMax, coreMaxErr := raplSysfs.GetMaxEnergyRangeFromCore()
	fmt.Printf("GetMaxEnergyRangeFromCore() returned: %d, error: %v\n", coreMax, coreMaxErr)

	uncoreMax, uncoreMaxErr := raplSysfs.GetMaxEnergyRangeFromUncore()
	fmt.Printf("GetMaxEnergyRangeFromUncore() returned: %d, error: %v\n", uncoreMax, uncoreMaxErr)

	pkgMax, pkgMaxErr := raplSysfs.GetMaxEnergyRangeFromPackage()
	fmt.Printf("GetMaxEnergyRangeFromPackage() returned: %d, error: %v\n", pkgMax, pkgMaxErr)

	// 9. Check file permissions
	fmt.Println("\n--- File Permission Analysis ---")
	energyPath := "/sys/class/powercap/intel-rapl/intel-rapl:0/energy_uj"
	maxEnergyPath := "/sys/class/powercap/intel-rapl/intel-rapl:0/max_energy_range_uj"

	if info, err := os.Stat(energyPath); err == nil {
		fmt.Printf("energy_uj permissions: %v\n", info.Mode())
	} else {
		fmt.Printf("energy_uj stat error: %v\n", err)
	}

	if info, err := os.Stat(maxEnergyPath); err == nil {
		fmt.Printf("max_energy_range_uj permissions: %v\n", info.Mode())
	} else {
		fmt.Printf("max_energy_range_uj stat error: %v\n", err)
	}

	fmt.Println("\n=== End of Debug Information ===")
}
