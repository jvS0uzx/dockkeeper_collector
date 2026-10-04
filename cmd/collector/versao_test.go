package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jvS0uzx/dockkeeper_collector/internal/push"
	"github.com/jvS0uzx/dockkeeper_collector/internal/snmp"
)

func TestVersaoPadraoEhDev(t *testing.T) {
	if Version != "dev" {
		t.Fatalf("Version = %q, esperado \"dev\" fora de um build com -ldflags", Version)
	}
}

func capturarLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var saida bytes.Buffer
	log.SetOutput(&saida)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &saida
}

func cicloContraPainel(t *testing.T, resposta string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, resposta)
	}))
	defer srv.Close()

	cfg := cfgSNMP(t, "9")
	coletor := snmp.NovoColetor(snmp.Config{Porta: cfg.SNMP.Porta, Community: "x", Timeout: cfg.SNMP.Timeout})
	client := push.New(srv.URL, push.Identity{DeviceID: "d", DeviceToken: "t"})

	saida := capturarLog(t)
	if err := cicloSNMP(context.Background(), cfg, coletor, client); err != nil {
		t.Fatalf("cicloSNMP: %v", err)
	}
	return saida.String()
}

func TestCicloSNMPAvisaQuandoOPainelDescartaEquipamentos(t *testing.T) {
	saida := cicloContraPainel(t, `{"devices": 1, "interfaces": 0, "rejeitados": 1}`)
	if !strings.Contains(saida, "AVISO") || !strings.Contains(saida, "descartou 1 de 1 equipamentos") {
		t.Errorf("log sem aviso de rejeitados:\n%s", saida)
	}
}

func TestCicloSNMPSemRejeitadosNaoAvisa(t *testing.T) {
	for _, resposta := range []string{`{"devices": 1, "interfaces": 0, "rejeitados": 0}`, `{"devices": 1, "interfaces": 0}`, ``} {
		if saida := cicloContraPainel(t, resposta); strings.Contains(saida, "AVISO") {
			t.Errorf("resposta %q gerou aviso:\n%s", resposta, saida)
		}
	}
}
