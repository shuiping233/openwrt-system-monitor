//go:build linux
// +build linux

//go:generate bpf2go -target bpf -output-dir ./pkg/ebpf -go-package ebpf Bpf ./ebpf/monitor.c -- -I/usr/include/bpf

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	frontend "openwrt-diskio-api"
	"openwrt-diskio-api/backend/metric"
	"openwrt-diskio-api/backend/model"

	"github.com/spf13/afero"
)

const workerNumber = 2

var (
	reader     = metric.FsReader{Fs: afero.NewOsFs()}
	runner     = metric.CommandRunner{}
	background = metric.BackgroundService{
		Reader:          reader,
		Runner:          runner,
		UpdateEventChan: make(chan string, workerNumber),
	}
)

func DynamicMetricHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	jsonBytes := background.GetJsonBytes(model.JsonCacheKeyDynamicMetric)
	if len(jsonBytes) == 0 {
		var err error
		jsonBytes, err = json.Marshal(&model.DynamicMetric{})
		if err != nil {
			errMsg := fmt.Sprintf("json marshal error : %s", err.Error())
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}
	}
	w.Write(jsonBytes)
}
func NetworkConnectionMetricHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	jsonBytes := background.GetJsonBytes(model.JsonCacheKeyNetworkConnectionMetric)
	if len(jsonBytes) == 0 {
		var err error
		jsonBytes, err = json.Marshal(&model.DynamicMetric{})
		if err != nil {
			errMsg := fmt.Sprintf("json marshal error : %s", err.Error())
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}
	}
	w.Write(jsonBytes)
}
func StaticMetricHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	jsonBytes := background.GetJsonBytes(model.JsonCacheKeyStaticMetric)
	if len(jsonBytes) == 0 {
		var err error
		jsonBytes, err = json.Marshal(&model.DynamicMetric{})
		if err != nil {
			errMsg := fmt.Sprintf("json marshal error : %s", err.Error())
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}
	}
	w.Write(jsonBytes)
}

func PrettyExit(httpServer *http.Server) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	if err := httpServer.Shutdown(context.Background()); err != nil {
		log.Printf("HTTP server close failed : %v", err)
	}
	log.Println("receive exit signal , closing service...")
	background.Close()
	os.Exit(0)
}

func main() {

	var (
		host                      = flag.String("host", "127.0.0.1", "listen host")
		port                      = flag.Int("port", 8080, "listen port")
		dynamicMetricInterval     = flag.Uint("dynamic-metric-interval", 1, " metric update interval")
		networkConnectionInterval = flag.Uint("network-connection-interval", 10, " network connection details update interval")
		staticMetricInterval      = flag.Uint("static-metric-interval", 60, " metric update interval")
	)
	flag.Parse()

	addr := *host + ":" + strconv.Itoa(*port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: http.DefaultServeMux, // 你的 HandleFunc 都注册在这里
	}

	go PrettyExit(httpServer)

	log.Println("print input config : ")
	log.Printf("host : %s", *host)
	log.Printf("port : %d", *port)
	log.Printf("dynamicMetricInterval : %v", *dynamicMetricInterval)
	log.Printf("networkConnectionInterval : %v", *networkConnectionInterval)
	log.Printf("staticMetricInterval : %v", *staticMetricInterval)

	background.SetUpdateDynamicMetricInterval(*dynamicMetricInterval)
	background.SetUpdateNetworkConnectionDetailsInterval(*networkConnectionInterval)
	background.SetUpdateStaticMetricInterval(*staticMetricInterval)

	background.UpdateStaticMetric()
	background.UpdateNetworkConnectionDetails()

	go background.UpdateDynamicMetric()
	for index := range workerNumber {
		go background.Worker(index)
	}

	webFS, _ := fs.Sub(frontend.WebEmb, frontend.FrontendDistPath)
	http.Handle("/", http.FileServer(http.FS(webFS)))

	http.HandleFunc("/metric/dynamic", DynamicMetricHandler)
	http.HandleFunc("/metric/network_connection", NetworkConnectionMetricHandler)
	http.HandleFunc("/metric/static", StaticMetricHandler)

	log.Printf("listen http://%s/", addr)
	log.Printf("Interface url : http://%s/metric/dynamic", addr)
	log.Printf("Interface url : http://%s/metric/network_connection", addr)
	log.Printf("Interface url : http://%s/metric/static", addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen error: %s\n", err)
	}
}
