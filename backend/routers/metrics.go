package routers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"openwrt-diskio-api/backend/metric"
	"openwrt-diskio-api/backend/model"
	"openwrt-diskio-api/backend/utils"
	"time"

	"github.com/spf13/afero"
)

const workerNumber = 3

var (
	reader     = metric.FsReader{Fs: afero.NewOsFs()}
	runner     = metric.CommandRunner{}
	Background = metric.BackgroundService{
		Reader:          reader,
		Runner:          runner,
		UpdateEventChan: make(chan string, workerNumber),
	}
)

func InitAllMetricService(
	staticMetricInterval uint,
	dynamicMetricInterval uint,
	networkConnectionInterval uint,
	trafficCaptureInterfaceName string,
	trafficKeyExpiredTime time.Duration,
) {
	Background.SetConfig(
		staticMetricInterval,
		dynamicMetricInterval,
		networkConnectionInterval,
		trafficCaptureInterfaceName,
		trafficKeyExpiredTime,
	)
	Background.UpdateStaticMetric()
	Background.UpdateNetworkConnectionDetails()
}

func RunAllMetricService(ctx context.Context) {
	go Background.RunDynamicMetricService(ctx)
	go Background.RunAggregationTrafficService(ctx)
	for index := range workerNumber {
		go Background.Worker(index)
	}
}

func DynamicMetricHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET", http.StatusMethodNotAllowed)
		return
	}

	utils.SetJsonHeader(w)

	jsonBytes, isGzip := Background.GetJsonBytes(model.JsonCacheKeyDynamicMetric)
	if len(jsonBytes) == 0 {
		var err error
		jsonBytes, err = json.Marshal(&model.DynamicMetric{})
		if err != nil {
			errMsg := fmt.Sprintf("json marshal error : %s", err.Error())
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}
	}
	if isGzip {
		utils.SetGzipHeader(w)
	}
	_, _ = w.Write(jsonBytes)
	Background.DynamicMetricServiceActiveSignal()
}

func NetworkConnectionMetricHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET", http.StatusMethodNotAllowed)
		return
	}

	utils.SetJsonHeader(w)

	jsonBytes, isGzip := Background.GetJsonBytes(model.JsonCacheKeyNetworkConnectionMetric)
	if len(jsonBytes) == 0 {
		var err error
		jsonBytes, err = json.Marshal(&model.NetworkConnectionMetric{})
		if err != nil {
			errMsg := fmt.Sprintf("json marshal error : %s", err.Error())
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}
	}
	if isGzip {
		utils.SetGzipHeader(w)
	}
	_, _ = w.Write(jsonBytes)
}

func StaticMetricHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET", http.StatusMethodNotAllowed)
		return
	}
	utils.SetJsonHeader(w)

	jsonBytes, isGzip := Background.GetJsonBytes(model.JsonCacheKeyStaticMetric)
	if len(jsonBytes) == 0 {
		var err error
		jsonBytes, err = json.Marshal(&model.StaticMetric{})
		if err != nil {
			errMsg := fmt.Sprintf("json marshal error : %s", err.Error())
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}
	}
	if isGzip {
		utils.SetGzipHeader(w)
	}
	_, _ = w.Write(jsonBytes)
}
func AggregationTrafficHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET", http.StatusMethodNotAllowed)
		return
	}
	utils.SetJsonHeader(w)

	jsonBytes, isGzip := Background.GetJsonBytes(model.JsonCacheKeyAggregationTraffic)
	if len(jsonBytes) == 0 {
		var err error
		jsonBytes, err = json.Marshal(&model.AggregationTrafficMetric{})
		if err != nil {
			errMsg := fmt.Sprintf("json marshal error : %s", err.Error())
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}
	}
	if isGzip {
		utils.SetGzipHeader(w)
	}
	_, _ = w.Write(jsonBytes)
	Background.AggregationTrafficServiceActiveSignal()
}
