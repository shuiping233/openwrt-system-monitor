//go:build linux
// +build linux

package metric

import (
	"encoding/binary"
	"fmt"
	"log"
	"net/netip"
	"strconv"
	"strings"

	"openwrt-diskio-api/backend/model"
	"openwrt-diskio-api/backend/utils"
)

var (
	procPaths = model.ProcfsPaths{}
)

type rawConn struct {
	ipFamily string
	protocol string
	state    string // 只有tcp有
	kv       map[string]string
}

// TODO 暂时只实现了读取cpu总温度,arm设备一般cpu簇共用一个温度传感器,后面再说
func readCpuTemperature(reader FsReaderInterface) (float64, string) {
	raw, err := reader.ReadFile(procPaths.CpuTemp())
	if err != nil {
		return -1, model.Celsius
	}
	microCelsius, _ := strconv.Atoi(strings.TrimSpace(raw)) // 毫摄氏度
	return float64(microCelsius) / 1000, model.Celsius
}

func readCpuIdle(reader FsReaderInterface) (allCoreCycles uint64, allCoreIdle uint64, coresIdle []model.CpuSnapUnit, err error) {
	raw, err := reader.ReadFile(procPaths.CpuUsage())
	if err != nil {
		return 0, 0, coresIdle, err
	}
	lines := strings.Split(raw, "\n")

	for _, line := range lines {
		if !strings.HasPrefix(line, "cpu") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}

		// 所有核心的汇总时间片,第5列是 idle 时间 :
		// cpu  131326 24084 154131 514013688 7967 0 10506 0 0 0

		// 各个核心各自的时间片,第5列是 idle 时间 :
		// cpu0 6068 1272 7995 25699473 227 0 6274 0 0 0
		// cpu1 6569 972 6603 25700482 1112 0 1322 0 0 0

		idle, _ := strconv.ParseUint(fields[4], 10, 64)
		coreCycles, err := utils.SumUint64(fields[1:])
		if err != nil {
			return 0, 0, coresIdle, err
		}
		if fields[0] == "cpu" {
			allCoreCycles = coreCycles
			allCoreIdle = idle
		} else {
			coresIdle = append(
				coresIdle,
				model.CpuSnapUnit{
					Idle:   idle,
					Cycles: coreCycles,
				},
			)
		}
	}
	return allCoreCycles, allCoreIdle, coresIdle, nil
}

func readTotalCpuUsage(reader FsReaderInterface, lastSnap *model.CpuSnap) (allCoresUsage float64, coresUsage []float64) {
	nowAllCoreCycles, nowAllCoreIdle, nowCoresStatus, _ := readCpuIdle(reader)

	// 总占用
	allCoresUsage = utils.CalculateCpuUsage(
		nowAllCoreCycles,
		lastSnap.AllCycles,
		nowAllCoreIdle,
		lastSnap.AllCoreIdle,
	)

	// 每核心占用
	for index, nowCore := range nowCoresStatus {
		if index >= len(lastSnap.Cores) {
			coresUsage = append(coresUsage, -1)
			continue
		}
		coreUsage := utils.CalculateCpuUsage(
			nowCore.Cycles,
			lastSnap.Cores[index].Cycles,
			nowCore.Idle,
			lastSnap.Cores[index].Idle,
		)
		coresUsage = append(coresUsage, coreUsage)
	}

	lastSnap.AllCycles = nowAllCoreCycles
	lastSnap.AllCoreIdle = nowAllCoreIdle
	lastSnap.Cores = nowCoresStatus
	return allCoresUsage, coresUsage
}

// example output : 2d 14h 7m 36s
func readSystemUptime(reader FsReaderInterface) string {
	raw, _ := reader.ReadFile(procPaths.SystemUptime())
	floatTime, _ := strconv.ParseFloat(strings.Fields(raw)[0], 64)
	second := int(floatTime)
	day := int(second) / 86400
	second %= 86400
	hour := int(second) / 3600
	second %= 3600
	minute := int(second) / 60
	s := int(second) % 60

	// 省略为 0 的段
	builder := strings.Builder{}
	if day > 0 {
		builder.WriteString(strconv.Itoa(day) + "d ")
	}
	if hour > 0 {
		builder.WriteString(strconv.Itoa(hour) + "h ")
	}
	if minute > 0 {
		builder.WriteString(strconv.Itoa(minute) + "m ")
	}
	builder.WriteString(strconv.Itoa(s) + "s")
	return builder.String()
}

