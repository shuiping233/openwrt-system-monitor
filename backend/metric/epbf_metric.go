//go:build linux

package metric

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net/netip"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"openwrt-diskio-api/backend/model"
	bpf "openwrt-diskio-api/backend/pkg/ebpf"
	"openwrt-diskio-api/backend/utils"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	EbpfBatchLookupSize = 1024
	SmoothingAlphaRate  = 0.6
)

var (
	ipv4Broadcast = netip.AddrFrom4([4]byte{255, 255, 255, 255})
)

type IPMetrics struct {
	IP string
	// 瞬时累加速率 (每秒 frame 开始时清零)
	UploadRate   float64
	DownloadRate float64
	// 平滑后的显示速率 (用于输出给前端)
	SmoothUploadRate   float64
	SmoothDownloadRate float64
	// 累计总量
	TotalUpload   uint64
	TotalDownload uint64
	Tcp           int32
	Udp           int32
	Other         int32
}

type IpStatus struct {
	LastIncomingBytes uint64    // 上次统计时的总入向流量
	LastOutgoingBytes uint64    // 上次统计时的总出向流量
	LastSeen          time.Time // 上次统计的精确时间戳

	// 平滑后的速率（用于输出）
	SmoothIncomingRate float64
	SmoothOutgoingRate float64
}

type EbpfNetTrafficService struct {
	captureInterface    string
	interfaceIpv4       netip.Addr
	interfaceIpv4Prefix netip.Prefix
	interfaceIpv6       netip.Addr
	interfaceIpv6Prefix netip.Prefix
	keyExpiredTime      time.Duration
	activeChan          chan struct{}
	objs                *bpf.BpfObjects
	link                netlink.Link
	metricsMap          map[netip.Addr]*IPMetrics
	mutex               sync.RWMutex
	lastRequestTimeUnix int64
	captureStartAt      int64
	lastFrameTime       time.Time
	possibleCpuNumber   int
}

func NewEbpfNetTrafficService(keyExpiredTime time.Duration) *EbpfNetTrafficService {
	return &EbpfNetTrafficService{
		keyExpiredTime:    keyExpiredTime,
		activeChan:        make(chan struct{}, 1),
		metricsMap:        make(map[netip.Addr]*IPMetrics),
		captureStartAt:    time.Now().UnixNano(),
		possibleCpuNumber: runtime.NumCPU(),
	}
}

func (svc *EbpfNetTrafficService) InitEbpfInterfaceDevice(targetInterface string) error {
	possibleCpuNumber, err := ebpf.PossibleCPU()
	if err != nil {
		return fmt.Errorf("Get possible cpu number failed: %w", err)
	}
	svc.possibleCpuNumber = possibleCpuNumber
	log.Printf("Possible cpu number: %d \n", possibleCpuNumber)

	svc.captureInterface = targetInterface
	ipv4, ipv4Prefix, err := utils.GetInterfaceIpv4Info(targetInterface)
	if err != nil {
		return err
	}
	log.Printf("Get %q interface ipv4: %q \n", targetInterface, ipv4.String())
	log.Printf("Get %q interface ipv4Prefix: %q \n", targetInterface, ipv4Prefix.String())
	ipv6, ipv6Prefix, err := utils.GetInterfaceGuaIpv6Info(targetInterface)
	if err != nil {
		return err
	}
	log.Printf("Get %q interface ipv6: %q \n", targetInterface, ipv6.String())
	log.Printf("Get %q interface ipv6Prefix: %q \n", targetInterface, ipv6Prefix.String())
	svc.interfaceIpv4 = ipv4
	svc.interfaceIpv4Prefix = ipv4Prefix
	svc.interfaceIpv6 = ipv6
	svc.interfaceIpv6Prefix = ipv6Prefix

	if err := rlimit.RemoveMemlock(); err != nil {
		return fmt.Errorf("Try to remove ebpf memory lock failed: %w", err)
	}

	objs := bpf.BpfObjects{}
	if err := bpf.LoadBpfObjects(&objs, nil); err != nil {
		return fmt.Errorf("Load BPF object failed: %w", err)
	}

	link, err := netlink.LinkByName(targetInterface)
	if err != nil {
		return fmt.Errorf("Network interface %q not found: %w", targetInterface, err)
	}

	if err := attachTCObjects(link, objs.CountFlow.FD()); err != nil {
		log.Fatalf("Attach network interface %q failed: %s", targetInterface, err)
	}
	log.Printf("Capture traffic from interface %q now\n", targetInterface)

	startCapture(&objs)
	svc.link = link
	svc.objs = &objs

	return nil
}

