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
	"testing"
)

func TestPowerRAPLSysfs_GetName(t *testing.T) {
	rapl := &PowerRAPLSysfs{}
	if got := rapl.GetName(); got != "rapl-sysfs-psys" {
		t.Errorf("PowerRAPLSysfs.GetName() = %v, want %v", got, "rapl-sysfs-psys")
	}
}

func TestPowerRAPLSysfs_StopPower(t *testing.T) {
	rapl := &PowerRAPLSysfs{}
	// Should not panic
	rapl.StopPower()
}

func TestPowerRAPLSysfs_IsSystemCollectionSupported(t *testing.T) {
	rapl := &PowerRAPLSysfs{}
	// This test depends on system hardware
	// We just verify it doesn't panic
	supported := rapl.IsSystemCollectionSupported()
	t.Logf("RAPL psys support: %v", supported)
}

func TestNewPowerRAPLSysfs(t *testing.T) {
	// This test depends on system hardware
	rapl := NewPowerRAPLSysfs()
	if rapl != nil {
		t.Logf("RAPL psys source created successfully")
		if rapl.psysPath == "" {
			t.Error("psysPath should be set if creation succeeded")
		}
	} else {
		t.Logf("RAPL psys not available on this system")
	}
}
