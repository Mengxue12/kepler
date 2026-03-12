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

package network

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/sustainable-computing-io/kepler/pkg/collector/stats"
	"github.com/sustainable-computing-io/kepler/pkg/config"
	"github.com/sustainable-computing-io/kepler/pkg/utils"
	"k8s.io/klog/v2"
)

var (
	mx sync.Mutex

	// Previous cumulative /proc/<pid>/net/dev values by container ID.
	prevContainerRxBytes = map[string]uint64{}
	prevContainerTxBytes = map[string]uint64{}

	// Previous cumulative /proc/net/dev values.
	prevNodeRxBytes uint64
	prevNodeTxBytes uint64
)

func UpdateContainerNetworkMetrics(containerStats map[string]*stats.ContainerStats) {
	mx.Lock()
	defer mx.Unlock()

	for containerID, cStat := range containerStats {
		pids, pidNetNSMap, ok := getOnePIDPerNetNS(cStat)
		if !ok {
			continue
		}
		klog.V(6).Infof("pod/container %s/%s %s selected pids[network namespace]: %v", cStat.PodName, cStat.ContainerName, containerID, pidNetNSMap)

		var rxBytes, txBytes uint64
		reads := 0
		for _, pid := range pids {
			pidRxBytes, pidTxBytes, err := getNetDevBytes(fmt.Sprintf("/proc/%d/net/dev", pid), true)
			if err != nil {
				klog.V(6).Infof("failed to read net stats for container %s pid %d: %v", containerID, pid, err)
				continue
			}
			rxBytes += pidRxBytes
			txBytes += pidTxBytes
			reads++
		}
		if reads == 0 {
			continue
		}

		prevRx := prevContainerRxBytes[containerID]
		prevTx := prevContainerTxBytes[containerID]
		prevContainerRxBytes[containerID] = rxBytes
		prevContainerTxBytes[containerID] = txBytes

		if rxBytes >= prevRx {
			cStat.ResourceUsage[config.NetRX].AddDeltaStat(utils.GenericSocketID, rxBytes-prevRx)
		}
		if txBytes >= prevTx {
			cStat.ResourceUsage[config.NetTX].AddDeltaStat(utils.GenericSocketID, txBytes-prevTx)
		}
	}
}

func UpdateNodeNetworkMetrics(nodeStats *stats.NodeStats) {
	mx.Lock()
	defer mx.Unlock()

	rxBytes, txBytes, err := getNetDevBytes("/proc/net/dev", true)
	if err != nil {
		klog.V(3).Infof("failed to read /proc/net/dev: %v", err)
		return
	}

	if rxBytes >= prevNodeRxBytes {
		nodeStats.ResourceUsage[config.NetRX].AddDeltaStat(utils.GenericSocketID, rxBytes-prevNodeRxBytes)
	} else {
		klog.V(6).Infof("node network RX bytes decreased: %d -> %d", prevNodeRxBytes, rxBytes)
	}
	if txBytes >= prevNodeTxBytes {
		nodeStats.ResourceUsage[config.NetTX].AddDeltaStat(utils.GenericSocketID, txBytes-prevNodeTxBytes)
	} else {
		klog.V(6).Infof("node network TX bytes decreased: %d -> %d", prevNodeTxBytes, txBytes)
	}

	prevNodeRxBytes = rxBytes
	prevNodeTxBytes = txBytes
}

func getOnePIDPerNetNS(cStat *stats.ContainerStats) ([]uint64, map[uint64]string, bool) {
	pidByNetNS := map[string]uint64{}
	for pid := range cStat.PIDS {
		netNSID, ok := getNetNSID(pid)
		if !ok {
			continue
		}
		if existingPID, exists := pidByNetNS[netNSID]; !exists || pid < existingPID {
			pidByNetNS[netNSID] = pid
		}
	}
	if len(pidByNetNS) == 0 {
		return nil, nil, false
	}
	pids := make([]uint64, 0, len(pidByNetNS))
	pidNetNSMap := make(map[uint64]string, len(pidByNetNS))
	for _, pid := range pidByNetNS {
		pids = append(pids, pid)
	}
	for netNSID, pid := range pidByNetNS {
		pidNetNSMap[pid] = netNSID
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
	return pids, pidNetNSMap, true
}

func getNetNSID(pid uint64) (string, bool) {
	netNSLink, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/net", pid))
	if err != nil {
		return "", false
	}
	return netNSLink, true
}

func getNetDevBytes(path string, skipLoopback bool) (uint64, uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var rxTotal, txTotal uint64
	lineNum := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lineNum++
		// Skip headers in /proc/net/dev.
		if lineNum <= 2 || line == "" {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		if skipLoopback && iface == "lo" {
			continue
		}

		fields := strings.Fields(parts[1])
		// rx bytes is field 0, tx bytes is field 8.
		if len(fields) < 9 {
			continue
		}

		rx, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		tx, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			continue
		}
		rxTotal += rx
		txTotal += tx
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	return rxTotal, txTotal, nil
}
