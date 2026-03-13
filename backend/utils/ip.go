package utils

import (
	"net"
)

// 判断是否为私网地址
func IsIpInSubnet(ip string, cidr string) bool {
	ipObj := net.ParseIP(ip)
	_, subnet, _ := net.ParseCIDR(cidr)
	return subnet.Contains(ipObj)
}

func IsIpInSubnets(ip string, cidrs []string) bool {
	ipObj := net.ParseIP(ip)
	for _, cidr := range cidrs {
		_, subnet, _ := net.ParseCIDR(cidr)
		if subnet.Contains(ipObj) {
			return true
		}
	}
	return false
}