func readKernelVersion(runner CommandRunnerInterface) string {
	version, err := runner.Run("uname", "-r")
	if err != nil {
		return model.StringDefault
	}

	return strings.TrimSpace(version)
}
func readSystemArch(runner CommandRunnerInterface) string {
	arch, err := runner.Run("uname", "-m")
	if err != nil {
		return model.StringDefault
	}
	return strings.TrimSpace(arch)
}

// such as : "Asia/Shanghai" , default is "UTC"
func readLocalTimeZone(reader FsReaderInterface, runner CommandRunnerInterface) string {
	result := model.StringDefault

	// 1. systemd timedatectl (not openwrt)
	timeZone, err := runner.Run("timedatectl", "show", "-p", "Timezone", "--value")
	if err == nil {
		return strings.TrimSpace(timeZone)
	}

	// 2. OpenWrt config "option zonename"
	raw, err := reader.ReadFile(procPaths.SystemConfig())
	if err != nil {
		return result
	}

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "option zonename") {
			continue
		}
		fields := strings.Split(line, " ")
		if len(fields) < 2 {
			return result
		}
		result = strings.TrimSpace(fields[2])
		result = strings.Trim(result, "'")
	}
	return result
}

func parseNetworkConnectionLine(line string) (*rawConn, *rawConn) {
	fields := strings.Fields(line)
	if len(fields) < 6 {
		return nil, nil
	}
	origin := rawConn{
		ipFamily: fields[model.NetConnectionIndexIpFamily],
		protocol: fields[model.NetConnectionIndexProto],
		kv:       make(map[string]string, len(fields)),
	}
	reply := rawConn{
		ipFamily: fields[model.NetConnectionIndexIpFamily],
		protocol: fields[model.NetConnectionIndexProto],
		kv:       make(map[string]string, len(fields)),
	}

	// example :
	// ipv4     2 udp      17 54 src=192.168.0.249 dst=20.189.79.72 sport=123 dport=123 packets=1 bytes=76 src=20.189.79.72 dst=163.142.193.23 sport=123 dport=123 packets=1 bytes=76 mark=0 zone=0 use=2
	// 0 "ipv4" | 1 "2" | 2 "udp" | 3 "17" | 4 "54" | 5 "src=192.168.0.249" | 6 "dst=20.189.79.72" | 7 "sport=123" | 8 "dport=123" | 9 "packets=1" | 10 "bytes=76" | 11 "src=20.189.79.72" | 12 "dst=163.142.193.23" | 13 "sport=123" | 14 "dport=123" | 15 "packets=1" | 16 "bytes=76" | 17 "mark=0" | 18 "zone=0" | 19 "use=2" |
	// ipv4     2 tcp      6 95 TIME_WAIT src=192.168.0.236 dst=192.168.0.1 sport=55674 dport=5000 packets=6 bytes=426 src=192.168.0.1 dst=192.168.0.236 sport=5000 dport=55674 packets=5 bytes=3007 [ASSURED] mark=0 zone=0 use=2
	// 0 "ipv4" | 1 "2" | 2 "tcp" | 3 "6" | 4 "5" | 5 "TIME_WAIT" | 6 "src=192.168.0.236" | 7 "dst=192.168.0.1" | 8 "sport=42322" | 9 "dport=5000" | 10 "packets=7" | 11 "bytes=478" | 12 "src=192.168.0.1" | 13 "dst=192.168.0.236" | 14 "sport=5000" | 15 "dport=42322" | 16 "packets=5" | 17 "bytes=3007" | 18 "[ASSURED]" | 19 "mark=0" | 20 "zone=0" | 21 "use=2" |
	// 固定头里找 state（TCP 才有）, 固定头在tcp下就前6个元素,udp的话就前5个元素
	if fields[model.NetConnectionIndexState] != "" && !strings.Contains(fields[model.NetConnectionIndexState], "=") {
		origin.state = fields[model.NetConnectionIndexState]
		reply.state = fields[model.NetConnectionIndexState]
	}
	// 剩下全部 key=value
	for _, item := range fields {
		if kv := strings.SplitN(item, "=", 2); len(kv) == 2 {
			key := kv[0]
			value := kv[1]
			_, exist := origin.kv[key]
			if exist {
				reply.kv[key] = value
				continue
			}
			origin.kv[key] = value
		}
	}
	return &origin, &reply
}

