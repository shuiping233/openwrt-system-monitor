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
	"strings"
	"syscall"
	"time"

	frontend "openwrt-diskio-api"
	"openwrt-diskio-api/backend/dns"
	"openwrt-diskio-api/backend/metric"
	"openwrt-diskio-api/backend/model"

	"github.com/spf13/afero"
)

const workerNumber = 3

var (
	reader     = metric.FsReader{Fs: afero.NewOsFs()}
	runner     = metric.CommandRunner{}
	background = metric.BackgroundService{
		Reader:          reader,
		Runner:          runner,
		UpdateEventChan: make(chan string, workerNumber),
	}
	dnsQueryService *dns.DnsQueryService
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
	background.DynamicMetricServiceActiveSignal()
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
		jsonBytes, err = json.Marshal(&model.NetworkConnectionMetric{})
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
		jsonBytes, err = json.Marshal(&model.StaticMetric{})
		if err != nil {
			errMsg := fmt.Sprintf("json marshal error : %s", err.Error())
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}
	}
	w.Write(jsonBytes)
}
func AggregationTrafficHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	jsonBytes := background.GetJsonBytes(model.JsonCacheKeyAggregationTraffic)
	if len(jsonBytes) == 0 {
		var err error
		jsonBytes, err = json.Marshal(&model.AggregationTrafficMetric{})
		if err != nil {
			errMsg := fmt.Sprintf("json marshal error : %s", err.Error())
			http.Error(w, errMsg, http.StatusInternalServerError)
			return
		}
	}
	w.Write(jsonBytes)
	background.AggregationTrafficServiceActiveSignal()
}
func DnsQueryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "only GET", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	ipParams, ok := query["ip"]
	if !ok {
		http.Error(w, "missing \"ip\" query parameter", http.StatusBadRequest)
		return
	}

	var ips []string
	for _, item := range ipParams {
		if strings.Contains(item, ",") {
			ips = append(ips, strings.Split(item, ",")...)
		} else {
			ips = append(ips, item)
		}
	}

	finalIps := make([]string, 0, len(ips))
	for _, ip := range ips {
		trimmed := strings.TrimSpace(ip)
		if trimmed != "" {
			finalIps = append(finalIps, trimmed)
		}
	}

	if len(finalIps) == 0 {
		http.Error(w, "\"ip\" parameter is empty", http.StatusBadRequest)
		return
	}

	results, err := dnsQueryService.LookupAddr(finalIps)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func PrettyExit(httpServer *http.Server) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

}

func main() {

	var (
		host                        = flag.String("host", "127.0.0.1", "listen host")
		port                        = flag.Int("port", 8080, "listen port")
		dynamicMetricInterval       = flag.Uint("dynamic-metric-interval", 1, "metric update interval")
		networkConnectionInterval   = flag.Uint("network-connection-interval", 10, "network connection details update interval")
		staticMetricInterval        = flag.Uint("static-metric-interval", 60, "metric update interval")
		trafficCaptureInterfaceName = flag.String("traffic-capture-interface-name", "br-lan", "traffic capture interface name , only use on realtime traffic capture and should be input LAN interface")
		trafficKeyExpiredTime       = flag.Duration("traffic-key-expired-time", model.MinServiceRunDuration, "metric update interval")
		dnsServerIp                 = flag.String("dns-server-ip", "127.0.0.1", "dns server ip , ipv6 support , only support tcp or udp 53 port dns")
		dnsQueryTimeout             = flag.Duration("dns-query-timeout", 1*time.Second, "dns query timeout")
	)
	flag.Parse()

	addr := *host + ":" + strconv.Itoa(*port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: http.DefaultServeMux, // 你的 HandleFunc 都注册在这里
	}

	log.Println("print input config : ")
	log.Printf("host : %s", *host)
	log.Printf("port : %d", *port)
	log.Printf("dynamicMetricInterval : %v", *dynamicMetricInterval)
	log.Printf("networkConnectionInterval : %v", *networkConnectionInterval)
	log.Printf("staticMetricInterval : %v", *staticMetricInterval)
	log.Printf("trafficCaptureInterfaceName : %v", *trafficCaptureInterfaceName)
	log.Printf("trafficKeyExpiredTime : %v", *trafficKeyExpiredTime)
	log.Printf("dnsServerIp : %v", *dnsServerIp)
	log.Printf("dnsQueryTimeout : %v", *dnsQueryTimeout)

	background.SetConfig(
		*staticMetricInterval,
		*dynamicMetricInterval,
		*networkConnectionInterval,
		*trafficCaptureInterfaceName,
		*trafficKeyExpiredTime,
	)
	dnsQueryService = dns.NewDnsQueryService(
		*dnsServerIp,
		*dnsQueryTimeout,
	)

	background.UpdateStaticMetric()
	background.UpdateNetworkConnectionDetails()

	canExit := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		log.Println("Receive ctrl+c signal , closing service...")

		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP Server releasing failed: %v", err)
		}
		background.Close()
		close(canExit)
	}()

	go background.RunDynamicMetricService(ctx)
	go background.RunAggregationTrafficService(ctx)
	for index := range workerNumber {
		go background.Worker(index)
	}

	webFS, _ := fs.Sub(frontend.WebEmb, frontend.FrontendDistPath)
	http.Handle("/", http.FileServer(http.FS(webFS)))

	http.HandleFunc("/metric/dynamic", DynamicMetricHandler)
	http.HandleFunc("/metric/network_connection", NetworkConnectionMetricHandler)
	http.HandleFunc("/metric/static", StaticMetricHandler)
	http.HandleFunc("/metric/aggregation_traffic", AggregationTrafficHandler)
	http.HandleFunc("/dns/query", DnsQueryHandler)

	log.Printf("listen http://%s/", addr)
	log.Printf("Interface url : http://%s/metric/dynamic", addr)
	log.Printf("Interface url : http://%s/metric/network_connection", addr)
	log.Printf("Interface url : http://%s/metric/static", addr)
	log.Printf("Interface url : http://%s/metric/aggregation_traffic", addr)
	log.Printf("Interface url : http://%s/dns/query", addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen error: %s\n", err)
	}
	<-canExit
}
