package metric

import (
	"context"
	"log"
	"openwrt-diskio-api/backend/model"
	"sync/atomic"
	"time"
)

type DynamicMetricService struct {
	UpdateInterval      uint
	activeChan          chan struct{}
	lastRequestTimeUnix int64
	reader              FsReaderInterface
	dynamicMetric       *model.DynamicMetric
}

func NewDynamicMetricService(reader FsReaderInterface, updateInterval uint) *DynamicMetricService {
	return &DynamicMetricService{
		UpdateInterval:      updateInterval,
		activeChan:          make(chan struct{}, 1),
		lastRequestTimeUnix: time.Now().UnixNano(),
		reader:              reader,
		dynamicMetric:       &model.DynamicMetric{},
	}
}

func (dms *DynamicMetricService) ActiveSignal() {
	atomic.StoreInt64(&dms.lastRequestTimeUnix, time.Now().UnixNano())
	select {
	case dms.activeChan <- struct{}{}:
	default:
	}
}

func (dms *DynamicMetricService) Run(ctx context.Context) {
	diskSnap := model.DiskSnap{}
	cpuSnap := model.CpuSnap{}
	netSnap := model.NetSnap{
		Interfaces: map[string]model.NetSnapUnit{},
	}
	updateIntervalSecond := dms.UpdateInterval
	tickDuration := time.Duration(updateIntervalSecond) * time.Second
	reader := dms.reader
	isRunning := true
	ticker := time.NewTicker(tickDuration)
	defer ticker.Stop()

	prevTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case <-dms.activeChan:
			if !isRunning {
				log.Println("Receive dynamic system metric service active signal")
				isRunning = true
				prevTime = time.Now()
			}
		case <-ticker.C:
			if !isRunning {
				continue
			}

			lastUnix := atomic.LoadInt64(&dms.lastRequestTimeUnix)
			lastTime := time.Unix(0, lastUnix)
			if time.Since(lastTime) > model.MinServiceRunDuration {
				isRunning = false
				continue
			}

			currTime := time.Now()
			elapsed := currTime.Sub(prevTime).Seconds()
			if elapsed <= 0 {
				continue
			}
			networkMetric := ReadNetworkMetric(reader, &netSnap, updateIntervalSecond)
			cpuMetric := ReadCpuMetric(reader, &cpuSnap)
			storageMetric := ReadStorageMetric(reader, diskSnap, updateIntervalSecond)
			memoryMetric := ReadMemoryMetric(reader)
			systemMetric := ReadSystemMetric(reader)

			dms.dynamicMetric = &model.DynamicMetric{
				Cpu:     cpuMetric,
				Memory:  memoryMetric,
				Network: networkMetric,
				Storage: storageMetric,
				System:  systemMetric,
			}
			prevTime = currTime
		}
	}
}

func (dms *DynamicMetricService) GetDynamicMetric() *model.DynamicMetric {
	return dms.dynamicMetric
}
