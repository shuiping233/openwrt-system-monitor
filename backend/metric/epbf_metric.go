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

const EbpfBatchLookupSize = 1024

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
}

func NewEbpfNetTrafficService(keyExpiredTime time.Duration) *EbpfNetTrafficService {
	return &EbpfNetTrafficService{
		keyExpiredTime: keyExpiredTime,
		activeChan:     make(chan struct{}, 1),
		metricsMap:     make(map[netip.Addr]*IPMetrics),
		captureStartAt: time.Now().UnixNano(),
	}
}

func (svc *EbpfNetTrafficService) InitEbpfInterfaceDevice(targetInterface string) error {
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

	numCPU, _ := ebpf.PossibleCPU()
	var (
		batchSize = EbpfBatchLookupSize
		keys      = make([]bpf.BpfFlowKey, batchSize)
		vals      = make([]bpf.BpfFlowStats, batchSize*numCPU)
		cursor    ebpf.MapBatchCursor
	)

	// 重置本轮瞬时速率
	svc.mutex.Lock()
	for _, m := range svc.metricsMap {
		m.UploadRate = 0
		m.DownloadRate = 0
	}

	// 2. 迭代 eBPF Map 进行采样
	for {
		count, err := objs.FlowMap.BatchLookup(&cursor, keys, vals, nil)

		for i := 0; i < count; i++ {
			key := keys[i]
			var totalBytes uint64
			// 聚合所有 CPU 的字节数
			for cpu := 0; cpu < numCPU; cpu++ {
				totalBytes += vals[i*numCPU+cpu].Bytes
			}

			// 计算增量 (Delta)
			lastBytes := lastSnapshots[key] // map 取不到返回 0，逻辑自然成立
			delta := uint64(0)
			if totalBytes > lastBytes {
				delta = totalBytes - lastBytes
			}

			// 更新快照
			lastSnapshots[key] = totalBytes

			// 只有在有流量时才进行聚合计算，减少 parseToAddr 等操作的开销
			if delta > 0 {
				svc.trafficAggregateWithDuration(key, delta, dt)
			}
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

func (svc *EbpfNetTrafficService) Run(ctx context.Context) {
	updateChan, done, err := subscribeNetworkChanges()
	if err != nil {
		log.Fatalln(err)
	}
	defer close(done)
	go svc.WatchNetworkChanges(ctx, updateChan)

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
	clearFlowMap(objs.FlowMap)
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
	stopCapture(svc.objs)
	cleanUpTC(svc.link)
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
			IpType:          model.IpAddressTypeLan, // TODO 先写死,因为其他类型的ip流量抓取还没做
			IpFamily:        ipFamily,
			Incoming:        incoming,
			Outgoing:        outgoing,
			TotalThroughput: totalThroughput,
			TotalIncoming:   totalIncoming,
			TotalOutgoing:   totalOutgoing,
			TotalTraffic:    totalTraffic,
			Tcp:             -1, // TODO 先这样,后面会找NetworkConnection里的值统计好之后再填进去
			Udp:             -1, // TODO 先这样,后面会找NetworkConnection里的值统计好之后再填进去
			Other:           -1, // TODO 先这样,后面会找NetworkConnection里的值统计好之后再填进去
		})
	}
	return result
}

