package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jvS0uzx/dockkeeper_collector/internal/config"
	"github.com/jvS0uzx/dockkeeper_collector/internal/push"
	"github.com/jvS0uzx/dockkeeper_collector/internal/snmp"
)

const communityDoMain = "c0mmun1ty-d0-m41n"

func cfgSNMP(t *testing.T, porta string) config.Config {
	t.Helper()
	vars := map[string]string{
		"COLLECTOR_SERVER_URL": "https://painel.exemplo",
		"COLLECTOR_SITE":       "filial-a",
		"COLLECTOR_CIDRS":      "192.168.0.0/24",
		"SNMP_TARGETS":         "127.0.0.1",
		"SNMP_COMMUNITY":       communityDoMain,
		"SNMP_INTERVAL":        "45s",
		"SNMP_TIMEOUT":         "200ms",
		"SNMP_RETRIES":         "0",
		"SNMP_PORT":            porta,
	}
	cfg, err := config.Load(func(k string) string { return vars[k] })
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestResumoSNMPNaoMostraCommunity(t *testing.T) {
	cfg := cfgSNMP(t, "161")
	resumo := resumoSNMP(cfg.SNMP)
	if strings.Contains(resumo, communityDoMain) {
		t.Fatalf("community no log de subida: %s", resumo)
	}
	if !strings.Contains(resumo, "127.0.0.1") || !strings.Contains(resumo, "45s") {
		t.Errorf("resumo incompleto: %s", resumo)
	}
	if !strings.Contains(resumoSNMP(config.SNMP{}), "desligado") {
		t.Errorf("SNMP sem alvos deveria aparecer como desligado")
	}
}

func TestCicloSNMPEnviaAlvoInalcancavelSemCommunity(t *testing.T) {
	var corpo []byte
	var chamadas atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamadas.Add(1)
		corpo, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := cfgSNMP(t, "9")
	coletor := snmp.NovoColetor(snmp.Config{
		Porta:     cfg.SNMP.Porta,
		Community: string(cfg.SNMP.Community),
		Timeout:   cfg.SNMP.Timeout,
		Retries:   cfg.SNMP.Retries,
	})
	client := push.New(srv.URL, push.Identity{DeviceID: "d", DeviceToken: "t"})

	if err := cicloSNMP(context.Background(), cfg, coletor, client); err != nil {
		t.Fatalf("cicloSNMP: %v", err)
	}
	if chamadas.Load() != 1 {
		t.Fatalf("%d envios, esperado 1", chamadas.Load())
	}
	if strings.Contains(string(corpo), communityDoMain) {
		t.Fatalf("community no corpo enviado: %s", corpo)
	}

	var enviado struct {
		Schema      int    `json:"schema"`
		SiteCode    string `json:"site_code"`
		IntervalSec int    `json:"interval_sec"`
		CollectedAt string `json:"collected_at"`
		Devices     []struct {
			IP        string            `json:"ip"`
			Reachable bool              `json:"reachable"`
			Error     string            `json:"error"`
			Ifs       []json.RawMessage `json:"interfaces"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(corpo, &enviado); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if enviado.Schema != 1 || enviado.SiteCode != "filial-a" || enviado.IntervalSec != 45 {
		t.Errorf("cabeçalho do lote = %+v", enviado)
	}
	if _, err := time.Parse(time.RFC3339, enviado.CollectedAt); err != nil || !strings.HasSuffix(enviado.CollectedAt, "Z") {
		t.Errorf("collected_at = %q", enviado.CollectedAt)
	}
	if len(enviado.Devices) != 1 || enviado.Devices[0].Reachable || enviado.Devices[0].Error == "" || enviado.Devices[0].Ifs == nil {
		t.Errorf("dispositivo = %+v", enviado.Devices)
	}
}
