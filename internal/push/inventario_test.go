package push

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jvS0uzx/dockkeeper_collector/scan"
)

const inventarioEsperado = `{
  "schema": 1,
  "site_code": "filial-a",
  "collector_version": "1.0.0",
  "hosts": [
    {
      "ip": "192.168.0.10",
      "hostname": "impressora-1",
      "mac": "00:11:22:33:44:55",
      "open_ports": [
        80,
        9100
      ]
    }
  ],
  "report_interval_sec": 900
}`

func TestInventarioSegueOContrato(t *testing.T) {
	var recebido []byte
	var caminho string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caminho = r.URL.Path
		recebido, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	payload := Payload{
		Schema:           7,
		SiteCode:         "filial-a",
		CollectorVersion: "1.0.0",
		Hosts: []scan.Host{
			{IP: "192.168.0.10", Hostname: "impressora-1", MAC: "00:11:22:33:44:55", OpenPorts: []int{80, 9100}},
		},
		ReportIntervalSec: 900,
	}
	if err := clienteDeTeste(srv.URL, Identity{DeviceID: "d", DeviceToken: "t"}).Send(context.Background(), payload); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if caminho != "/api/ingest/inventory" {
		t.Errorf("caminho = %q", caminho)
	}
	var indentado bytes.Buffer
	if err := json.Indent(&indentado, recebido, "", "  "); err != nil {
		t.Fatalf("corpo não é JSON: %v: %s", err, recebido)
	}
	if indentado.String() != inventarioEsperado {
		t.Errorf("corpo diverge do contrato.\nrecebido:\n%s\nesperado:\n%s", indentado.String(), inventarioEsperado)
	}
}
