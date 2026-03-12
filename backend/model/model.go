package model

import "time"

const MinServiceRunDuration = 35 * time.Second

const (
	StringDefault              = "unknown"
	NetConnectionIndexIpFamily = 0 // ipv4/ipv6
	NetConnectionIndexProto    = 2 // tcp/udp/icmp
	NetConnectionIndexState    = 5 // 只有 TCP 有
)

type IpFamilyType string

const (
	IpFamilyTypeIpv4 IpFamilyType = "ipv4"
	IpFamilyTypeIpv6 IpFamilyType = "ipv6"
)

type IpAddressType string

const (
	IpAddressTypeLan     IpAddressType = "lan"
	IpAddressTypeWan     IpAddressType = "wan"
	IpAddressTypeUnknown IpAddressType = "unknown"
)

const (
	ProtoICMP     uint8 = 1   // Internet Control Message (IPv4 Ping)
	ProtoIGMP     uint8 = 2   // Internet Group Management (组播管理)
	ProtoTCP      uint8 = 6   // Transmission Control Protocol
	ProtoUDP      uint8 = 17  // User Datagram Protocol
	ProtoIPv6ICMP uint8 = 58  // ICMP for IPv6 (IPv6 Ping/邻居发现)
	ProtoGRE      uint8 = 47  // General Routing Encapsulation (虚拟链路/VPN)
	ProtoESP      uint8 = 50  // Encap Security Payload (IPsec 加密数据)
	ProtoAH       uint8 = 51  // Authentication Header (IPsec 认证)
	ProtoOSPF     uint8 = 89  // OSPF 路由协议流量
	ProtoSCTP     uint8 = 132 // Stream Control Transmission Protocol
)

const (
	BSecond  = "B/S"
	KbSecond = "KB/S"
	MbSecond = "MB/S"
	GbSecond = "GB/S"
	TbSecond = "TB/S"
	PbSecond = "PB/S"
	Byte     = "B"
	KiloByte = "KB"
	MegaByte = "MB"
	GigaByte = "GB"
	TeraByte = "TB"
	PetaByte = "PB"
	Percent  = "%"
	Celsius  = "°C"
)

var (
	RateUnitList                        = []string{BSecond, KbSecond, MbSecond, GbSecond, TbSecond, PbSecond}
	DataUnitList                        = []string{Byte, KiloByte, MegaByte, GigaByte, TeraByte, PetaByte}
	InternalNetworkDeviceNamePrefixList = []string{"br-lan", "docker", "tun"}
	JsonCacheKeyStaticMetric            = "StaticMetric"
	JsonCacheKeyDynamicMetric           = "DynamicMetric"
	JsonCacheKeyNetworkConnectionMetric = "NetworkConnectionMetric"
	JsonCacheKeyAggregationTraffic      = "AggregationTraffic"
)

type CacheValue struct {
	UpdateAt time.Time
	ExpireAt time.Time
	Data     []byte
}

type CacheStringValue struct {
	UpdateAt time.Time
	ExpireAt time.Time
	Data     string
}
type CacheDnsValue struct {
	ExpireAt time.Time
	Data     []string
}

type NetSnapUnit struct {
	RxBytes float64
	TxBytes float64
}
type NetSnap struct {
	Interfaces map[string]NetSnapUnit
}

type CpuSnap struct {
	AllCycles   uint64        // 所有核心时间片总和("cpu"一行)
	AllCoreIdle uint64        // 所有核心idle总和("cpu"一行)
	Cores       []CpuSnapUnit // 各核心时间片总和和idle("cpu0","cpu1"等行)
}
type CpuSnapUnit struct {
	Cycles uint64
	Idle   uint64
}

type DiskSnap map[string]DiskSnapUnit
type DiskSnapUnit struct{ ReadBytes, WriteBytes float64 }

type DynamicMetric struct {
	Storage StorageMetric `json:"storage"`
	Cpu     CpuMetric     `json:"cpu"`
	Network NetworkMetric `json:"network"`
	Memory  MemoryMetric  `json:"memory"`
	System  SystemMetric  `json:"system"`
}

type NetworkConnectionMetric struct {
	Counts  NetworkConnectionCounts `json:"counts"`
	Details []NetworkConnection     `json:"connections"`
}

type NetworkConnectionCounts struct {
	Tcp   uint `json:"tcp"`
	Udp   uint `json:"udp"`
	Other uint `json:"other"`
}

func (n *NetworkConnectionCounts) AddCountTcp() {
	n.Tcp += 1
}

func (n *NetworkConnectionCounts) AddCountUdp() {
	n.Udp += 1
}

func (n *NetworkConnectionCounts) AddCountOther() {
	n.Other += 1
}

