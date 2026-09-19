package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultInterval = 15 * time.Minute
	MinInterval     = time.Minute
)

type Config struct {
	ServerURL string
	Inseguro  bool
	SiteCode  string

	CIDRs    []string
	Ports    []int
	Interval time.Duration

	Once bool
}

type Getenv func(string) string

var (
	ErrMissingServerURL = errors.New("COLLECTOR_SERVER_URL não definido")
	ErrMissingSite      = errors.New("COLLECTOR_SITE não definido (código da unidade cadastrado no painel)")
	ErrMissingCIDRs     = errors.New("COLLECTOR_CIDRS não definido (ex: 192.168.0.0/24)")
)

func alvoInseguro(bruto string) (bool, error) {
	endereco, err := url.Parse(strings.TrimSpace(bruto))
	if err != nil || endereco.Host == "" {
		return false, fmt.Errorf("COLLECTOR_SERVER_URL inválido: %q", bruto)
	}
	if endereco.Scheme != "http" {
		return false, nil
	}
	host := endereco.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return false, nil
	}
	return true, nil
}

func Load(getenv Getenv) (Config, error) {
	cfg := Config{
		ServerURL: strings.TrimRight(strings.TrimSpace(getenv("COLLECTOR_SERVER_URL")), "/"),
		SiteCode:  strings.ToLower(strings.TrimSpace(getenv("COLLECTOR_SITE"))),
		CIDRs:     SplitList(getenv("COLLECTOR_CIDRS")),
		Interval:  DefaultInterval,
		Once:      isTrue(getenv("COLLECTOR_ONCE")),
	}

	for _, raw := range SplitList(getenv("COLLECTOR_PORTS")) {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n < 65536 {
			cfg.Ports = append(cfg.Ports, n)
		}
	}

	if raw := strings.TrimSpace(getenv("COLLECTOR_INTERVAL_MIN")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			cfg.Interval = time.Duration(n) * time.Minute
		}
	}
	if cfg.Interval < MinInterval {
		cfg.Interval = MinInterval
	}

	inseguro, err := alvoInseguro(cfg.ServerURL)
	if cfg.ServerURL != "" && err != nil {
		return cfg, err
	}
	permitido := isTrue(getenv("ALLOW_INSECURE_HTTP"))
	if inseguro && !permitido {
		return cfg, fmt.Errorf(
			"COLLECTOR_SERVER_URL usa http:// para um painel remoto (%s): a credencial do coletor viajaria em claro. Use https, ou defina ALLOW_INSECURE_HTTP=true se a rede for confiável",
			cfg.ServerURL)
	}
	cfg.Inseguro = inseguro && permitido

	switch {
	case cfg.ServerURL == "":
		return cfg, ErrMissingServerURL
	case cfg.SiteCode == "":
		return cfg, ErrMissingSite
	case len(cfg.CIDRs) == 0:
		return cfg, ErrMissingCIDRs
	}
	return cfg, nil
}

func SplitList(raw string) []string {
	var out []string
	for _, item := range strings.Split(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func isTrue(raw string) bool {
	v, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && v
}
