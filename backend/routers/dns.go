package routers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"openwrt-diskio-api/backend/dns"
	"openwrt-diskio-api/backend/utils"
)

var dnsQueryService *dns.DnsQueryService

func InitDnsQueryService(dnsServerIp string, dnsQueryTimeout time.Duration) {
	dnsQueryService = dns.NewDnsQueryService(
		dnsServerIp,
		dnsQueryTimeout,
	)
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

	utils.SetJsonHeader(w)
	_ = json.NewEncoder(w).Encode(results)
}
