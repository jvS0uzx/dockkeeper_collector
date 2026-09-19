package scan_test

import (
	"slices"
	"testing"

	"github.com/jvS0uzx/dockkeeper_collector/scan"
)

var portasDoContrato = []int{22, 80, 135, 139, 443, 445, 515, 631, 3389, 5000, 8080, 9100}

func TestPortasPadraoSaoAsDoContrato(t *testing.T) {
	if !slices.Equal(scan.DefaultPorts, portasDoContrato) {
		t.Errorf("DefaultPorts = %v, contrato %v", scan.DefaultPorts, portasDoContrato)
	}
}

func TestAPIPublicaUsavelDeOutroPacote(t *testing.T) {
	ips, err := scan.ExpandCIDR("192.168.10.0/30")
	if err != nil || !slices.Equal(ips, []string{"192.168.10.1", "192.168.10.2"}) {
		t.Errorf("ExpandCIDR = %v, %v", ips, err)
	}
	if _, err := scan.ExpandCIDR("8.8.8.0/24"); err == nil {
		t.Error("faixa pública aceita")
	}

	cfg := scan.Config{CIDRs: []string{"10.0.0.0/30"}}.WithDefaults()
	if !slices.Equal(cfg.Ports, scan.DefaultPorts) || cfg.Timeout != scan.DefaultTimeout || cfg.Concurrency != scan.DefaultConcurrency {
		t.Errorf("WithDefaults = %+v", cfg)
	}

	if !scan.LessIP("10.0.0.2", "10.0.0.10") {
		t.Error("LessIP não ordena numericamente")
	}
	_ = scan.ARPTable()
}