func ReadPrivateIpv4Addresses(runner CommandRunnerInterface) (cidrs []string) {
	raw, err := runner.Run("ip", "-o", "addr", "show")
	if err != nil {
		return
	}

	for _, l := range strings.Split(raw, "\n") {
		fields := strings.Fields(l)
		if len(fields) < 4 {
			continue
		}
		var ipv4Cidr string
		// var ipv6Cidr string
		networkDeviceName := fields[1]

		isPrivateNetworkDevice := false
		for _, prefix := range model.InternalNetworkDeviceNamePrefixList {
			if strings.HasPrefix(networkDeviceName, prefix) {
				isPrivateNetworkDevice = true
				break
			}
		}
		if !isPrivateNetworkDevice {
			continue
		}

		switch fields[2] {
		case "inet":
			ipv4Cidr = fields[3]
		// case "inet6":
		// ipv6Cidr = fields[3]
		default:
			continue
		}
		cidrs = append(cidrs, ipv4Cidr)

	}
	return cidrs

}

// "result" must be not nil
func readNetworkInterfaceIpAddress(runner CommandRunnerInterface, result model.StaticNetworkMetric) {

	if result == nil {
		return
	}

	// IPv4 从 ip addr 简析（无 ip 命令就读 /proc/net/dev 无地址，可接受）
	raw, err := runner.Run("ip", "-o", "addr", "show")
	if err != nil {
		return
	}

	// example :
	// 16: br-lan    inet 192.168.0.1/24 brd 192.168.0.255 scope global br-lan\       valid_lft forever preferred_lft forever
	// 0   1         2    3              4   5             6     7      8             9         10      11            12

	// 16: br-lan    inet6 xxxx:xxxx:xxxx:xxxx::1/64 scope global dynamic noprefixroute \       valid_lft 3028sec preferred_lft 3028sec
	// 0   1         2     3                         4     5      6       7             8       9         10      11            12

	// 16: br-lan    inet6 fe80::8409:9bff:fe6b:79ca/64 scope link \       valid_lft forever preferred_lft forever
	// 0   1         2     3                            4     5    6       7         8       9             10

	for _, l := range strings.Split(raw, "\n") {
		fields := strings.Fields(l)
		if len(fields) < 4 {
			continue
		}
		var ipv4, ipv6 string
		networkDeviceName := fields[1]
		switch fields[2] {
		case "inet":
			ipv4 = utils.TrimSubnetMask(fields[3])
		case "inet6":
			ipv6 = utils.TrimSubnetMask(fields[3])
		default:
			continue
		}
		interfaceInfo, exists := result[networkDeviceName]
		if !exists {
			interfaceInfo = model.StaticNetworkInterfaceMetric{}
			result[networkDeviceName] = interfaceInfo
		}
		if ipv4 != "" {
			interfaceInfo.Ipv4 = append(interfaceInfo.Ipv4, ipv4)
		}
		if ipv6 != "" {
			interfaceInfo.Ipv6 = append(interfaceInfo.Ipv6, ipv6)
		}
		result[networkDeviceName] = interfaceInfo
	}
}

func readDns(reader FsReaderInterface) []string {
	dns := []string{}
	raw, err := reader.ReadFile(procPaths.DefaultDns())
	if err != nil {
		return dns
	}
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "nameserver") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// example : "nameserver 8.8.8.8"
		dns = append(dns, fields[1])
	}
	return dns
}

