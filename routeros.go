package mikrotik

import (
	"context"
	"fmt"
	"net"
	"time"

	ros "github.com/go-routeros/routeros/v3"
)

// rosClient is the testable subset of *ros.Client.
type rosClient interface {
	RunArgsContext(ctx context.Context, args []string) (*ros.Reply, error)
	Close() error
}

// timeoutToRouterOSString converts a Go duration to RouterOS HH:MM:SS format.
// Zero and negative durations are returned as "0s".
func timeoutToRouterOSString(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	total := int(d.Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// cmdPathForAddr returns the RouterOS command path for the given address.
// IPv4 addresses resolve to /ip/firewall/address-list; everything else
// (IPv6, invalid) resolves to /ipv6/firewall/address-list.
func cmdPathForAddr(addr string) string {
	ip := net.ParseIP(addr)
	if ip != nil && ip.To4() != nil {
		return "/ip/firewall/address-list"
	}
	return "/ipv6/firewall/address-list"
}

// writeToRouterOS idempotently writes an address-list entry. It queries for
// an existing entry matching (address, list) and either updates or creates it.
func writeToRouterOS(ctx context.Context, client rosClient, addr, list string, timeout time.Duration, comment string) error {
	cmdPath := cmdPathForAddr(addr)
	wantTimeout := timeoutToRouterOSString(timeout)

	// Query for an existing entry.
	reply, err := client.RunArgsContext(ctx, []string{
		cmdPath + "/print",
		"?address=" + addr,
		"?list=" + list,
	})
	if err != nil {
		return err
	}

	if len(reply.Re) > 0 {
		// Existing entry found – update.
		entry := reply.Re[0]
		setArgs := []string{
			cmdPath + "/set",
			"=.id=" + entry.Map[".id"],
			"=timeout=" + wantTimeout,
		}
		if comment != "" && comment != entry.Map["comment"] {
			setArgs = append(setArgs, "=comment="+comment)
		}
		_, err = client.RunArgsContext(ctx, setArgs)
		return err
	}

	// No existing entry – add.
	addArgs := []string{
		cmdPath + "/add",
		"=address=" + addr,
		"=list=" + list,
		"=timeout=" + wantTimeout,
	}
	if comment != "" {
		addArgs = append(addArgs, "=comment="+comment)
	}
	_, err = client.RunArgsContext(ctx, addArgs)
	return err
}