// because tcp/udp/icmp counts are counted income and outcome connection ,
// divide them by 2 is true connection counts
func (n *NetworkConnectionCounts) DivideAllCounts() {
	if n.Tcp > 0 {
		n.Tcp /= 2
	}
	if n.Udp > 0 {
		n.Udp /= 2
	}
	if n.Other > 0 {
		n.Other /= 2
	}
}

type NetworkConnection struct {
	IpFamily        string     `json:"ip_family"`
	SourceIp        string     `json:"source_ip"`
	SourcePort      int        `json:"source_port"`
	DestinationIp   string     `json:"destination_ip"`
	DestinationPort int        `json:"destination_port"`
	Protocol        string     `json:"protocol"`
	State           string     `json:"state"`
	Traffic         MetricUnit `json:"traffic"`
	Packets         int64      `json:"packets"`
}

type StorageMetric map[string]StorageIoMetric

type StorageIoMetric struct {
	Read  MetricUnit `json:"read"`
	Write MetricUnit `json:"write"`
	// if not read storage device usage , fill -1
	Total MetricUnit `json:"total,omitempty"`
	// if not read storage device usage , fill -1
	Used MetricUnit `json:"used,omitempty"`
	// if not read storage device usage , fill -1
	UsedPercent MetricUnit `json:"used_percent,omitempty"`
}

func (s StorageMetric) SetTotal(read float64, readUnit string, write float64, writeUnit string) {
	s["total"] = StorageIoMetric{
		Read:  MetricUnit{read, readUnit},
		Write: MetricUnit{write, writeUnit},
	}
}

type CpuMetric map[string]CpuUsageMetric

type CpuUsageMetric struct {
	Usage       MetricUnit `json:"usage"`
	Temperature MetricUnit `json:"temperature"`
}

func (c CpuMetric) SetTotal(usage float64, usageUnit string, temperature float64, temperatureUnit string) {
	c["total"] = CpuUsageMetric{
		Usage:       MetricUnit{usage, usageUnit},
		Temperature: MetricUnit{temperature, temperatureUnit},
	}
}

type NetworkMetric map[string]NetworkIoMetric

type NetworkIoMetric struct {
	Incoming MetricUnit `json:"incoming"`
	Outgoing MetricUnit `json:"outgoing"`
}

func (c NetworkMetric) SetTotal(incoming float64, incomingUnit string, outgoing float64, outgoingUnit string) {
	c["total"] = NetworkIoMetric{
		Incoming: MetricUnit{incoming, incomingUnit},
		Outgoing: MetricUnit{outgoing, outgoingUnit},
	}
}

type MemoryMetric struct {
	Total       MetricUnit `json:"total"`
	Used        MetricUnit `json:"used"`
	UsedPercent MetricUnit `json:"used_percent"`
}

type SystemMetric struct {
	Uptime string `json:"uptime"`
}

type StaticMetric struct {
	Network StaticNetworkMetric `json:"network"`
	System  StaticSystemMetric  `json:"system"`
}

type StaticNetworkMetric map[string]StaticNetworkInterfaceMetric

type StaticNetworkInterfaceMetric struct {
	Ipv4    []string `json:"ipv4"`
	Ipv6    []string `json:"ipv6"`
	Dns     []string `json:"dns,omitempty"`
	Gateway string   `json:"gateway,omitempty"`
}

func (s StaticNetworkMetric) SetGlobal(Ipv4 []string, Ipv6 []string, dns []string, gateway string) {
	s["global"] = StaticNetworkInterfaceMetric{
		Ipv4:    Ipv4,
		Ipv6:    Ipv6,
		Dns:     dns,
		Gateway: gateway,
	}
}

type StaticSystemMetric struct {
	Hostname   string `json:"hostname"`
	Kernel     string `json:"kernel"`
	Os         string `json:"os"`
	DeviceName string `json:"device_name"`
	Arch       string `json:"arch"`
	Timezone   string `json:"timezone"`
}

type AggregationTrafficMetric struct {
	CaptureStartAt   time.Time                   `json:"capture_start_at"`
	CaptureInterface string                      `json:"capture_interface"`
	Details          []AggregationTrafficDetails `json:"details"`
}

type AggregationTrafficDetails struct {
	Ip              string        `json:"ip"`
	IpType          IpAddressType `json:"ip_type"`
	IpFamily        IpFamilyType  `json:"ip_family"`
	Incoming        MetricUnit    `json:"incoming"`
	Outgoing        MetricUnit    `json:"outgoing"`
	TotalThroughput MetricUnit    `json:"total_throughput"`
	TotalIncoming   MetricUnit    `json:"total_incoming"`
	TotalOutgoing   MetricUnit    `json:"total_outgoing"`
	TotalTraffic    MetricUnit    `json:"total_traffic"`
	Tcp             int32         `json:"tcp"`
	Udp             int32         `json:"udp"`
	Other           int32         `json:"other"` // 指的是"当前时刻此ip的非tcp/udp连接数"
}

type MetricUnit struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type DnsResult map[string][]string