func readDefaultGateway(reader FsReaderInterface) string {
	raw, err := reader.ReadFile(procPaths.DefaultGateway())
	if err != nil {
		return model.StringDefault
	}
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		// Destination == 00000000 且 Flags 含 0003
		if fields[1] == "00000000" && strings.Contains(fields[3], "003") {
			// Gateway 是小端十六进制
			hexIP := fields[2]
			if len(hexIP) != 8 {
				continue
			}
			// 反转字节序
			u, _ := strconv.ParseUint(hexIP, 16, 32)
			ip := make([]byte, 4)
			binary.LittleEndian.PutUint32(ip, uint32(u))
			return fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3])
		}
	}
	return model.StringDefault
}

func readDiskUsage(reader FsReaderInterface, metric model.StorageMetric) {
	raw, err := reader.ReadFile(procPaths.StorageDeviceMounts())
	if err != nil {
		return
	}

	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		deviceName := fields[0] // 设备名，如 /dev/mmcblk2p2 或 overlay
		mountPoint := fields[1]
		fsType := fields[2]

		// 过滤掉虚拟文件系统和不需要的
		if strings.HasPrefix(deviceName, "none") ||
			fsType == "proc" ||
			fsType == "sysfs" ||
			fsType == "devtmpfs" ||
			fsType == "tmpfs" ||
			fsType == "cgroup" ||
			fsType == "debugfs" {
			continue
		}

		// OpenWrt 特例：overlayfs 通常基于 /dev/...，但在 /proc/mounts 里可能是 "overlay"
		// 我们可以暂时忽略 overlay 的统计，或者通过 cat /proc/mounts 找到其下层的设备
		// 这里为了通用性，如果 devSource 看起来是个设备路径（以 /dev 开头），我们就统计它
		if !strings.HasPrefix(deviceName, "/dev/") {
			// 如果是 /dev/root 这种软链接情况，也可以处理，这里简化为只处理绝对路径
			// 实际上 OpenWrt 的 rootfs_data 通常在 /dev/mmcblk2p2 之类
			continue
		}

		deviceName = strings.TrimPrefix(deviceName, "/dev/")

		stat, err := getStatfs(mountPoint)
		if err != nil {
			continue
		}

		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bfree * uint64(stat.Bsize)
		used := total - free

		if total == 0 {
			continue
		}

		convertTotal, totalUnit := utils.ConvertBytes(float64(total), model.Byte)
		convertUsed, usedUnit := utils.ConvertBytes(float64(used), model.Byte)
		percent := float64(used) / float64(total) * 100

		// deviceMetric, exist := metric[deviceName]
		// if !exist {
		// 	continue
		// }

		metric[deviceName] = model.StorageIoMetric{
			Read: model.MetricUnit{
				Value: -1,
				Unit:  "",
			},
			Write: model.MetricUnit{
				Value: -1,
				Unit:  "",
			},
			Total: model.MetricUnit{
				Value: convertTotal,
				Unit:  totalUnit,
			},
			Used: model.MetricUnit{
				Value: convertUsed,
				Unit:  usedUnit,
			},
			UsedPercent: model.MetricUnit{
				Value: percent,
				Unit:  model.Percent,
			},
		}
	}
}

func readDiskIoStats(reader FsReaderInterface, metric model.StorageMetric, lastSnap model.DiskSnap, updateInterval uint) {

	raw, err := reader.ReadFile(procPaths.StorageDeviceIo())
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		deviceName := fields[2]
		if strings.HasPrefix(deviceName, "loop") ||
			strings.HasPrefix(deviceName, "ram") ||
			strings.HasPrefix(deviceName, "nbd") ||
			strings.HasPrefix(deviceName, "zram") {
			continue
		}

		readBytesNow, _ := strconv.ParseFloat(fields[5], 64)
		writeBytesNow, _ := strconv.ParseFloat(fields[9], 64)

		readRate := utils.CalculateRate(readBytesNow, lastSnap[deviceName].ReadBytes, updateInterval)
		writeRate := utils.CalculateRate(writeBytesNow, lastSnap[deviceName].WriteBytes, updateInterval)

		readRate, readDeltaUnit := utils.ConvertBytes(readRate, model.BSecond)
		writeRate, WriteDeltaUnit := utils.ConvertBytes(writeRate, model.BSecond)

		deviceMetric, exist := metric[deviceName]
		if !exist {
			continue
		}

		deviceMetric.Read = model.MetricUnit{
			Value: readRate,
			Unit:  readDeltaUnit,
		}
		deviceMetric.Write = model.MetricUnit{
			Value: writeRate,
			Unit:  WriteDeltaUnit,
		}
		metric[deviceName] = deviceMetric

		lastSnap[deviceName] = model.DiskSnapUnit{
			ReadBytes:  readBytesNow,
			WriteBytes: writeBytesNow,
		}
	}
}

