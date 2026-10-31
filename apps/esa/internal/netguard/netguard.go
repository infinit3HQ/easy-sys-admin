// Package netguard decides which addresses esa may connect to on behalf of
// a user: SSH hosts, certificate checks, and notification endpoints.
//
// Some addresses are always refused (cloud metadata, link-local, multicast).
// Private and loopback addresses are refused unless allowPrivate is set,
// which self-hosted installs on a LAN need.
package netguard

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
)

// BlockedError means the address is not allowed by the policy.
type BlockedError struct {
	IP netip.Addr
	// Private is true when ESA_ALLOW_PRIVATE_NETWORKS would allow it.
	Private bool
}

func (e *BlockedError) Error() string {
	if e.Private {
		return "esa does not connect to " + e.IP.String() + ": private and internal addresses are blocked (ESA_ALLOW_PRIVATE_NETWORKS)"
	}
	return "esa does not connect to " + e.IP.String() + ": link-local, metadata, multicast, and unspecified addresses are always blocked"
}

// IsBlocked reports whether err comes from the address policy.
func IsBlocked(err error) bool {
	var be *BlockedError
	return errors.As(err, &be)
}

var (
	thisNetwork = netip.MustParsePrefix("0.0.0.0/8")
	cgnat       = netip.MustParsePrefix("100.64.0.0/10")
	nat64       = netip.MustParsePrefix("64:ff9b::/96")
	alwaysAddrs = []netip.Addr{
		netip.MustParseAddr("169.254.169.254"), // covered by link-local; listed for clarity
		netip.MustParseAddr("fd00:ec2::254"),   // AWS IMDS over IPv6
		netip.MustParseAddr("100.100.100.200"), // Alibaba Cloud metadata
		netip.MustParseAddr("255.255.255.255"),
	}
)

// Check returns a *BlockedError when esa must not connect to ip.
func Check(ip netip.Addr, allowPrivate bool) error {
	ip = ip.Unmap()
	if !ip.IsValid() {
		return errors.New("esa does not connect to an invalid address")
	}
	if nat64.Contains(ip) {
		// NAT64 reaches the embedded IPv4 address; judge that one.
		b := ip.As16()
		if err := Check(netip.AddrFrom4([4]byte(b[12:])), allowPrivate); err != nil {
			var be *BlockedError
			errors.As(err, &be)
			return &BlockedError{IP: ip, Private: be != nil && be.Private}
		}
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || thisNetwork.Contains(ip) {
		return &BlockedError{IP: ip}
	}
	for _, a := range alwaysAddrs {
		if ip == a {
			return &BlockedError{IP: ip}
		}
	}
	if allowPrivate {
		return nil
	}
	if ip.IsLoopback() || ip.IsPrivate() || cgnat.Contains(ip) || !ip.IsGlobalUnicast() {
		return &BlockedError{IP: ip, Private: true}
	}
	return nil
}

// CheckIP is Check for a net.IP.
func CheckIP(ip net.IP, allowPrivate bool) error {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return errors.New("esa does not connect to an invalid address")
	}
	return Check(a, allowPrivate)
}

// Control is a net.Dialer Control function. It runs after DNS resolution,
// for every address actually dialled, so a name that resolves (or later
// rebinds) to a blocked address is refused.
func Control(allowPrivate bool) func(network, address string, c syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("netguard: %w", err)
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("netguard: %q is not an IP address", host)
		}
		return Check(ip, allowPrivate)
	}
}

// Dialer returns a net.Dialer that applies the policy.
func Dialer(allowPrivate bool, d net.Dialer) *net.Dialer {
	d.Control = Control(allowPrivate)
	return &d
}
