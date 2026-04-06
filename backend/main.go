//go:build linux
// +build linux

//go:generate bpf2go -target bpf -output-dir ./pkg/ebpf -go-package ebpf Bpf ./ebpf/monitor.c -- -I/usr/include/bpf

package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	frontend "openwrt-diskio-api"
	"openwrt-diskio-api/backend/model"
	"openwrt-diskio-api/backend/routers"
)

func PrettyExit(httpServer *http.Server) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
}

func main() {
	log.SetOutput(os.Stdout)

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
		Addr:              addr,
		Handler:           http.DefaultServeMux,
		ReadHeaderTimeout: model.HttpServerReadHeaderTimeout,
		ReadTimeout:       model.HttpServerReadTimeout,
		WriteTimeout:      model.HttpServerWriteTimeout,
		IdleTimeout:       model.HttpServerIdleTimeout,
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

	routers.InitAllMetricService(
		*staticMetricInterval,
		*dynamicMetricInterval,
		*networkConnectionInterval,
		*trafficCaptureInterfaceName,
		*trafficKeyExpiredTime,
	)
	routers.InitDnsQueryService(
		*dnsServerIp,
		*dnsQueryTimeout,
	)

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
		routers.Background.Close()
		close(canExit)
	}()

	routers.RunAllMetricService(ctx)

	// webFS, _ := fs.Sub(frontend.WebEmb, frontend.FrontendDistPath)
	// http.Handle("/", http.FileServer(http.FS(webFS)))
	routers.InitFs(frontend.WebEmb, frontend.FrontendDistPath)
	http.HandleFunc("/", routers.FileServer)

	http.HandleFunc("/metric/dynamic", routers.DynamicMetricHandler)
	http.HandleFunc("/metric/network_connection", routers.NetworkConnectionMetricHandler)
	http.HandleFunc("/metric/static", routers.StaticMetricHandler)
	http.HandleFunc("/metric/aggregation_traffic", routers.AggregationTrafficHandler)
	http.HandleFunc("/dns/query", routers.DnsQueryHandler)

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
