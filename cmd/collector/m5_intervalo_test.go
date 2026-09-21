package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jvS0uzx/dockkeeper_collector/internal/config"
)

func TestPayloadDeclaraOIntervaloConfigurado(t *testing.T) {
	cfg := config.Config{SiteCode: "norte", Interval: 7 * time.Minute}

	bruto, err := json.Marshal(montarPayload(cfg, nil))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var campos map[string]json.RawMessage
	if err := json.Unmarshal(bruto, &campos); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got := string(campos["report_interval_sec"]); got != "420" {
		t.Fatalf("report_interval_sec = %q, quero 420; payload: %s", got, bruto)
	}
}