func (svc *EbpfNetTrafficService) frame(
	objs *bpf.BpfObjects,
	lastSnapshots map[bpf.BpfFlowKey]uint64,
) {
	if !isCapturing(objs) {
		return
	}

	now := time.Now()
	// 计算采样间隔 dt
	dt := now.Sub(svc.lastFrameTime).Seconds()
	if dt <= 0 {
		dt = 1.0
	}

	// 1. 活跃状态检查：如果太久没请求，停止抓取并清理（保持不变，作为安全阀）
	lastUnix := atomic.LoadInt64(&svc.lastRequestTimeUnix)
	if time.Since(time.Unix(0, lastUnix)) > svc.keyExpiredTime {
		svc.shutdownCapture(objs, lastSnapshots)
		return
	}

	numCpu := svc.possibleCpuNumber
	var (
		batchSize = EbpfBatchLookupSize
		keys      = make([]bpf.BpfFlowKey, batchSize)
		vals      = make([]bpf.BpfFlowStats, batchSize*numCpu)
		cursor    ebpf.MapBatchCursor
	)

	// 重置本轮瞬时速率
	svc.mutex.Lock()
	for _, m := range svc.metricsMap {
		m.UploadRate = 0
		m.DownloadRate = 0
		m.Tcp = 0
		m.Udp = 0
		m.Other = 0
	}

	// 2. 迭代 eBPF Map 进行采样
	for {
		count, err := objs.FlowMap.BatchLookup(&cursor, keys, vals, nil)

		for index := range count {
			key := keys[index]
			var totalBytes uint64
			// 聚合所有 CPU 的字节数
			for cpu := range numCpu {
				totalBytes += vals[index*numCpu+cpu].Bytes
			}

			// 计算增量 (Delta)
			lastBytes := lastSnapshots[key] // map 取不到返回 0，逻辑自然成立
			delta := uint64(0)
			if totalBytes > lastBytes {
				delta = totalBytes - lastBytes
			}

			// 更新快照
			lastSnapshots[key] = totalBytes

			srcAddr := svc.parseToAddr(key.SrcAddr, key.Family)
			dstAddr := svc.parseToAddr(key.DstAddr, key.Family)
			rate := float64(delta) / dt

			svc.trafficAggregateWithDuration(srcAddr, dstAddr, delta, rate, key.Proto)
		}
		if err != nil || count < batchSize {
			break
		}
	}
	svc.mutex.Unlock()

	// 3. 更新时间轴并应用平滑
	svc.lastFrameTime = now
	svc.applySmoothing()
}

func (svc *EbpfNetTrafficService) trafficAggregateWithDuration(srcAddr netip.Addr, dstAddr netip.Addr, delta uint64, rate float64, proto uint8) {
	// 统计上传
	if !IsIgnoredAddr(srcAddr) {
		metric := getOrCreateMetrics(srcAddr, svc.metricsMap)
		matchProtoAndCount(proto, metric)
		if delta > 0 {
			metric.UploadRate += rate
			metric.TotalUpload += delta
		}
	}
	// 统计下载
	if !IsIgnoredAddr(dstAddr) {
		metric := getOrCreateMetrics(dstAddr, svc.metricsMap)
		matchProtoAndCount(proto, metric)
		if delta > 0 {
			metric.DownloadRate += rate
			metric.TotalDownload += delta
		}
	}
}

