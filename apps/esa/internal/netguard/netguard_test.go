package netguard

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestCheck(t *testing.T) {
	always := []string{
		"0.0.0.0", "0.1.2.3", "::", "169.254.169.254", "169.254.1.1", "fe80::1", "fd00:ec2::254",
		"100.100.100.200", "255.255.255.255", "224.0.0.1", "ff02::1", "::ffff:169.254.169.254",
		"64:ff9b::a9fe:a9fe",
	}
	privateOnly := []string{
		"127.0.0.1", "::1", "10.0.0.1", "172.16.5.4", "192.168.1.10", "fc00::1", "fd12:3456::1",
		"100.64.0.1", "100.127.255.254", "::ffff:10.0.0.1", "64:ff9b::a00:1",
	}
	public := []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111", "100.63.255.255", "100.128.0.1", "64:ff9b::808:808"}

	for _, s := range always {
		ip := netip.MustParseAddr(s)
		for _, allow := range []bool{true, false} {
			err := Check(ip, allow)
			if err == nil || !IsBlocked(err) {
				t.Errorf("Check(%s, allowPrivate=%v) = %v, want blocked", s, allow, err)
			}
		}
	}
	for _, s := range privateOnly {
		ip := netip.MustParseAddr(s)
		if err := Check(ip, true); err != nil {
			t.Errorf("Check(%s, allowPrivate=true) = %v, want allowed", s, err)
		}
		err := Check(ip, false)
		if err == nil {
			t.Errorf("Check(%s, allowPrivate=false) allowed, want blocked", s)
		} else if !strings.Contains(err.Error(), "ESA_ALLOW_PRIVATE_NETWORKS") {
			t.Errorf("Check(%s) error %q should name the setting", s, err)
		}
	}
	for _, s := range public {
		for _, allow := range []bool{true, false} {
			if err := Check(netip.MustParseAddr(s), allow); err != nil {
				t.Errorf("Check(%s, allowPrivate=%v) = %v, want allowed", s, allow, err)
			}
		}
	}
	if err := CheckIP(net.ParseIP("169.254.169.254"), true); !IsBlocked(err) {
		t.Errorf("CheckIP metadata = %v", err)
	}
}

// The dialer refuses blocked addresses before connecting, including names
// that resolve to them.
func TestDialerRefuses(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	strict := Dialer(false, net.Dialer{Timeout: time.Second})
	for _, host := range []string{"127.0.0.1", "localhost"} {
		if _, err := strict.DialContext(ctx, "tcp", net.JoinHostPort(host, port)); !IsBlocked(err) {
			t.Errorf("strict dial %s: %v, want blocked", host, err)
		}
	}
	lax := Dialer(true, net.Dialer{Timeout: time.Second})
	c, err := lax.DialContext(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("allowPrivate dial loopback: %v", err)
	}
	c.Close()
	if _, err := lax.DialContext(ctx, "tcp", "169.254.169.254:80"); !IsBlocked(err) {
		t.Errorf("metadata dial: %v, want blocked", err)
	}
}