func ReadNetworkMetric(reader FsReaderInterface, lastSnap *model.NetSnap, updateInterval uint) model.NetworkMetric {
	if lastSnap == nil {
		panic("ReadNetworkMetric lastSnap is nil")
	}

	result := model.NetworkMetric{}

	data, _ := reader.ReadFile(procPaths.NetworkDeviceIo())

	totalRxRateNow := 0.0
	totalTxRateNow := 0.0

	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.Fields(line)
		interfaceName := strings.TrimSuffix(parts[0], ":")
		if interfaceName == "lo" ||
			strings.HasPrefix(interfaceName, "loopback") {
			continue
		}

		rxNow, _ := strconv.ParseFloat(parts[1], 64)
		txNow, _ := strconv.ParseFloat(parts[9], 64)

		if _, exist := lastSnap.Interfaces[interfaceName]; !exist {
			lastSnap.Interfaces[interfaceName] = model.NetSnapUnit{}
		}
		lastUnit := lastSnap.Interfaces[interfaceName]

		rxRate := utils.CalculateRate(rxNow, lastUnit.RxBytes, updateInterval)
		txRate := utils.CalculateRate(txNow, lastUnit.TxBytes, updateInterval)
		totalRxRateNow += rxRate
		totalTxRateNow += txRate

		lastSnap.Interfaces[interfaceName] = model.NetSnapUnit{
			RxBytes: rxNow,
			TxBytes: txNow,
		}

		rxRate, rxUnit := utils.ConvertBytes(rxRate, model.BSecond)
		txRate, txUnit := utils.ConvertBytes(txRate, model.BSecond)

		result[interfaceName] = model.NetworkIoMetric{
			Incoming: model.MetricUnit{Value: rxRate, Unit: rxUnit},
			Outgoing: model.MetricUnit{Value: txRate, Unit: txUnit},
		}
	}

	totalRxRate, totalRxUnit := utils.ConvertBytes(totalRxRateNow, model.BSecond)
	totalTxRate, totalTxUnit := utils.ConvertBytes(totalTxRateNow, model.BSecond)

	result.SetTotal(
		totalRxRate, totalRxUnit,
		totalTxRate, totalTxUnit,
	)

	return result
}

func ReadCpuMetric(reader FsReaderInterface, lastSnap *model.CpuSnap) model.CpuMetric {
	if lastSnap == nil {
		panic("ReadCpuMetric lastSnap is nil")
	}
	nowMetric := model.CpuMetric{}

	totalUsage, coresUsage := readTotalCpuUsage(reader, lastSnap)
	temperature, temperatureUnit := readCpuTemperature(reader)
	nowMetric.SetTotal(
		totalUsage, model.Percent, temperature, temperatureUnit,
	)

	for index, usage := range coresUsage {
		nowMetric["cpu"+strconv.Itoa(index)] = model.CpuUsageMetric{
			Usage: model.MetricUnit{
				Value: usage,
				Unit:  model.Percent,
			},
			Temperature: model.MetricUnit{
				Value: temperature,
				Unit:  temperatureUnit,
			},
		}
	}
	return nowMetric
}

