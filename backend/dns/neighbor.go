package dns

import (
	"context"
	"fmt"
	"log"
	"openwrt-diskio-api/backend/model"
	"sync/atomic"
	"time"

	"github.com/vishvananda/netlink"
)

const NeighReloadInterval = 30 * time.Second

type NeighborService struct {
	ipToMac    atomic.Value
	macToV4    atomic.Value
	activeChan chan struct{}
	lastActive int64
}

func NewNeighborService() *NeighborService {
	ns := &NeighborService{
		activeChan: make(chan struct{}, 1),
		lastActive: time.Now().UnixNano(),
	}
	ns.ipToMac.Store(make(map[string]string))
	ns.macToV4.Store(make(map[string]string))
	_ = ns.Reload()
	return ns
}

func (ns *NeighborService) ActiveSignal() {
	atomic.StoreInt64(&ns.lastActive, time.Now().UnixNano())
	select {
	case ns.activeChan <- struct{}{}:
	default:
	}
}

func (ns *NeighborService) Run(ctx context.Context) {
	ticker := time.NewTicker(NeighReloadInterval)
	defer ticker.Stop()

	isRunning := true
	// TODO 这里可以做netlink.NeighSubscribe,但是处理Neigh事件挺麻烦的
	// 先不做

	for {
		select {
		case <-ctx.Done():
			return
		case <-ns.activeChan:
			if !isRunning {
				isRunning = true
				log.Println("Enable ipv6 neighbor query service")
				if err := ns.Reload(); err != nil {
					log.Println(err)
				}
			}

		case <-ticker.C:
			if !isRunning {
				continue
			}
			lastActiveUnix := atomic.LoadInt64(&ns.lastActive)
			if time.Since(time.Unix(0, lastActiveUnix)) > model.MinServiceRunDuration {
				isRunning = false
				log.Println("Disable ipv6 neighbor query service")
				continue
			}

			if err := ns.Reload(); err != nil {
				log.Println(err)
			}
		}
	}
}

func (ns *NeighborService) Reload() error {
	// 构造临时 map
	nextIpToMac := make(map[string]string)
	nextMacToV4 := make(map[string]string)

	v4, err := netlink.NeighList(0, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("Get ipv4 neigh list failed: %w", err)
	}
	v6, err := netlink.NeighList(0, netlink.FAMILY_V6)
	if err != nil {
		return fmt.Errorf("Get ipv6 neigh list failed: %w", err)
	}

	for _, n := range append(v4, v6...) {
		if n.State&(netlink.NUD_INCOMPLETE|netlink.NUD_FAILED|netlink.NUD_NONE) == 0 {
			ip := n.IP.String()
			mac := n.HardwareAddr.String()
			if mac == "" {
				continue
			}

			nextIpToMac[ip] = mac
			if n.IP.To4() != nil {
				nextMacToV4[mac] = ip
			}
		}
	}
	ns.ipToMac.Store(nextIpToMac)
	ns.macToV4.Store(nextMacToV4)
	return nil
}

func (ns *NeighborService) GetMac(ip string) string {
	m, ok := ns.ipToMac.Load().(map[string]string)
	if !ok {
		log.Println("Arp search ipToMac map load failed: not a map[string]string type")
		return ""
	}
	result, ok := m[ip]
	if !ok {
		return ""
	}
	return result
}

func (ns *NeighborService) GetV4ByMac(mac string) string {
	m, ok := ns.macToV4.Load().(map[string]string)
	if !ok {
		log.Println("Arp search macToV4 map load failed: not a map[string]string type")
		return ""
	}
	result, ok := m[mac]
	if !ok {
		return ""
	}
	return result
}
