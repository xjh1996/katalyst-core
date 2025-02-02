/*
Copyright 2022 The Katalyst Authors.

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

package malachite

// for those metrics need extra calculation logic,
// we will put them in a separate file here
import (
	"time"

	"github.com/kubewharf/katalyst-core/pkg/consts"
	"github.com/kubewharf/katalyst-core/pkg/metaserver/agent/metric/malachite/types"
	"github.com/kubewharf/katalyst-core/pkg/util/metric"
)

// processContainerMemBandwidth handles memory bandwidth (read/write) rate in a period while,
// and it will need the previously collected data to do this
func (m *MalachiteMetricsFetcher) processContainerMemBandwidth(podUID, containerName string, cgStats *types.MalachiteCgroupInfo, lastUpdateTimeInSec float64) {
	var (
		lastOCRReadDRAMsMetric, _ = m.metricStore.GetContainerMetric(podUID, containerName, consts.MetricOCRReadDRAMsContainer)
		lastIMCWritesMetric, _    = m.metricStore.GetContainerMetric(podUID, containerName, consts.MetricIMCWriteContainer)
		lastStoreAllInsMetric, _  = m.metricStore.GetContainerMetric(podUID, containerName, consts.MetricStoreAllInsContainer)
		lastStoreInsMetric, _     = m.metricStore.GetContainerMetric(podUID, containerName, consts.MetricStoreInsContainer)

		// those value are uint64 type from source
		lastOCRReadDRAMs = uint64(lastOCRReadDRAMsMetric.Value)
		lastIMCWrites    = uint64(lastIMCWritesMetric.Value)
		lastStoreAllIns  = uint64(lastStoreAllInsMetric.Value)
		lastStoreIns     = uint64(lastStoreInsMetric.Value)
	)

	var (
		curOCRReadDRAMs, curIMCWrites, curStoreAllIns, curStoreIns uint64
		curUpdateTimeInSec                                         float64
	)

	if cgStats.CgroupType == "V1" {
		curOCRReadDRAMs = cgStats.V1.Cpu.OCRReadDRAMs
		curIMCWrites = cgStats.V1.Cpu.IMCWrites
		curStoreAllIns = cgStats.V1.Cpu.StoreAllInstructions
		curStoreIns = cgStats.V1.Cpu.StoreInstructions
		curUpdateTimeInSec = float64(cgStats.V1.Cpu.UpdateTime)
	} else if cgStats.CgroupType == "V2" {
		curOCRReadDRAMs = cgStats.V2.Cpu.OCRReadDRAMs
		curIMCWrites = cgStats.V2.Cpu.IMCWrites
		curStoreAllIns = cgStats.V2.Cpu.StoreAllInstructions
		curStoreIns = cgStats.V2.Cpu.StoreInstructions
		curUpdateTimeInSec = float64(cgStats.V2.Cpu.UpdateTime)
	}

	// read bandwidth
	m.setContainerRateMetric(podUID, containerName, consts.MetricMemBandwidthReadContainer,
		func() float64 {
			// read megabyte
			return float64(uint64CounterDelta(lastOCRReadDRAMs, curOCRReadDRAMs)) * 64 / (1024 * 1024)
		},
		int64(lastUpdateTimeInSec), int64(curUpdateTimeInSec))

	// write bandwidth
	m.setContainerRateMetric(podUID, containerName, consts.MetricMemBandwidthWriteContainer,
		func() float64 {
			storeAllInsInc := uint64CounterDelta(lastStoreAllIns, curStoreAllIns)
			if storeAllInsInc == 0 {
				return 0
			}

			storeInsInc := uint64CounterDelta(lastStoreIns, curStoreIns)
			imcWritesInc := uint64CounterDelta(lastIMCWrites, curIMCWrites)

			// write megabyte
			return float64(storeInsInc) / float64(storeAllInsInc) / (1024 * 1024) * float64(imcWritesInc) * 64
		},
		int64(lastUpdateTimeInSec), int64(curUpdateTimeInSec))
}

// setContainerRateMetric is used to set rate metric in container level.
// This method will check if the metric is really updated, and decide weather to update metric in metricStore.
// The method could help avoid lots of meaningless "zero" value.
func (m *MalachiteMetricsFetcher) setContainerRateMetric(podUID, containerName, targetMetricName string, deltaValueFunc func() float64, lastUpdateTime, curUpdateTime int64) {
	timeDeltaInSec := curUpdateTime - lastUpdateTime
	if lastUpdateTime == 0 || timeDeltaInSec <= 0 {
		// Return directly when the following situations happen:
		// 1. lastUpdateTime == 0, which means no previous data.
		// 2. timeDeltaInSec == 0, which means the metric is not updated,
		//	this is originated from the sampling lag between katalyst-core and malachite(data source)
		// 3. timeDeltaInSec < 0, this is illegal and unlikely to happen.
		return
	}

	// TODO this will duplicate "updateTime" a lot.
	// But to my knowledge, the cost could be acceptable.
	updateTime := time.Unix(curUpdateTime, 0)
	m.metricStore.SetContainerMetric(podUID, containerName, targetMetricName,
		metric.MetricData{Value: deltaValueFunc() / float64(timeDeltaInSec), Time: &updateTime})
}

// uint64CounterDelta calculate the delta between two uint64 counters
// Sometimes the counter value would go beyond the MaxUint64. In that case,
// negative counter delta would happen, and the data is not incorrect.
func uint64CounterDelta(previous, current uint64) uint64 {
	if current >= previous {
		return current - previous
	}

	// Return 0 when previous > current, because we may not be able to make sure
	// the upper bound for each counter.
	return 0
}
