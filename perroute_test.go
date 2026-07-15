package mikrotik

import (
	"testing"
	"github.com/coredns/caddy"
)

func TestPerRouteForward(t *testing.T) {
	p1 := writeDomainFile(t, "a.com\n")
	p2 := writeDomainFile(t, "b.com\n")
	input := "mikrotik {\n    domains-file " + p1 + " forward 8.8.8.8\n    domains-file " + p2 + " forward 114.114.114.114 no-write\n}"
	c := caddy.NewTestController("dns", input)
	m, err := parseConfig(c)
	if err != nil { t.Fatal(err) }
	if len(m.routes) != 2 { t.Fatalf("routes=%d", len(m.routes)) }
	if m.routes[0].Forward != "8.8.8.8" { t.Fatalf("route0 Forward=%q", m.routes[0].Forward) }
	if m.routes[1].Forward != "114.114.114.114" { t.Fatalf("route1 Forward=%q", m.routes[1].Forward) }
}