func ReadMemoryMetric(reader FsReaderInterface) model.MemoryMetric {
	result := model.MemoryMetric{}

	raw, _ := reader.ReadFile(procPaths.SystemMemoryInfo())
	var total, avail, free uint64
	for _, l := range strings.Split(raw, "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		kB, _ := strconv.ParseUint(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			total = kB
		case "MemAvailable:":
			avail = kB
		case "MemFree:":
			if avail == 0 { // 老内核没有 Available
				free = kB
			}
		}
	}
	if avail == 0 {
		avail = free
	}

	used := total - avail
	usedPercent := float64(used) * 100 / float64(total)
	result.UsedPercent = model.MetricUnit{
		Value: usedPercent,
		Unit:  model.Percent,
	}

	convertTotal, unit := utils.ConvertBytes(float64(total), model.KiloByte)
	result.Total = model.MetricUnit{
		Value: convertTotal,
		Unit:  unit,
	}

	convertUsed, unit := utils.ConvertBytes(float64(used), model.KiloByte)
	result.Used = model.MetricUnit{
		Value: convertUsed,
		Unit:  unit,
	}

	return result
}

func ReadSystemMetric(reader FsReaderInterface) model.SystemMetric {
	result := model.SystemMetric{
		Uptime: readSystemUptime(reader),
	}
	return result
}

func selectPrivateAddress(
	originConnectionSrcAddr string,
	replyConnectionSrcAddr string,
	originConnectionSrcPort int,
	replyConnectionSrcPort int,
	originConnectionDstAddr string,
	replyConnectionDstAddr string,
	originConnectionDstPort int,
	replyConnectionDstPort int,

	privateCidr []string) (srcAddr string, srcPort int, dstAddr string, dstPort int) {
	isOriginSrcAddrInSubnets := utils.IsIpInSubnets(originConnectionSrcAddr, privateCidr)
	isOriginDstAddrInSubnets := utils.IsIpInSubnets(originConnectionDstAddr, privateCidr)
	isReplySrcAddrInSubnets := utils.IsIpInSubnets(replyConnectionSrcAddr, privateCidr)
	isReplyDstAddrInSubnets := utils.IsIpInSubnets(replyConnectionDstAddr, privateCidr)

	if utils.All([]bool{isOriginDstAddrInSubnets, isOriginSrcAddrInSubnets, isReplyDstAddrInSubnets, isReplySrcAddrInSubnets}) {
		return originConnectionSrcAddr, originConnectionSrcPort, originConnectionDstAddr, originConnectionDstPort
	}

	if isOriginSrcAddrInSubnets || isOriginDstAddrInSubnets {
		return originConnectionSrcAddr, originConnectionSrcPort, originConnectionDstAddr, originConnectionDstPort
	}

	if isReplySrcAddrInSubnets || isReplyDstAddrInSubnets {
		return replyConnectionDstAddr, replyConnectionDstPort, replyConnectionSrcAddr, replyConnectionSrcPort
	}

	return originConnectionSrcAddr, originConnectionSrcPort, originConnectionDstAddr, originConnectionDstPort

}

