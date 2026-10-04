package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jvS0uzx/dockkeeper_collector/internal/snmp"
)

func f64(v float64) *float64 { return &v }

func u64(v uint64) *uint64 { return &v }

func metricasDeExemplo() MetricasDeRede {
	return MetricasDeRede{
		SiteCode:         "filial-a",
		CollectorVersion: "1.1.0",
		IntervalSec:      60,
		CollectedAt:      time.Date(2026, 10, 3, 12, 0, 0, 500, time.FixedZone("BRT", -3*3600)),
		Devices: []snmp.Dispositivo{
			{
				IP:        "192.0.2.1",
				Reachable: true,
				SysName:   "sw-core",
				SysDescr:  "texto",
				UptimeSec: u64(123456),
				Interfaces: []snmp.Interface{
					{
						IfIndex: 1, IfName: "ge-0/0/1", IfAlias: "uplink",
						SpeedMbps: f64(1000), OperStatus: "up", AdminStatus: "up",
						InBps: f64(1234.5), OutBps: f64(99),
						InErrors: u64(0), OutErrors: u64(0), InDiscards: u64(0), OutDiscards: u64(0),
					},
					{
						IfIndex: 2, IfDescr: "eth1", OperStatus: "down", AdminStatus: "unknown",
					},
				},
			},
			{
				IP:         "192.0.2.2",
				Error:      "sem resposta SNMP no tempo limite",
				Interfaces: []snmp.Interface{},
			},
		},
	}
}

const corpoEsperado = `{
  "schema": 1,
  "site_code": "filial-a",
  "collector_version": "1.1.0",
  "interval_sec": 60,
  "collected_at": "2026-10-03T15:00:00Z",
  "devices": [
    {
      "ip": "192.0.2.1",
      "reachable": true,
      "error": "",
      "sys_name": "sw-core",
      "sys_descr": "texto",
      "uptime_sec": 123456,
      "interfaces": [
        {
          "if_index": 1,
          "if_name": "ge-0/0/1",
          "if_descr": "",
          "if_alias": "uplink",
          "speed_mbps": 1000,
          "oper_status": "up",
          "admin_status": "up",
          "in_bps": 1234.5,
          "out_bps": 99,
          "in_errors": 0,
          "out_errors": 0,
          "in_discards": 0,
          "out_discards": 0
        },
        {
          "if_index": 2,
          "if_name": "",
          "if_descr": "eth1",
          "if_alias": "",
          "speed_mbps": null,
          "oper_status": "down",
          "admin_status": "unknown",
          "in_bps": null,
          "out_bps": null,
          "in_errors": null,
          "out_errors": null,
          "in_discards": null,
          "out_discards": null
        }
      ]
    },
    {
      "ip": "192.0.2.2",
      "reachable": false,
      "error": "sem resposta SNMP no tempo limite",
      "sys_name": "",
      "sys_descr": "",
      "uptime_sec": null,
      "interfaces": []
    }
  ]
}`

func TestMetricasDeRedeSeguemOContrato(t *testing.T) {
	var recebido []byte
	var caminho string
	var headers http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caminho = r.URL.Path
		headers = r.Header.Clone()
		recebido, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := clienteDeTeste(srv.URL, Identity{DeviceID: "dev-1", DeviceToken: "segredo-1"})
	if _, err := c.SendMetricasDeRede(context.Background(), metricasDeExemplo()); err != nil {
		t.Fatalf("SendMetricasDeRede: %v", err)
	}

	if caminho != "/api/ingest/network-metrics" {
		t.Errorf("caminho = %q", caminho)
	}
	if headers.Get("X-Device-Id") != "dev-1" || headers.Get("X-Device-Token") != "segredo-1" || headers.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", headers)
	}

	var indentado bytes.Buffer
	if err := json.Indent(&indentado, recebido, "", "  "); err != nil {
		t.Fatalf("corpo não é JSON: %v: %s", err, recebido)
	}
	if indentado.String() != corpoEsperado {
		t.Errorf("corpo diverge do contrato.\nrecebido:\n%s\nesperado:\n%s", indentado.String(), corpoEsperado)
	}
}

