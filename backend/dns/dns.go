package dns

import (
	"context"
	"log"
	"net"
	"openwrt-diskio-api/backend/model"
	"strings"
	"sync"
	"time"
)

const DnsCacheExpireTime = 5 * time.Minute

type DnsQueryService struct {
	dnsCache        sync.Map
	resolver        *net.Resolver
	queryTimeout    time.Duration
	neighborService *NeighborService
}

func NewDnsQueryService(dnsIp string, queryTimeout time.Duration) *DnsQueryService {
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: queryTimeout,
			}
			return d.DialContext(ctx, "udp", net.JoinHostPort(dnsIp, "53"))
		},
	}

	return &DnsQueryService{
		dnsCache:        sync.Map{},
		resolver:        resolver,
		queryTimeout:    queryTimeout,
		neighborService: NewNeighborService(),
	}
}

func (dqs *DnsQueryService) LookupAddr(ips []string) (model.DnsResult, error) {
	result := make(model.DnsResult, len(ips))

	// TODO 可以改成异步并发查,但是我觉得性能应该够,先这样吧
	for _, ip := range ips {
		rawCache, ok := dqs.dnsCache.Load(ip)
		if ok {
			cache, ok := rawCache.(model.CacheDnsValue)
			if !ok {
				log.Printf("get dns cache %q failed : not valid %T ", ip, model.CacheValue{})
			}
			if len(cache.Data) > 0 && cache.ExpireAt.After(time.Now()) {
				result[ip] = cache.Data
				continue
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), dqs.queryTimeout)
		names, err := dqs.resolver.LookupAddr(ctx, ip)
		cancel()
		if err != nil || len(names) == 0 {
			// 查询失败如果打印出来会导致有几吨的日志
			// log.Printf("Dns query for %q failed: %s\n", ip, err)
			names = dqs.LookupIpv6ByNeighborService(ip)
		}

		if len(names) == 0 {
			continue
		}

		hostnameList := make([]string, 0, len(names))
		for _, name := range names {
			name = strings.TrimSuffix(name, ".")
			hostnameList = append(hostnameList, name)
		}
		dqs.dnsCache.Store(ip, model.CacheDnsValue{
			ExpireAt: time.Now().Add(DnsCacheExpireTime),
			Data:     hostnameList,
		})
		result[ip] = hostnameList
	}
	return result, nil
}

func (dqs *DnsQueryService) LookupIpv6ByNeighborService(ip string) []string {
	result := make([]string, 0, 1)
	if dqs.neighborService == nil {
		log.Println("neighbor service is not init , skip Neighbor Service(Mac addr) lookup")
		return result
	}
	// 防止无限递归,也不查ipv4的arp
	if !strings.Contains(ip, ":") {
		return result
	}
	dqs.neighborService.ActiveSignal()

	// IPv6 -> MAC
	mac := dqs.neighborService.GetMac(ip)
	if mac == "" {
		return result
	}
	v4 := dqs.neighborService.GetV4ByMac(mac)
	if v4 == "" {
		return result
	}

	// 拿着 IPv4 去递归查一下主机名
	v4Res, _ := dqs.LookupAddr([]string{v4})
	if hostnameList, exists := v4Res[v4]; exists && len(hostnameList) > 0 {
		result = hostnameList
	}
	return result
}
