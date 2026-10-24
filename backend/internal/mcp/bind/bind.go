// Package bind decides which address the MCP listener may take and stores the choice (mcp.json in the
// data directory). The rule (spec/architecture/mcp.md §3): an IP address that is assigned to a network
// interface of this machine and is a loopback address or inside 100.64.0.0/10 (the Tailscale range).
// Anything else (0.0.0.0, a LAN or public address, a host name) is refused, so that the bearer token is
// never the only thing between the Internet or the local network and mam.
package bind

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// Default is the address the listener takes when none is saved.
const Default = "127.0.0.1"

// tailnet is 100.64.0.0/10, the carrier-grade NAT range Tailscale assigns its addresses from.
var tailnet = netip.MustParsePrefix("100.64.0.0/10")

// ErrRejected matches (errors.Is) every refusal of Validate.
var ErrRejected = errors.New("bind address rejected")

// RejectedError is a refusal of Validate. Message is for the person who typed the address.
type RejectedError struct{ Message string }

func (e *RejectedError) Error() string { return e.Message }

// Is makes errors.Is(err, ErrRejected) hold.
func (e *RejectedError) Is(target error) bool { return target == ErrRejected }

func reject(format string, args ...any) error {
	return &RejectedError{Message: fmt.Sprintf(format, args...)}
}

// Allowed reports whether ip is in a range the listener may bind to, whether or not this machine has it.
func Allowed(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsLoopback() || (ip.Is4() && tailnet.Contains(ip))
}

// Validate checks host against the rule and returns it as a normalised address. local are the addresses
// of the machine's interfaces (LocalAddrs; a parameter so that a test can say what the machine has).
func Validate(host string, local []netip.Addr) (netip.Addr, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return netip.Addr{}, reject("アドレスを入力してください")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, reject("%q は IP アドレスではありません（ホスト名は使えません）", host)
	}
	ip = ip.Unmap()
	if ip.IsUnspecified() {
		return netip.Addr{}, reject("%s ではすべてのネットワークに公開されるため使えません", ip)
	}
	if !Allowed(ip) {
		return netip.Addr{}, reject("%s は使えません。127.0.0.1（ループバック）か tailnet の IP（100.64.0.0/10）を指定してください", ip)
	}
	for _, l := range local {
		if l.Unmap() == ip {
			return ip, nil
		}
	}
	return netip.Addr{}, reject("%s はこのマシンのネットワークインターフェースにありません（Tailscale が起動しているか確認してください）", ip)
}

// LocalAddrs lists the addresses assigned to the interfaces of this machine.
func LocalAddrs() ([]netip.Addr, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("list network interfaces: %w", err)
	}
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		if p, err := netip.ParsePrefix(a.String()); err == nil {
			out = append(out, p.Addr())
		}
	}
	return out, nil
}

// Candidates are the addresses of this machine the settings screen offers: the loopback address and
// every tailnet address. It is a hint only; Validate decides.
func Candidates(local []netip.Addr) []string {
	out := []string{Default}
	for _, ip := range local {
		if ip = ip.Unmap(); ip.Is4() && tailnet.Contains(ip) {
			out = append(out, ip.String())
		}
	}
	return out
}