func TestMetricasDeRedeSemDispositivosEnviaListaVazia(t *testing.T) {
	var recebido []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recebido, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := clienteDeTeste(srv.URL, Identity{LegacyToken: "x"}).SendMetricasDeRede(context.Background(), MetricasDeRede{}); err != nil {
		t.Fatalf("SendMetricasDeRede: %v", err)
	}
	if !strings.Contains(string(recebido), `"devices":[]`) || !strings.Contains(string(recebido), `"schema":1`) {
		t.Errorf("corpo = %s", recebido)
	}
}

func TestMetricasDeRedeRespostasDoPainel(t *testing.T) {
	casos := []struct {
		status     int
		tentativas int32
		alvo       error
	}{
		{http.StatusBadRequest, 1, ErrRecusado},
		{http.StatusConflict, 1, ErrRecusado},
		{http.StatusRequestEntityTooLarge, 1, ErrRecusado},
		{http.StatusUnauthorized, 1, ErrUnauthorized},
		{http.StatusForbidden, 1, ErrUnauthorized},
		{http.StatusServiceUnavailable, int32(maxAttempts), nil},
	}
	for _, caso := range casos {
		var chamadas atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			chamadas.Add(1)
			w.WriteHeader(caso.status)
		}))
		_, err := clienteDeTeste(srv.URL, Identity{DeviceID: "d", DeviceToken: "t"}).SendMetricasDeRede(context.Background(), metricasDeExemplo())
		srv.Close()

		if err == nil {
			t.Errorf("HTTP %d: esperava erro", caso.status)
			continue
		}
		if n := chamadas.Load(); n != caso.tentativas {
			t.Errorf("HTTP %d: %d tentativas, esperado %d", caso.status, n, caso.tentativas)
		}
		if caso.alvo != nil && !errors.Is(err, caso.alvo) {
			t.Errorf("HTTP %d: erro = %v, esperado %v", caso.status, err, caso.alvo)
		}
		if caso.alvo == nil && (errors.Is(err, ErrRecusado) || errors.Is(err, ErrUnauthorized)) {
			t.Errorf("HTTP %d: transitório classificado como recusa: %v", caso.status, err)
		}
	}
}

func TestMetricasDeRedeLeContagemDaResposta(t *testing.T) {
	casos := []struct {
		corpo    string
		esperado RespostaRede
	}{
		{`{"devices": 2, "interfaces": 5, "rejeitados": 1}`, RespostaRede{Devices: 2, Interfaces: 5, Rejeitados: 1}},
		{`{"devices": 2, "interfaces": 5}`, RespostaRede{Devices: 2, Interfaces: 5}},
		{``, RespostaRede{}},
		{`não é json`, RespostaRede{}},
	}
	for _, caso := range casos {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, caso.corpo)
		}))
		resposta, err := clienteDeTeste(srv.URL, Identity{DeviceID: "d", DeviceToken: "t"}).SendMetricasDeRede(context.Background(), metricasDeExemplo())
		srv.Close()

		if err != nil {
			t.Errorf("corpo %q: erro = %v", caso.corpo, err)
			continue
		}
		if resposta != caso.esperado {
			t.Errorf("corpo %q: resposta = %+v, esperado %+v", caso.corpo, resposta, caso.esperado)
		}
	}
}

func TestMetricasDeRedeNuncaEnviaMedidaNegativaOuNaoFinita(t *testing.T) {
	var recebido []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recebido, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	original := metricasDeExemplo()
	original.Devices[0].Interfaces[0].InBps = f64(-8)
	original.Devices[0].Interfaces[0].OutBps = f64(math.Inf(1))
	original.Devices[0].Interfaces[0].SpeedMbps = f64(math.NaN())

	if _, err := clienteDeTeste(srv.URL, Identity{DeviceID: "d", DeviceToken: "t"}).SendMetricasDeRede(context.Background(), original); err != nil {
		t.Fatalf("SendMetricasDeRede: %v", err)
	}

	var enviado struct {
		Devices []struct {
			Interfaces []map[string]json.RawMessage `json:"interfaces"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(recebido, &enviado); err != nil {
		t.Fatalf("corpo não é JSON: %v: %s", err, recebido)
	}
	primeira := enviado.Devices[0].Interfaces[0]
	for _, campo := range []string{"in_bps", "out_bps", "speed_mbps"} {
		if string(primeira[campo]) != "null" {
			t.Errorf("%s = %s, esperado null", campo, primeira[campo])
		}
	}
	if *original.Devices[0].Interfaces[0].InBps != -8 {
		t.Errorf("a higienização alterou o lote do chamador")
	}
}
