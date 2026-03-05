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
	dnsCache     sync.Map
	resolver     *net.Resolver
	queryTimeout time.Duration
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
		dnsCache:     sync.Map{},
		resolver:     resolver,
		queryTimeout: queryTimeout,
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
		if err != nil {
			log.Printf("Dns query for %q failed: %s\n", ip, err)
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