func matchProtoAndCount(proto uint8, metric *IPMetrics) {
	switch proto {
	case model.ProtoTCP:
		metric.Tcp += 1
	case model.ProtoUDP:
		metric.Udp += 1
	default:
		metric.Other += 1
	}
}

func (svc *EbpfNetTrafficService) Run(ctx context.Context) {
	addrChan, linkChan, done, err := subscribeNetworkChanges()
	if err != nil {
		log.Fatalln(err)
	}
	defer close(done)
	go svc.WatchNetworkChanges(ctx, addrChan, linkChan)

	objs := svc.objs
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	gcTicker := time.NewTicker(5 * time.Second)
	defer gcTicker.Stop()

	keyExpiredTime := svc.keyExpiredTime
	lastSnapshots := make(map[bpf.BpfFlowKey]uint64)
	atomic.StoreInt64(&svc.lastRequestTimeUnix, time.Now().UnixNano())
	atomic.StoreInt64(&svc.captureStartAt, time.Now().UnixNano())

	for {
		select {
		case <-ctx.Done():
			return

		case <-svc.activeChan:
			// 1. 收到接口请求信号，刷新最后活跃时间
			if !isCapturing(objs) {
				atomic.StoreInt64(&svc.captureStartAt, time.Now().UnixNano())
				startCapture(objs)
			}

		case <-ticker.C:
			svc.frame(
				objs,
				lastSnapshots,
			)
		case <-gcTicker.C:
			// 异步执行清理
			go svc.cleanupExpiredFlows(objs, keyExpiredTime, lastSnapshots)

		}
	}
}

func (svc *EbpfNetTrafficService) shutdownCapture(objs *bpf.BpfObjects, lastSnapshots map[bpf.BpfFlowKey]uint64) {
	stopCapture(objs)
	clearFlowMap(objs.FlowMap, svc.possibleCpuNumber)
	svc.mutex.Lock()
	clear(svc.metricsMap)
	svc.mutex.Unlock()
	clear(lastSnapshots)
}

func (svc *EbpfNetTrafficService) ActiveSignal() {
	// log.Println("Receive ebpf active signal")
	atomic.StoreInt64(&svc.lastRequestTimeUnix, time.Now().UnixNano())

	// 如果通道满了，说明 Run 循环还没来得及处理之前的信号
	// 没关系，因为时间戳已经原子更新了，Run 下一秒会读到最新的
	select {
	case svc.activeChan <- struct{}{}:
	default:
	}
}

func (svc *EbpfNetTrafficService) Close() {
	log.Println("Cleaning ebpf resources...")
	if svc.objs == nil {
		return
	}
	if isCapturing(svc.objs) {
		stopCapture(svc.objs)
	}
	if svc.link != nil {
		cleanUpTC(svc.link)
	}
	svc.objs.Close()
}

