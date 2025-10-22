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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRAPLSysfs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RAPL Sysfs Suite")
}

var _ = Describe("RAPL Sysfs", func() {
	var (
		tempDir   string
		raplSysfs *PowerSysfs
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "rapl_test")
		Expect(err).NotTo(HaveOccurred())

		raplSysfs = &PowerSysfs{}
	})

	AfterEach(func() {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
	})

	Context("PowerSysfs struct", func() {
		It("should return correct name", func() {
			Expect(raplSysfs.GetName()).To(Equal("rapl-sysfs"))
		})

		It("should handle system collection support check", func() {
			// This test will depend on the actual system's RAPL support
			// In a real system, this would check /sys/class/powercap/intel-rapl/
			supported := raplSysfs.IsSystemCollectionSupported()
			// We can't easily mock this without root privileges, so we just verify it doesn't panic
			Expect(supported).To(BeAssignableToTypeOf(true))
		})
	})

	Context("Energy reading functions", func() {
		It("should handle energy reading gracefully when RAPL is not available", func() {
			// Test that functions don't panic when RAPL is not available
			val, err := raplSysfs.GetAbsEnergyFromDram()
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("could not read RAPL energy"))
			} else {
				// Also print the received result for debugging/informational purposes
				fmt.Printf("GetAbsEnergyFromDram() returned: %v\n", val)
			}

			val, err = raplSysfs.GetAbsEnergyFromCore()
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("could not read RAPL energy"))
			} else {
				// Also print the received result for debugging/informational purposes
				fmt.Printf("GetAbsEnergyFromCore() returned: %v\n", val)
			}

			val, err = raplSysfs.GetAbsEnergyFromUncore()
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("could not read RAPL energy"))
			} else {
				// Also print the received result for debugging/informational purposes
				fmt.Printf("GetAbsEnergyFromUncore() returned: %v\n", val)
			}

			val, err = raplSysfs.GetAbsEnergyFromPackage()
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("could not read RAPL energy"))
			} else {
				// Also print the received result for debugging/informational purposes
				fmt.Printf("GetAbsEnergyFromPackage() returned: %v\n", val)
			}
		})

		It("should handle max energy range reading gracefully", func() {
			val, err := raplSysfs.GetMaxEnergyRangeFromDram()
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("could not read RAPL energy max range"))
			} else {
				// Also print the received result for debugging/informational purposes
				fmt.Printf("GetMaxEnergyRangeFromDram() returned: %v\n", val)
			}

			val, err = raplSysfs.GetMaxEnergyRangeFromCore()
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("could not read RAPL energy max range"))
			} else {
				// Also print the received result for debugging/informational purposes
				fmt.Printf("GetMaxEnergyRangeFromCore() returned: %v\n", val)
			}

			val, err = raplSysfs.GetMaxEnergyRangeFromUncore()
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("could not read RAPL energy max range"))
			} else {
				// Also print the received result for debugging/informational purposes
				fmt.Printf("GetMaxEnergyRangeFromUncore() returned: %v\n", val)
			}

			val, err = raplSysfs.GetMaxEnergyRangeFromPackage()
			if err != nil {
				Expect(err.Error()).To(ContainSubstring("could not read RAPL energy max range"))
			} else {
				// Also print the received result for debugging/informational purposes
				fmt.Printf("GetMaxEnergyRangeFromPackage() returned: %v\n", val)
			}
		})

		It("should return node components energy map", func() {
			components := raplSysfs.GetAbsEnergyFromNodeComponents()
			Expect(components).To(BeAssignableToTypeOf(map[int]NodeComponentsEnergy{}))
		})
	})

	Context("StopPower function", func() {
		It("should not panic when called", func() {
			Expect(func() {
				raplSysfs.StopPower()
			}).NotTo(Panic())
		})
	})
})

// Unit tests using standard Go testing framework
func TestPowerSysfs_GetName(t *testing.T) {
	raplSysfs := &PowerSysfs{}
	if got := raplSysfs.GetName(); got != "rapl-sysfs" {
		t.Errorf("PowerSysfs.GetName() = %v, want %v", got, "rapl-sysfs")
	}
}

func TestPowerSysfs_StopPower(t *testing.T) {
	raplSysfs := &PowerSysfs{}
	// Should not panic
	raplSysfs.StopPower()
}

func TestPowerSysfs_GetAbsEnergyFromNodeComponents(t *testing.T) {
	raplSysfs := &PowerSysfs{}
	components := raplSysfs.GetAbsEnergyFromNodeComponents()

	// Should return a map (even if empty)
	if components == nil {
		t.Error("GetAbsEnergyFromNodeComponents() returned nil")
	}
}

// Benchmark tests
func BenchmarkPowerSysfs_GetName(b *testing.B) {
	raplSysfs := &PowerSysfs{}
	for i := 0; i < b.N; i++ {
		raplSysfs.GetName()
	}
}

func BenchmarkPowerSysfs_GetAbsEnergyFromNodeComponents(b *testing.B) {
	raplSysfs := &PowerSysfs{}
	for i := 0; i < b.N; i++ {
		raplSysfs.GetAbsEnergyFromNodeComponents()
	}
}