func ReadConnectionMetric(reader FsReaderInterface, metric *model.NetworkConnectionMetric, privateCidr []string) {
	var lines []string

	for _, path := range procPaths.NetworkConnection() {
		b, err := reader.ReadFile(path)
		if err != nil {
			continue
		}
		lines = strings.Split(string(b), "\n")
		break
	}

	var result []model.NetworkConnection
	for _, line := range lines {
		if line == "" {
			continue
		}
		originConnection, replyConnection := parseNetworkConnectionLine(line)
		if originConnection == nil {
			continue
		}

		// always originConnection.protocol == replyConnection.protocol
		// so count once
		switch originConnection.protocol {
		case "tcp":
			metric.Counts.AddCountTcp()
		case "udp":
			metric.Counts.AddCountUdp()
		default:
			metric.Counts.AddCountOther()
		}

		// if orig.src in RFC1918:
		// 	show (orig.src, orig.sport, orig.dst, orig.dport)   # 内网视角
		// else:
		// 	show (reply.dst, reply.dport, reply.src, reply.sport)   # 公网视角

		var sourceIp, destinationIp string
		var sourcePort, destinationPort int

		if originConnection.ipFamily == "ipv4" {
			// ipv4 has NAT
			sourceIp, sourcePort, destinationIp, destinationPort = selectPrivateAddress(
				originConnection.kv["src"],
				replyConnection.kv["src"],
				utils.TryInt(originConnection.kv["sport"]),
				utils.TryInt(replyConnection.kv["sport"]),
				originConnection.kv["dst"],
				replyConnection.kv["dst"],
				utils.TryInt(originConnection.kv["dport"]),
				utils.TryInt(replyConnection.kv["dport"]),
				privateCidr,
			)
		} else {
			// ipv6 has not NAT
			sourceIp = originConnection.kv["src"]
			destinationIp = originConnection.kv["dst"]
			sourcePort = utils.TryInt(originConnection.kv["sport"])
			destinationPort = utils.TryInt(originConnection.kv["dport"])
		}

		traffic := utils.TryFloat64(originConnection.kv["bytes"])
		replyTraffic := utils.TryFloat64(replyConnection.kv["bytes"])

		if replyTraffic > 0 {
			traffic += replyTraffic
		}
		traffic, unit := utils.ConvertBytes(traffic, model.Byte)

		originPackets := utils.TryInt64(originConnection.kv["packets"])
		replyPackets := utils.TryInt64(replyConnection.kv["packets"])
		packets := originPackets

		if replyPackets > 0 {
			packets += replyPackets
		}

		if originConnection.ipFamily == "ipv6" {
			sourceIp_, err := netip.ParseAddr(sourceIp)
			if err != nil {
				log.Printf("Parsing ip address %q failed: %s", sourceIp, err)
			} else {
				sourceIp = sourceIp_.String()
			}

			destinationIp_, err := netip.ParseAddr(destinationIp)
			if err != nil {
				log.Printf("Parsing ip address %q failed: %s", destinationIp, err)
			} else {
				destinationIp = destinationIp_.String()
			}
		}

		result = append(
			result,
			model.NetworkConnection{
				IpFamily:        originConnection.ipFamily,
				SourceIp:        sourceIp,
				SourcePort:      sourcePort,
				DestinationIp:   destinationIp,
				DestinationPort: destinationPort,
				Protocol:        originConnection.protocol,
				State:           originConnection.state,
				Traffic: model.MetricUnit{
					Value: traffic,
					Unit:  unit,
				},
				Packets: packets,
			},
		)
	}
	metric.Details = append(metric.Details, result...)
}

func ReadStaticSystemMetric(reader FsReaderInterface, runner CommandRunnerInterface) model.StaticSystemMetric {

	os, err := reader.ReadFile(procPaths.SystemVersion())
	if err != nil {
		os = model.StringDefault
	}
	os = strings.TrimSpace(os)

	deviceName, err := reader.ReadFile(procPaths.HardwareName())
	if err != nil {
		deviceName = model.StringDefault
	}
	deviceName = strings.TrimSpace(deviceName)
	deviceName = strings.Trim(deviceName, "\u0000")

	hostname, err := reader.ReadFile(procPaths.SystemHostname())
	if err != nil {
		hostname = model.StringDefault
	}
	hostname = strings.TrimSpace(hostname)

	kernelVersion := readKernelVersion(runner)
	arch := readSystemArch(runner)
	timezone := readLocalTimeZone(reader, runner)

	result := model.StaticSystemMetric{
		Arch:       arch,
		DeviceName: deviceName,
		Hostname:   hostname,
		Kernel:     kernelVersion,
		Os:         os,
		Timezone:   timezone,
	}
	return result
}

func ReadStaticNetworkMetric(reader FsReaderInterface, runner CommandRunnerInterface) model.StaticNetworkMetric {
	result := model.StaticNetworkMetric{}

	readNetworkInterfaceIpAddress(runner, result)

	wanIpv4 := []string{model.StringDefault}
	wanIpv6 := []string{model.StringDefault}
	wanIp, exist := result["pppoe-wan"]
	if exist {
		wanIpv4 = wanIp.Ipv4
		wanIpv6 = wanIp.Ipv6
	}

	result.SetGlobal(
		wanIpv4, wanIpv6,
		readDns(reader), readDefaultGateway(reader),
	)

	return result
}

func ReadStorageMetric(reader FsReaderInterface, lastSnap model.DiskSnap, updateInterval uint) model.StorageMetric {
	metric := model.StorageMetric{}
	// only show have storage usage device
	readDiskUsage(reader, metric)
	readDiskIoStats(reader, metric, lastSnap, updateInterval)
	return metric
}