func (svc *EbpfNetTrafficService) GetAggregationTrafficMetric() *model.AggregationTrafficMetric {
	metricsMap := svc.metricsMap
	captureStartAtUnix := atomic.LoadInt64(&svc.captureStartAt)
	captureStartAt := time.Unix(0, captureStartAtUnix)
	result := &model.AggregationTrafficMetric{
		CaptureStartAt:   captureStartAt,
		CaptureInterface: svc.captureInterface,
		Details:          make([]model.AggregationTrafficDetails, 0, len(metricsMap)),
	}
	svc.mutex.RLock()
	defer svc.mutex.RUnlock()
	for ip, value := range metricsMap {

		IpType := model.IpAddressTypeWan
		// 统计上传 (Source 是本地)
		if svc.IsLanIp(ip) {
			IpType = model.IpAddressTypeLan
		} else if IsUnknownIp(ip) {
			IpType = model.IpAddressTypeUnknown
		}

		ipStr := formatIP(ip)
		rate, unit := utils.ConvertBytes(value.SmoothDownloadRate, model.BSecond)
		incoming := model.MetricUnit{Value: rate, Unit: unit}

		rate, unit = utils.ConvertBytes(value.SmoothUploadRate, model.BSecond)
		outgoing := model.MetricUnit{Value: rate, Unit: unit}

		rate, unit = utils.ConvertBytes(value.SmoothDownloadRate+value.SmoothUploadRate, model.BSecond)
		totalThroughput := model.MetricUnit{Value: rate, Unit: unit}

		total, unit := utils.ConvertBytes(float64(value.TotalDownload), model.Byte)
		totalIncoming := model.MetricUnit{
			Value: total,
			Unit:  unit,
		}
		total, unit = utils.ConvertBytes(float64(value.TotalUpload), model.Byte)
		totalOutgoing := model.MetricUnit{
			Value: total,
			Unit:  unit,
		}
		total, unit = utils.ConvertBytes(float64(value.TotalDownload+value.TotalUpload), model.Byte)
		totalTraffic := model.MetricUnit{
			Value: total,
			Unit:  unit,
		}

		ipFamily := model.IpFamilyTypeIpv4
		if ip.Is6() {
			ipFamily = model.IpFamilyTypeIpv6
		}

		result.Details = append(result.Details, model.AggregationTrafficDetails{
			Ip:              ipStr,
			IpType:          IpType,
			IpFamily:        ipFamily,
			Incoming:        incoming,
			Outgoing:        outgoing,
			TotalThroughput: totalThroughput,
			TotalIncoming:   totalIncoming,
			TotalOutgoing:   totalOutgoing,
			TotalTraffic:    totalTraffic,
			Tcp:             value.Tcp / 2,
			Udp:             value.Udp / 2,
			Other:           value.Other / 2,
		})
	}
	return result
}

// 在 frame 函数末尾，BatchLookup 循环结束后执行：
func (svc *EbpfNetTrafficService) applySmoothing() {
	// 建议 Alpha 设为 0.3 - 0.5 之间
	// 0.3 极其平滑，但有 1-2 秒延迟；0.5 反应快，但仍有轻微跳动
	const alpha = SmoothingAlphaRate

	for _, m := range svc.metricsMap {
		// 对上传速率进行平滑
		if m.SmoothUploadRate == 0 {
			m.SmoothUploadRate = m.UploadRate
		} else {
			m.SmoothUploadRate = (alpha * m.UploadRate) + ((1 - alpha) * m.SmoothUploadRate)
		}

		// 对下载速率进行平滑
		if m.SmoothDownloadRate == 0 {
			m.SmoothDownloadRate = m.DownloadRate
		} else {
			m.SmoothDownloadRate = (alpha * m.DownloadRate) + ((1 - alpha) * m.SmoothDownloadRate)
		}

		// 补偿：如果平滑后的值极小（比如小于 1B/s），直接归零，防止 UI 长期显示微小余波
		if m.SmoothUploadRate < 1 {
			m.SmoothUploadRate = 0
		}
		if m.SmoothDownloadRate < 1 {
			m.SmoothDownloadRate = 0
		}
	}
}

func (svc *EbpfNetTrafficService) parseToAddr(addr [4]uint32, family uint8) netip.Addr {
	if family == 2 { // AF_INET
		// 将小端序 uint32 转为 4 字节数组
		b := [4]byte{byte(addr[0]), byte(addr[0] >> 8), byte(addr[0] >> 16), byte(addr[0] >> 24)}
		return netip.AddrFrom4(b)
	}
	// IPv6: 直接从 16 字节切片读取
	b := [16]byte{}
	binary.NativeEndian.PutUint32(b[0:4], addr[0])
	binary.NativeEndian.PutUint32(b[4:8], addr[1])
	binary.NativeEndian.PutUint32(b[8:12], addr[2])
	binary.NativeEndian.PutUint32(b[12:16], addr[3])
	return netip.AddrFrom16(b)
}

