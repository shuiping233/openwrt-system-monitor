package utils

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/netip"
	"openwrt-diskio-api/backend/model"
	"slices"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
)

// If "unit" is unknown unit , return unchanged.
func ConvertBytes(bytes float64, unit string) (float64, string) {
	unit = TrimBytesUnit(unit)

	unitList := model.RateUnitList
	if FindIndex(model.DataUnitList, unit) != -1 {
		unitList = model.DataUnitList
	}

	unitListIndex := FindIndex(unitList, unit)
	if unitListIndex < 0 {
		return bytes, unit
	}
	newBytes := bytes / 1024
	if newBytes < 1 {
		return bytes, unit
	}
	newUnitListIndex := unitListIndex + 1
	if newUnitListIndex >= len(unitList) {
		return bytes, unit
	}
	return ConvertBytes(newBytes, unitList[newUnitListIndex])
}

func TrimBytesUnit(unit string) string {
	return strings.ToUpper(strings.TrimSpace(unit))
}

// if not found , return -1 , python list index be like
func FindIndex(list []string, expected string) int {
	if list == nil {
		return -1
	}
	if expected == "" {
		return -1
	}
	for index, value := range list {
		if value == expected {
			return index
		}
	}
	return -1
}

func TrimSubnetMask(cidr string) string {
	if !strings.Contains(cidr, "/") {
		return cidr
	}
	return strings.Split(cidr, "/")[0]
}

// if "interval" == 0 , return -1
func CalculateRate(now float64, last float64, interval uint) (rate float64) {
	if interval == 0 {
		return -1
	}
	delta := now - last
	rate = delta / float64(interval)

	return rate
}

// if err , return 0 , "slice" must be all number and > 0
func SumUint64(slice []string) (uint64, error) {
	if slice == nil {
		return 0, nil
	}
	var sum uint64
	for _, item := range slice {
		number, err := strconv.ParseUint(item, 10, 64)
		if err != nil {
			return 0, err
		}
		sum += number
	}
	return sum, nil
}

func CalculateCpuUsage(nowCpuCycles uint64, lastCpuCycles uint64, nowCpuIdle uint64, lastCpuIdle uint64) (cpuUsage float64) {
	totalDelta := int(nowCpuCycles) - int(lastCpuCycles)

	if totalDelta <= 0 {
		return 0.0
	}
	idleDelta := int(nowCpuIdle) - int(lastCpuIdle)
	if idleDelta <= 0 {
		return 0.0
	}
	cpuUsage = (1.0 - float64(idleDelta)/float64(totalDelta)) * 100

	if cpuUsage < 0 {
		return 0.0
	}

	return cpuUsage
}

func RandHex(length int) string {
	if length <= 0 {
		return ""
	}
	b := make([]byte, (length/2)+1)
	rand.Read(b)
	return hex.EncodeToString(b)[:length]
}

// if err , return -1
func TryInt(input string) int {
	result, err := strconv.Atoi(input)
	if err != nil {
		return -1
	}
	return result
}
func TryInt64(input string) int64 {
	result, err := strconv.ParseInt(input, 10, 64)
	if err != nil {
		return -1
	}
	return result
}
func TryFloat64(input string) float64 {
	result, err := strconv.ParseFloat(input, 64)
	if err != nil {
		return -1
	}
	return result
}

func All(bb []bool) bool {
	return !slices.Contains(bb, false)
}

func Any(bb []bool) bool {
	return slices.Contains(bb, true)
}

// GetInterfaceIpv4Info 获取指定网卡的 IPv4 自身地址和子网前缀
func GetInterfaceIpv4Info(interfaceName string) (addr netip.Addr, prefix netip.Prefix, err error) {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return addr, prefix, fmt.Errorf("interface %q not found: %v", interfaceName, err)
	}

	addrList, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil || len(addrList) == 0 {
		return addr, prefix, fmt.Errorf("interface %q missing ipv4 address", interfaceName)
	}

	// netlink 返回的是 net.IP，转为 netip.Addr
	// addrList[0].IP 是自身 IP，addrList[0].Mask 是掩码
	ip, err := netip.ParseAddr(addrList[0].IP.String())
	if err != nil {
		return addr, prefix, fmt.Errorf("Get invalid ipv4 address from interface %q : %v", interfaceName, err)
	}
	ones, _ := addrList[0].Mask.Size()

	return ip, netip.PrefixFrom(ip, ones).Masked(), nil
}

// GetInterfaceGuaIpv6Info 获取指定网卡的 IPv6 GUA 自身地址和子网前缀 (Prefix)
func GetInterfaceGuaIpv6Info(interfaceName string) (addr netip.Addr, prefix netip.Prefix, err error) {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return addr, prefix, fmt.Errorf("interface %q not found: %v", interfaceName, err)
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_V6)
	if err != nil {
		return addr, prefix, err
	}

	for _, item := range addrs {
		ip, _ := netip.ParseAddr(item.IP.String())
		// 排除链路本地地址 (fe80::)
		if ip.IsLinkLocalUnicast() {
			continue
		}
		// 寻找 GUA 前缀（掩码小于 128 的通常是运营商下发的 PD 前缀段）
		ones, _ := item.Mask.Size()
		if ones < 128 {
			addr = ip
			prefix = netip.PrefixFrom(ip, ones).Masked()
			return addr, prefix, nil
		}
	}

	return addr, prefix, fmt.Errorf("no global ipv6 prefix found on %q", interfaceName)
}
