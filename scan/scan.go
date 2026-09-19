package scan

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

var DefaultPorts = []int{22, 80, 135, 139, 443, 445, 515, 631, 3389, 5000, 8080, 9100}

const (
	DefaultTimeout     = 400 * time.Millisecond
	DefaultConcurrency = 256

	minPrefixLen = 16

	reverseDNSTimeout = time.Second
)

type Host struct {
	IP        string `json:"ip"`
	Hostname  string `json:"hostname"`
	MAC       string `json:"mac"`
	OpenPorts []int  `json:"open_ports"`
}

type Config struct {
	CIDRs       []string
	Ports       []int
	Timeout     time.Duration
	Concurrency int
}

func (c Config) WithDefaults() Config {
	if len(c.Ports) == 0 {
		c.Ports = DefaultPorts
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	if c.Concurrency <= 0 {
		c.Concurrency = DefaultConcurrency
	}
	return c
}

func ExpandCIDR(cidr string) ([]string, error) {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("faixa inválida %q: %w", cidr, err)
	}
	if ip.To4() == nil {
		return nil, fmt.Errorf("faixa %q: só IPv4 é suportado", cidr)
	}
	if !isPrivate(network.IP) {
		return nil, fmt.Errorf("faixa %q não é privada; a varredura só cobre a rede interna", cidr)
	}

	ones, bits := network.Mask.Size()
	if ones < minPrefixLen {
		return nil, fmt.Errorf("faixa %q é grande demais (máximo /%d)", cidr, minPrefixLen)
	}

	var ips []string
	for addr := network.IP.Mask(network.Mask); network.Contains(addr); addr = nextIP(addr) {
		ips = append(ips, addr.String())
	}

	if ones < bits-1 && len(ips) > 2 {
		ips = ips[1 : len(ips)-1]
	}
	return ips, nil
}

func isPrivate(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLoopback()
}

func nextIP(ip net.IP) net.IP {
	next := make(net.IP, len(ip))
	copy(next, ip)
	for i := len(next) - 1; i >= 0; i-- {
		next[i]++
		if next[i] != 0 {
			break
		}
	}
	return next
}

func Run(ctx context.Context, cfg Config) ([]Host, []error) {
	cfg = cfg.WithDefaults()

	var targets []string
	var errs []error
	for _, cidr := range cfg.CIDRs {
		ips, err := ExpandCIDR(cidr)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		targets = append(targets, ips...)
	}
	if len(targets) == 0 {
		return nil, errs
	}

	var (
		mu    sync.Mutex
		found []Host
		wg    sync.WaitGroup
	)
	sem := make(chan struct{}, cfg.Concurrency)

	for _, ip := range targets {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			ports := probe(ctx, ip, cfg.Ports, cfg.Timeout)
			if len(ports) == 0 {
				return
			}

			host := Host{IP: ip, OpenPorts: ports, Hostname: reverseDNS(ctx, ip)}

			mu.Lock()
			found = append(found, host)
			mu.Unlock()
		}(ip)
	}
	wg.Wait()

	arp := ARPTable()
	for i := range found {
		found[i].MAC = arp[found[i].IP]
	}

	sort.Slice(found, func(i, j int) bool { return LessIP(found[i].IP, found[j].IP) })
	return found, errs
}

func probe(ctx context.Context, ip string, ports []int, timeout time.Duration) []int {
	var open []int
	dialer := net.Dialer{Timeout: timeout}

	for _, port := range ports {
		if ctx.Err() != nil {
			break
		}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
		if err != nil {
			continue
		}
		conn.Close()
		open = append(open, port)
	}
	return open
}

func reverseDNS(ctx context.Context, ip string) string {
	ctx, cancel := context.WithTimeout(ctx, reverseDNSTimeout)
	defer cancel()

	names, err := net.DefaultResolver.LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	name := names[0]
	if len(name) > 0 && name[len(name)-1] == '.' {
		name = name[:len(name)-1]
	}
	return name
}

func LessIP(a, b string) bool {
	ipA, ipB := net.ParseIP(a).To4(), net.ParseIP(b).To4()
	if ipA == nil || ipB == nil {
		return a < b
	}
	for i := range ipA {
		if ipA[i] != ipB[i] {
			return ipA[i] < ipB[i]
		}
	}
	return false
}