func (svc *EbpfNetTrafficService) cleanupExpiredFlows(
	objs *bpf.BpfObjects,
	timeout time.Duration,
	lastSnapshots map[bpf.BpfFlowKey]uint64,
) {
	nowKtime := getKtimeNS()
	timeoutNS := uint64(timeout.Nanoseconds())

	var (
		batchSize = EbpfBatchLookupSize
		keys      = make([]bpf.BpfFlowKey, EbpfBatchLookupSize)
		// 清理时我们只关心 LastSeen，可以减轻读取压力
		vals         = make([]bpf.BpfFlowStats, EbpfBatchLookupSize*runtime.NumCPU())
		cursor       ebpf.MapBatchCursor
		keysToDelete []bpf.BpfFlowKey
	)

	for {
		count, err := objs.FlowMap.BatchLookup(&cursor, keys, vals, nil)

		for index := range count {
			key := keys[index]
			// 找到该 Key 在所有 CPU 上的最大 LastSeen
			var maxLastSeen uint64
			for cpu := 0; cpu < runtime.NumCPU(); cpu++ {
				if vals[index*runtime.NumCPU()+cpu].LastSeen > maxLastSeen {
					maxLastSeen = vals[index*runtime.NumCPU()+cpu].LastSeen
				}
			}

			// 如果超过过期时间，加入待删除队列
			if nowKtime-maxLastSeen > timeoutNS {
				keysToDelete = append(keysToDelete, key)
			}
		}

		if err != nil || count < batchSize {
			break
		}
	}

	if len(keysToDelete) > 0 {
		// 1. 从内核 Map 批量删除
		_, err := objs.FlowMap.BatchDelete(keysToDelete, nil)
		if err != nil {
			log.Println("Batch delete ebpf flow map failed:", err)
		}

		// 2. 从 Go 内存快照删除 (由于 lastSnapshots 不是并发安全的，需要加锁或交给下一帧处理)
		// 这里建议在 svc.mutex 保护下清理
		svc.mutex.Lock()
		for _, k := range keysToDelete {
			delete(lastSnapshots, k)
			// 如果你觉得 metricsMap 里的 IP 也太久没见了，也可以顺便清理
		}
		// 额外逻辑：清理 metricsMap 中长期无流量的 IP
		for ip, m := range svc.metricsMap {
			if m.UploadRate == 0 && m.DownloadRate == 0 && m.SmoothUploadRate < 0.1 && m.SmoothDownloadRate < 0.1 {
				// 为了保险，可以再加个静默时间判断，防止闪现
				delete(svc.metricsMap, ip)
			}
		}
		svc.mutex.Unlock()

		// log.Printf("[GC] Cleaned up %d expired flows", len(keysToDelete))
	}
}

func (svc *EbpfNetTrafficService) refreshInterfaceInfo() {
	// 防止 refresh 时 frame 函数正在读取
	ipv4, ipv4Prefix, err4 := utils.GetInterfaceIpv4Info(svc.captureInterface)
	if err4 != nil {
		log.Println(err4)
	}
	ipv6, ipv6Prefix, err6 := utils.GetInterfaceGuaIpv6Info(svc.captureInterface)
	if err6 != nil {
		log.Println(err6)
	}
	if err4 != nil && err6 != nil {
		return
	}

	v4Change := ipv4Prefix != svc.interfaceIpv4Prefix
	v6Change := ipv6Prefix != svc.interfaceIpv6Prefix
	if !v4Change && !v6Change {
		return
	}

	svc.mutex.Lock()
	defer svc.mutex.Unlock()
	if err4 == nil && v4Change {
		svc.interfaceIpv4 = ipv4
		svc.interfaceIpv4Prefix = ipv4Prefix
		log.Printf("[Network] IPv4 Updated: %s (Prefix: %s)", ipv4, ipv4Prefix)
	}

	if err6 == nil && v6Change {
		svc.interfaceIpv6 = ipv6
		svc.interfaceIpv6Prefix = ipv6Prefix
		log.Printf("[Network] IPv6 Updated: %s (Prefix: %s)", ipv6, ipv6Prefix)
	}
}

