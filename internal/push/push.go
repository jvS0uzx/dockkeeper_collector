package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jvS0uzx/dockkeeper_collector/internal/snmp"
	"github.com/jvS0uzx/dockkeeper_collector/scan"
)

const (
	timeout = 30 * time.Second

	maxAttempts = 3
	retryDelay  = 5 * time.Second

	endpointPath     = "/api/ingest/inventory"
	endpointRedePath = "/api/ingest/network-metrics"

	SchemaRede = 1
)

var ErrUnauthorized = errors.New("credencial recusada pelo painel")

var ErrRecusado = errors.New("painel recusou o envio")

var errTransitorio = errors.New("falha transitória")

type Payload struct {
	SiteCode         string      `json:"site_code"`
	CollectorVersion string      `json:"collector_version"`
	Hosts            []scan.Host `json:"hosts"`

	ReportIntervalSec int `json:"report_interval_sec"`
}

type MetricasDeRede struct {
	Schema           int                `json:"schema"`
	SiteCode         string             `json:"site_code"`
	CollectorVersion string             `json:"collector_version"`
	IntervalSec      int                `json:"interval_sec"`
	CollectedAt      time.Time          `json:"collected_at"`
	Devices          []snmp.Dispositivo `json:"devices"`
}

type Identity struct {
	DeviceID    string
	DeviceToken string
	LegacyToken string
}

type Client struct {
	baseURL string
	id      Identity
	http    *http.Client

	delay time.Duration
}

func New(baseURL string, id Identity) *Client {
	return &Client{
		baseURL: baseURL,
		id:      id,
		http:    &http.Client{Timeout: timeout},
		delay:   retryDelay,
	}
}

func (c *Client) Send(ctx context.Context, payload Payload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("erro ao serializar o inventário: %w", err)
	}
	return c.enviar(ctx, endpointPath, body)
}

func (c *Client) SendMetricasDeRede(ctx context.Context, payload MetricasDeRede) error {
	payload.Schema = SchemaRede
	payload.CollectedAt = payload.CollectedAt.UTC().Truncate(time.Second)
	if payload.Devices == nil {
		payload.Devices = []snmp.Dispositivo{}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("erro ao serializar as métricas de rede: %w", err)
	}
	return c.enviar(ctx, endpointRedePath, body)
}

func (c *Client) enviar(ctx context.Context, caminho string, body []byte) error {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := c.post(ctx, caminho, body)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errTransitorio) {
			return err
		}
		lastErr = err

		if attempt == maxAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(c.delay):
		}
	}
	return fmt.Errorf("envio falhou após %d tentativas: %w", maxAttempts, lastErr)
}

func (c *Client) post(ctx context.Context, caminho string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+caminho, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.id.DeviceID != "" {
		req.Header.Set("X-Device-Id", c.id.DeviceID)
		req.Header.Set("X-Device-Token", c.id.DeviceToken)
	} else {
		req.Header.Set("X-Agent-Token", c.id.LegacyToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", errTransitorio, c.redact(err))
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK, resp.StatusCode == http.StatusAccepted:
		return nil
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		if c.id.DeviceID == "" {
			return fmt.Errorf("%w (HTTP %d): o token compartilhado so e aceito com "+
				"ALLOW_LEGACY_INGEST_TOKEN=true no painel; migre para COLLECTOR_ENROLL_TOKEN",
				ErrUnauthorized, resp.StatusCode)
		}
		return fmt.Errorf("%w (HTTP %d)", ErrUnauthorized, resp.StatusCode)
	case resp.StatusCode >= 500, resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: painel respondeu HTTP %d", errTransitorio, resp.StatusCode)
	default:
		return fmt.Errorf("%w (HTTP %d)", ErrRecusado, resp.StatusCode)
	}
}

func (c *Client) redact(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if c.id.DeviceToken != "" {
		msg = strings.ReplaceAll(msg, c.id.DeviceToken, "<CREDENCIAL>")
	}
	if c.id.LegacyToken != "" {
		msg = strings.ReplaceAll(msg, c.id.LegacyToken, "<COLLECTOR_TOKEN>")
	}
	return errors.New(msg)
}
