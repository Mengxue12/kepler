/*
Copyright 2026.

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

package disk

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/sustainable-computing-io/kepler/pkg/collector/stats"
	"github.com/sustainable-computing-io/kepler/pkg/config"
	"github.com/sustainable-computing-io/kepler/pkg/utils"
	"k8s.io/klog/v2"
)

const (
	cgroupRootPath = "/sys/fs/cgroup"
)

var (
	mx sync.Mutex

	// Previous cumulative io.stat values by cgroup path.
	prevContainerReadBytes  = map[string]uint64{}
	prevContainerWriteBytes = map[string]uint64{}

	// Previous cumulative bytes from /proc/diskstats.
	prevNodeReadBytes  uint64
	prevNodeWriteBytes uint64
)

func UpdateContainerDiskIOMetrics(containerStats map[string]*stats.ContainerStats) {
	mx.Lock()
	defer mx.Unlock()

	var totalNonSystemReadDelta uint64
	var totalNonSystemWriteDelta uint64
	seenCgroupPaths := map[string]bool{}

	for _, cStat := range containerStats {
		if cStat.ContainerID == utils.SystemProcessName {
			// Handle system_processes as residual (node - non-system containers) later.
			continue
		}

		cgroupPath := resolveContainerCgroupPath(cStat)
		if cgroupPath == "" {
			continue
		}
		if seenCgroupPaths[cgroupPath] {
			klog.V(6).Infof("skip duplicate cgroup path %s for container %s", cgroupPath, cStat.ContainerID)
			continue
		}
		seenCgroupPaths[cgroupPath] = true

		readBytes, writeBytes, err := parseCgroupIOStat(cgroupPath)
		if err != nil {
			klog.V(6).Infof("failed to parse io.stat for cgroup %s: %v", cgroupPath, err)
			continue
		}

		prevRead := prevContainerReadBytes[cgroupPath]
		prevWrite := prevContainerWriteBytes[cgroupPath]
		prevContainerReadBytes[cgroupPath] = readBytes
		prevContainerWriteBytes[cgroupPath] = writeBytes

		if readBytes >= prevRead {
			readDelta := readBytes - prevRead
			cStat.ResourceUsage[config.DiskRead].AddDeltaStat(utils.GenericSocketID, readDelta)
			totalNonSystemReadDelta += readDelta
		}
		if writeBytes >= prevWrite {
			writeDelta := writeBytes - prevWrite
			cStat.ResourceUsage[config.DiskWrite].AddDeltaStat(utils.GenericSocketID, writeDelta)
			totalNonSystemWriteDelta += writeDelta
		}
	}

	// Derive system_processes as residual to avoid overlap with container cgroup trees.
	systemStat, ok := containerStats[utils.SystemProcessName]
	if !ok {
		return
	}

	nodeReadBytes, nodeWriteBytes, err := getNodeDiskBytes()
	if err != nil {
		klog.V(6).Infof("failed to derive system process disk io from node stats: %v", err)
		return
	}
	var nodeReadDelta uint64
	var nodeWriteDelta uint64
	if nodeReadBytes >= prevNodeReadBytes {
		nodeReadDelta = nodeReadBytes - prevNodeReadBytes
	}
	if nodeWriteBytes >= prevNodeWriteBytes {
		nodeWriteDelta = nodeWriteBytes - prevNodeWriteBytes
	}

	if nodeReadDelta > totalNonSystemReadDelta {
		systemStat.ResourceUsage[config.DiskRead].AddDeltaStat(utils.GenericSocketID, nodeReadDelta-totalNonSystemReadDelta)
	}
	if nodeWriteDelta > totalNonSystemWriteDelta {
		systemStat.ResourceUsage[config.DiskWrite].AddDeltaStat(utils.GenericSocketID, nodeWriteDelta-totalNonSystemWriteDelta)
	}
	if nodeReadDelta < totalNonSystemReadDelta || nodeWriteDelta < totalNonSystemWriteDelta {
		klog.V(6).Infof("non-system container disk delta exceeds node delta (node read=%d write=%d, non-system read=%d write=%d)",
			nodeReadDelta, nodeWriteDelta, totalNonSystemReadDelta, totalNonSystemWriteDelta)
	}
}

func UpdateNodeDiskIOMetrics(nodeStats *stats.NodeStats) {
	mx.Lock()
	defer mx.Unlock()

	readBytes, writeBytes, err := getNodeDiskBytes()
	if err != nil {
		klog.V(3).Infof("failed to parse /proc/diskstats: %v", err)
		return
	}

	if readBytes >= prevNodeReadBytes {
		nodeStats.ResourceUsage[config.DiskRead].AddDeltaStat(utils.GenericSocketID, readBytes-prevNodeReadBytes)
	}
	if writeBytes >= prevNodeWriteBytes {
		nodeStats.ResourceUsage[config.DiskWrite].AddDeltaStat(utils.GenericSocketID, writeBytes-prevNodeWriteBytes)
	}

	prevNodeReadBytes = readBytes
	prevNodeWriteBytes = writeBytes
}

func resolveContainerCgroupPath(cStat *stats.ContainerStats) string {
	for pid := range cStat.PIDS {
		cgroupPath, err := getCgroupPathFromPID(pid)
		if err == nil && cgroupPath != "" {
			return cgroupPath
		}
	}
	return ""
}

func getCgroupPathFromPID(pid uint64) (string, error) {
	path := fmt.Sprintf("/proc/%d/cgroup", pid)
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// cgroup v2 format: 0::/kubepods.slice/...
		if strings.HasPrefix(line, "0::") {
			relative := strings.TrimPrefix(line, "0::")
			if relative == "" {
				relative = "/"
			}
			return cgroupRootPath + relative, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("cgroup v2 path not found for pid %d", pid)
}

func parseCgroupIOStat(cgroupPath string) (uint64, uint64, error) {
	data, err := os.ReadFile(cgroupPath + "/io.stat")
	if err != nil {
		return 0, 0, err
	}
	var readBytes, writeBytes uint64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		// line format: "<major>:<minor> rbytes=... wbytes=..."
		for _, field := range fields {
			if strings.HasPrefix(field, "rbytes=") {
				v, err := strconv.ParseUint(strings.TrimPrefix(field, "rbytes="), 10, 64)
				if err != nil {
					return 0, 0, err
				}
				readBytes += v
			}
			if strings.HasPrefix(field, "wbytes=") {
				v, err := strconv.ParseUint(strings.TrimPrefix(field, "wbytes="), 10, 64)
				if err != nil {
					return 0, 0, err
				}
				writeBytes += v
			}
		}
	}
	return readBytes, writeBytes, nil
}

func getNodeDiskBytes() (uint64, uint64, error) {
	data, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return 0, 0, err
	}
	var readSectors, writeSectors uint64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		// Format:
		// major minor name reads ... sectors_read ... writes ... sectors_written ...
		if len(fields) < 10 {
			continue
		}
		name := fields[2]
		// Avoid obvious pseudo devices and reduce double-counting from loop/ram devices.
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			continue
		}
		read, err := strconv.ParseUint(fields[5], 10, 64)
		if err != nil {
			continue
		}
		write, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			continue
		}
		readSectors += read
		writeSectors += write
	}

	// Linux sectors in /proc/diskstats are 512-byte units.
	return readSectors * 512, writeSectors * 512, nil
}