func (svc *EbpfNetTrafficService) WatchNetworkChanges(ctx context.Context, addrChan <-chan netlink.AddrUpdate, linkChan chan netlink.LinkUpdate) {
	log.Println("Watching for network interface changes...")
	for {
		select {
		case <-ctx.Done():
			return
		case signal, ok := <-addrChan:
			if !ok {
				log.Println("Netlink address update channel closed")
				return
			}
			link, _ := netlink.LinkByIndex(signal.LinkIndex)
			if link != nil && link.Attrs().Name == svc.captureInterface {
				// 内核很多网卡事件都会进来,所以不打印
				// log.Printf("Network change (NewAddr: %v) detected on %s", update.NewAddr, svc.captureInterface)
				svc.refreshInterfaceInfo()
			}
		case signal, ok := <-linkChan:
			if !ok {
				log.Println("Netlink device update channel closed")
				return
			}
			// 网卡状态变了 (重点解决 eBPF 失效)
			if signal.Attrs().Name != svc.captureInterface {
				continue
			}
			log.Printf("Detected network interface state changed on %s", signal.Attrs().Name)

			// 只有当接口处于 UP 状态且 (是新创建的链接 或 状态真的从 DOWN 变 UP)
			isUp := signal.Attrs().RawFlags&unix.IFF_UP != 0
			if !isUp {
				continue
			}

			log.Printf("Network interface %q is UP, checking eBPF attachment...", svc.captureInterface)

			// 重新挂载
			if svc.objs == nil {
				log.Printf("Ebpf objects is nil")
				continue
			}

			targetLink := signal.Link
			if targetLink == nil {
				log.Printf("signal.Link from link update channel is nil , try to get it by name")
				targetLink_, err := netlink.LinkByName(svc.captureInterface)
				if err != nil {
					log.Printf("Failed to get link by name: %v", err)
					continue
				}
				targetLink = targetLink_
			}

			err := attachTCObjects(targetLink, svc.objs.CountFlow.FD())
			if err != nil {
				log.Printf("Ebpf re-attach failed: %v", err)
			}

			svc.mutex.Lock()
			svc.link = targetLink
			svc.mutex.Unlock()
			log.Printf("Ebpf re-attached to %q (Index: %d)", svc.captureInterface, targetLink.Attrs().Index)

			svc.refreshInterfaceInfo()
		}
	}
}

// 一定要记得close(done)通道
func subscribeNetworkChanges() (addrChan chan netlink.AddrUpdate, linkChan chan netlink.LinkUpdate, done chan struct{}, err error) {
	addrChan = make(chan netlink.AddrUpdate)
	linkChan = make(chan netlink.LinkUpdate)
	done = make(chan struct{})
	if err := netlink.AddrSubscribe(addrChan, done); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to subscribe netlink ip address changes: %w", err)
	}
	if err := netlink.LinkSubscribe(linkChan, done); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to subscribe netlink device changes: %w", err)
	}
	return addrChan, linkChan, done, nil
}

func getOrCreateMetrics(ip netip.Addr, res map[netip.Addr]*IPMetrics) *IPMetrics {
	if m, ok := res[ip]; ok {
		return m
	}
	m := &IPMetrics{IP: formatIP(ip)}
	res[ip] = m
	return m
}

func getKtimeNS() uint64 {
	var ts unix.Timespec
	unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts)
	return uint64(ts.Sec)*1e9 + uint64(ts.Nsec)
}

func (svc *EbpfNetTrafficService) IsLanIp(ip netip.Addr) bool {
	if ip.Is4() {
		return svc.interfaceIpv4Prefix.Contains(ip)
	}

	if ip.Is6() {
		// 只有在 Prefix 有效时才进行判断
		if !svc.interfaceIpv6Prefix.IsValid() {
			return false
		}
		return svc.interfaceIpv6Prefix.Contains(ip)
	}
	return false
}

func IsWanIp(ip netip.Addr) bool {
	if ip.IsGlobalUnicast() {
		return true
	}
	return false
}

