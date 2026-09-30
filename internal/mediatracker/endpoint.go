package mediatracker

import "net/netip"

type binaryEndpointKey struct {
	fallbackIP string
	ipv4       [4]byte
	port       uint16
	isIPv4     bool
}

func newBinaryEndpointKey(ip string, port uint16) binaryEndpointKey {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is4() {
		return binaryEndpointKey{fallbackIP: ip, port: port}
	}
	return binaryEndpointKey{ipv4: addr.As4(), port: port, isIPv4: true}
}

func (k binaryEndpointKey) ipString() string {
	if !k.isIPv4 {
		return k.fallbackIP
	}
	return netip.AddrFrom4(k.ipv4).String()
}

func (k binaryEndpointKey) sameIP(other binaryEndpointKey) bool {
	if k.isIPv4 != other.isIPv4 {
		return false
	}
	if k.isIPv4 {
		return k.ipv4 == other.ipv4
	}
	return k.fallbackIP == other.fallbackIP
}