// 在 frame 函数末尾，BatchLookup 循环结束后执行：
func (svc *EbpfNetTrafficService) applySmoothing() {
	// 建议 Alpha 设为 0.3 - 0.5 之间
	// 0.3 极其平滑，但有 1-2 秒延迟；0.5 反应快，但仍有轻微跳动
	const alpha = 0.4

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

func (svc *EbpfNetTrafficService) trafficAggregateWithDuration(key bpf.BpfFlowKey, delta uint64, duration float64) {
	// 速率 = 增量字节 / 实际耗时
	rate := float64(delta) / duration

	srcAddr := svc.parseToAddr(key.SrcAddr, key.Family)
	dstAddr := svc.parseToAddr(key.DstAddr, key.Family)

	// 统计上传 (Source 是本地)
	if srcAddr != svc.interfaceIpv4 && svc.IsInLocalSubnet(srcAddr) {
		metric := getOrCreateMetrics(srcAddr, svc.metricsMap)
		metric.UploadRate += rate
		metric.TotalUpload += delta
	}

	// 统计下载 (Destination 是本地)
	if dstAddr != svc.interfaceIpv4 && svc.IsInLocalSubnet(dstAddr) {
		metric := getOrCreateMetrics(dstAddr, svc.metricsMap)
		metric.DownloadRate += rate
		metric.TotalDownload += delta
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

		for i := 0; i < count; i++ {
			key := keys[i]
			// 找到该 Key 在所有 CPU 上的最大 LastSeen
			var maxLastSeen uint64
			for cpu := 0; cpu < runtime.NumCPU(); cpu++ {
				if vals[i*runtime.NumCPU()+cpu].LastSeen > maxLastSeen {
					maxLastSeen = vals[i*runtime.NumCPU()+cpu].LastSeen
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
		_, _ = objs.FlowMap.BatchDelete(keysToDelete, nil)

		// 2. 从 Go 内存快照删除 (由于 lastSnapshots 不是并发安全的，需要加锁或交给下一帧处理)
		// 这里建议在 svc.mutex 保护下清理
		svc.mutex.Lock()
		for _, k := range keysToDelete {
			delete(lastSnapshots, k)
			// 如果你觉得 metricsMap 里的 IP 也太久没见了，也可以顺便清理
		}
		svc.mutex.Unlock()

		log.Printf("[GC] Cleaned up %d expired flows", len(keysToDelete))
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

func (svc *EbpfNetTrafficService) WatchNetworkChanges(ctx context.Context, ch <-chan netlink.AddrUpdate) {
	log.Println("Watching for network interface changes...")
	for {
		select {
		case <-ctx.Done():
			return
		case update, ok := <-ch:
			if !ok {
				log.Println("Netlink address update channel closed")
				return
			}
			link, _ := netlink.LinkByIndex(update.LinkIndex)
			if link != nil && link.Attrs().Name == svc.captureInterface {
				// 内核很多网卡事件都会进来,所以不打印
				// log.Printf("Network change (NewAddr: %v) detected on %s", update.NewAddr, svc.captureInterface)
				svc.refreshInterfaceInfo()
			}
		}
	}
}

// 一定要记得close(done)通道
func subscribeNetworkChanges() (updateChan chan netlink.AddrUpdate, done chan struct{}, err error) {
	updateChan = make(chan netlink.AddrUpdate)
	done = make(chan struct{})
	if err := netlink.AddrSubscribe(updateChan, done); err != nil {
		return nil, nil, fmt.Errorf("failed to subscribe netlink addr changes: %w", err)
	}
	return updateChan, done, nil
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

func (svc *EbpfNetTrafficService) IsInLocalSubnet(ip netip.Addr) bool {
	if ip.Is4() {
		return svc.interfaceIpv4Prefix.Contains(ip)
	}

	if ip.Is6() {
		// 过滤链路本地地址 (fe80::/10)，这种流量通常不计入互联网统计
		if ip.IsLinkLocalUnicast() {
			return false
		}
		// 只有在 Prefix 有效时才进行判断
		if !svc.interfaceIpv6Prefix.IsValid() {
			return false
		}
		return svc.interfaceIpv6Prefix.Contains(ip)
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

func clearFlowMap(m *ebpf.Map) {
	if m == nil {
		return
	}
	var key bpf.BpfFlowKey
	var keys []bpf.BpfFlowKey

	// 先收集所有 Key
	iter := m.Iterate()
	for iter.Next(&key, nil) {
		keys = append(keys, key)
	}

	if len(keys) == 0 {
		return
	}

	// 批量删除
	_, err := m.BatchDelete(keys, nil)
	if err != nil {
		// 如果内核不支持 BatchDelete，降级回普通循环删除
		for _, k := range keys {
			_ = m.Delete(k)
		}
	}
}