func IsUnknownIp(ip netip.Addr) bool {
	// 2. 协议噪音过滤：过滤所有组播 (IPv4 224+ / IPv6 ffxx::)
	// 源码里 IsMulticast 涵盖了所有的 Multicast 变体
	if ip.IsMulticast() {
		return true
	}

	// 4. 补漏：IPv4 全网广播
	if ip.Is4() && ip == ipv4Broadcast {
		return true
	}
	return false
}

func IsIgnoredAddr(ip netip.Addr) bool {

	if !ip.IsValid() || ip.IsUnspecified() || ip.IsLoopback() {
		return true
	}

	// 3. 链路本地过滤：过滤 fe80:: 和 169.254.x.x
	// 这些地址不经过三层路由，仅用于二层链路发现
	if ip.IsLinkLocalUnicast() {
		return true
	}

	return false
}

func formatIP(n netip.Addr) string {
	return n.String()
}

// --- TC 控制 ---

func attachTCObjects(link netlink.Link, fd int) error {
	cleanUpTC(link)
	qdisc := &netlink.GenericQdisc{
		QdiscAttrs: netlink.QdiscAttrs{
			LinkIndex: link.Attrs().Index,
			Handle:    netlink.MakeHandle(0xffff, 0),
			Parent:    netlink.HANDLE_CLSACT,
		},
		QdiscType: "clsact",
	}
	if err := netlink.QdiscAdd(qdisc); err != nil {
		return err
	}

	parents := []uint32{netlink.HANDLE_MIN_INGRESS, netlink.HANDLE_MIN_EGRESS}
	for _, parent := range parents {
		filter := &netlink.BpfFilter{
			FilterAttrs: netlink.FilterAttrs{
				LinkIndex: link.Attrs().Index,
				Parent:    parent,
				Priority:  1,
				Protocol:  unix.ETH_P_ALL,
			},
			Fd:           fd,
			DirectAction: true,
		}
		if err := netlink.FilterAdd(filter); err != nil {
			return err
		}
	}
	return nil
}

func cleanUpTC(link netlink.Link) {
	qdiscs, _ := netlink.QdiscList(link)
	for _, q := range qdiscs {
		if q.Attrs().Parent == netlink.HANDLE_CLSACT {
			netlink.QdiscDel(q)
		}
	}
}

func startCapture(objs *bpf.BpfObjects) {
	log.Println("Enable ebpf network traffic capture")
	key := uint32(0)
	val := uint32(1)
	objs.ConfigMap.Update(&key, &val, ebpf.UpdateAny)
}

func stopCapture(objs *bpf.BpfObjects) {
	if objs == nil || objs.ConfigMap == nil {
		return
	}
	log.Println("Disable ebpf network traffic capture")
	key := uint32(0)
	val := uint32(0)
	objs.ConfigMap.Update(&key, &val, ebpf.UpdateAny)
}

func isCapturing(objs *bpf.BpfObjects) bool {
	key := uint32(0)
	val := uint32(0)

	err := objs.ConfigMap.Lookup(&key, &val)
	if err != nil {
		return false
	}
	return val == 1
}

func clearFlowMap(ebpfMap *ebpf.Map, numCpu int) {
	if ebpfMap == nil {
		return
	}

	var (
		keys = make([]bpf.BpfFlowKey, EbpfBatchLookupSize)
		// 依然需要提供合法的内存空间给 Value
		vals   = make([]bpf.BpfFlowStats, EbpfBatchLookupSize*numCpu)
		cursor ebpf.MapBatchCursor
	)

	for {
		// BatchLookup 虽然也需要 vals，但它在处理循环时比 Iterator 更健壮
		count, err := ebpfMap.BatchLookup(&cursor, keys, vals, nil)

		if count > 0 {
			_, _ = ebpfMap.BatchDelete(keys[:count], nil)
		}

		if err != nil { // 包括 io.EOF
			break
		}
	}
}
