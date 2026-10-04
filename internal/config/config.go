package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultInterval = 15 * time.Minute
	MinInterval     = time.Minute

	DefaultSNMPInterval = time.Minute
	MinSNMPInterval     = 10 * time.Second
	DefaultSNMPTimeout  = 2 * time.Second
	DefaultSNMPRetries  = 1
	DefaultSNMPPort     = 161
)

type Segredo string

func (Segredo) String() string { return "<oculto>" }

func (Segredo) GoString() string { return "<oculto>" }

type SNMP struct {
	Alvos     []string
	Community Segredo
	Intervalo time.Duration
	Timeout   time.Duration
	Retries   int
	Porta     uint16
}

func (s SNMP) Ativo() bool { return len(s.Alvos) > 0 }

type Config struct {
	ServerURL string
	Inseguro  bool
	SiteCode  string

	CIDRs    []string
	Ports    []int
	Interval time.Duration

	Once bool

	SNMP SNMP
}

type Getenv func(string) string

var (
	ErrMissingServerURL = errors.New("COLLECTOR_SERVER_URL não definido")
	ErrMissingSite      = errors.New("COLLECTOR_SITE não definido (código da unidade cadastrado no painel)")
	ErrMissingCIDRs     = errors.New("COLLECTOR_CIDRS não definido (ex: 192.168.0.0/24)")
	ErrMissingCommunity = errors.New("SNMP_COMMUNITY não definido: obrigatório quando SNMP_TARGETS tem alvos")
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

	snmp, err := carregarSNMP(getenv)
	if err != nil {
		return cfg, err
	}
	cfg.SNMP = snmp
	return cfg, nil
}

func carregarSNMP(getenv Getenv) (SNMP, error) {
	s := SNMP{
		Intervalo: DefaultSNMPInterval,
		Timeout:   DefaultSNMPTimeout,
		Retries:   DefaultSNMPRetries,
		Porta:     DefaultSNMPPort,
	}

	vistos := map[string]bool{}
	for _, bruto := range SplitList(getenv("SNMP_TARGETS")) {
		ip, err := netip.ParseAddr(bruto)
		if err != nil || ip.Zone() != "" {
			return SNMP{}, fmt.Errorf("SNMP_TARGETS tem um alvo inválido: %q não é um endereço IP", bruto)
		}
		texto := ip.Unmap().String()
		if !vistos[texto] {
			vistos[texto] = true
			s.Alvos = append(s.Alvos, texto)
		}
	}
	if !s.Ativo() {
		return SNMP{}, nil
	}

	switch versao := strings.TrimSpace(getenv("SNMP_VERSION")); versao {
	case "", "2c":
	default:
		return SNMP{}, fmt.Errorf("SNMP_VERSION=%q não é aceito: só 2c por enquanto; v3 vem numa fase seguinte", versao)
	}

	s.Community = Segredo(getenv("SNMP_COMMUNITY"))
	if strings.TrimSpace(string(s.Community)) == "" {
		return SNMP{}, ErrMissingCommunity
	}

	if bruto := strings.TrimSpace(getenv("SNMP_INTERVAL")); bruto != "" {
		d, err := duracao(bruto)
		if err != nil || d <= 0 {
			return SNMP{}, fmt.Errorf("SNMP_INTERVAL inválido: %q (use segundos, ex: 60, ou duração, ex: 60s)", bruto)
		}
		s.Intervalo = d
	}
	if s.Intervalo < MinSNMPInterval {
		s.Intervalo = MinSNMPInterval
	}

	if bruto := strings.TrimSpace(getenv("SNMP_TIMEOUT")); bruto != "" {
		d, err := duracao(bruto)
		if err != nil || d <= 0 {
			return SNMP{}, fmt.Errorf("SNMP_TIMEOUT inválido: %q (use segundos, ex: 2, ou duração, ex: 2s)", bruto)
		}
		s.Timeout = d
	}

	if bruto := strings.TrimSpace(getenv("SNMP_RETRIES")); bruto != "" {
		n, err := strconv.Atoi(bruto)
		if err != nil || n < 0 || n > 10 {
			return SNMP{}, fmt.Errorf("SNMP_RETRIES inválido: %q (de 0 a 10)", bruto)
		}
		s.Retries = n
	}

	if bruto := strings.TrimSpace(getenv("SNMP_PORT")); bruto != "" {
		n, err := strconv.Atoi(bruto)
		if err != nil || n <= 0 || n >= 65536 {
			return SNMP{}, fmt.Errorf("SNMP_PORT inválido: %q", bruto)
		}
		s.Porta = uint16(n)
	}
	return s, nil
}

func duracao(bruto string) (time.Duration, error) {
	if n, err := strconv.Atoi(bruto); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	return time.ParseDuration(bruto)
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
