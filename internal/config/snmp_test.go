package config

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const communityDeTeste = "c0mmun1ty-s3cr3ta"

func envSNMP(extra map[string]string) map[string]string {
	vars := validEnv()
	vars["SNMP_TARGETS"] = "10.20.0.1, 10.20.0.2"
	vars["SNMP_COMMUNITY"] = communityDeTeste
	for k, v := range extra {
		vars[k] = v
	}
	return vars
}

func TestSNMPDesligadoSemAlvos(t *testing.T) {
	vars := validEnv()
	vars["SNMP_VERSION"] = "3"
	cfg, err := Load(env(vars))
	if err != nil {
		t.Fatalf("Load sem SNMP_TARGETS: %v", err)
	}
	if cfg.SNMP.Ativo() {
		t.Errorf("SNMP ativo sem alvos: %+v", cfg.SNMP)
	}
}

func TestSNMPPadroes(t *testing.T) {
	cfg, err := Load(env(envSNMP(nil)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := cfg.SNMP
	if len(s.Alvos) != 2 || s.Alvos[0] != "10.20.0.1" || s.Alvos[1] != "10.20.0.2" {
		t.Errorf("Alvos = %v", s.Alvos)
	}
	if string(s.Community) != communityDeTeste {
		t.Errorf("Community não carregada")
	}
	if s.Intervalo != time.Minute || s.Timeout != 2*time.Second || s.Retries != 1 || s.Porta != 161 {
		t.Errorf("padrões = intervalo %v timeout %v retries %d porta %d", s.Intervalo, s.Timeout, s.Retries, s.Porta)
	}
}

func TestSNMPValoresInformados(t *testing.T) {
	cfg, err := Load(env(envSNMP(map[string]string{
		"SNMP_TARGETS":  "10.20.0.1,10.20.0.1, fd00::1",
		"SNMP_VERSION":  "2c",
		"SNMP_INTERVAL": "30s",
		"SNMP_TIMEOUT":  "5",
		"SNMP_RETRIES":  "0",
		"SNMP_PORT":     "1161",
	})))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := cfg.SNMP
	if len(s.Alvos) != 2 || s.Alvos[1] != "fd00::1" {
		t.Errorf("Alvos = %v, esperado sem duplicata", s.Alvos)
	}
	if s.Intervalo != 30*time.Second || s.Timeout != 5*time.Second || s.Retries != 0 || s.Porta != 1161 {
		t.Errorf("valores = %+v", s)
	}
}

func TestSNMPIntervaloMinimo(t *testing.T) {
	cfg, err := Load(env(envSNMP(map[string]string{"SNMP_INTERVAL": "3"})))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SNMP.Intervalo != MinSNMPInterval {
		t.Errorf("Intervalo = %v, esperado o mínimo %v", cfg.SNMP.Intervalo, MinSNMPInterval)
	}
}

func TestSNMPAlvoInvalido(t *testing.T) {
	for _, alvo := range []string{"10.20.0.300", "switch.local", "10.20.0.0/24", "fe80::1%eth0", "0.0.0.0", "::", "224.0.0.1", "255.255.255.255"} {
		_, err := Load(env(envSNMP(map[string]string{"SNMP_TARGETS": "10.20.0.1," + alvo, "SNMP_PERMITIR_PUBLICO": "true"})))
		if err == nil {
			t.Errorf("alvo %q aceito", alvo)
			continue
		}
		if !strings.Contains(err.Error(), "SNMP_TARGETS") || !strings.Contains(err.Error(), alvo) {
			t.Errorf("alvo %q: mensagem pouco clara: %v", alvo, err)
		}
	}
}

func TestSNMPAlvoPublicoRecusadoNoBoot(t *testing.T) {
	for _, alvo := range []string{"8.8.8.8", "200.160.2.3", "192.0.2.1", "100.64.0.1", "2001:db8::1", "::ffff:8.8.8.8"} {
		_, err := Load(env(envSNMP(map[string]string{"SNMP_TARGETS": "10.20.0.1," + alvo})))
		if err == nil {
			t.Errorf("alvo público %q aceito sem SNMP_PERMITIR_PUBLICO", alvo)
			continue
		}
		for _, trecho := range []string{"SNMP_TARGETS", alvo, "rede privada", "SNMP_PERMITIR_PUBLICO=true"} {
			if !strings.Contains(err.Error(), trecho) {
				t.Errorf("alvo %q: mensagem sem %q: %v", alvo, trecho, err)
			}
		}
	}
}

func TestSNMPAlvoPublicoComPermissaoExplicita(t *testing.T) {
	cfg, err := Load(env(envSNMP(map[string]string{
		"SNMP_TARGETS":          "10.20.0.1, 8.8.8.8, ::ffff:8.8.8.8, 2001:db8::1",
		"SNMP_PERMITIR_PUBLICO": "true",
	})))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.SNMP.Alvos) != 3 {
		t.Errorf("Alvos = %v", cfg.SNMP.Alvos)
	}
	if len(cfg.SNMP.Publicos) != 2 || cfg.SNMP.Publicos[0] != "8.8.8.8" || cfg.SNMP.Publicos[1] != "2001:db8::1" {
		t.Errorf("Publicos = %v", cfg.SNMP.Publicos)
	}

	for _, valor := range []string{"", "false", "talvez"} {
		_, err := Load(env(envSNMP(map[string]string{"SNMP_TARGETS": "8.8.8.8", "SNMP_PERMITIR_PUBLICO": valor})))
		if err == nil {
			t.Errorf("SNMP_PERMITIR_PUBLICO=%q liberou alvo público", valor)
		}
	}
}

func TestSNMPAlvosInternosAceitos(t *testing.T) {
	alvos := "10.0.0.1, 172.16.5.5, 192.168.1.1, fd12:3456::1, 127.0.0.1, ::1, 169.254.10.20"
	cfg, err := Load(env(envSNMP(map[string]string{"SNMP_TARGETS": alvos})))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.SNMP.Alvos) != 7 || len(cfg.SNMP.Publicos) != 0 {
		t.Errorf("Alvos = %v, Publicos = %v", cfg.SNMP.Alvos, cfg.SNMP.Publicos)
	}
}

func TestSNMPVersaoDiferenteDe2c(t *testing.T) {
	for _, versao := range []string{"3", "v3", "1", "2"} {
		_, err := Load(env(envSNMP(map[string]string{"SNMP_VERSION": versao})))
		if err == nil {
			t.Errorf("versão %q aceita", versao)
			continue
		}
		if !strings.Contains(err.Error(), "v3") || !strings.Contains(err.Error(), "fase seguinte") {
			t.Errorf("versão %q: mensagem não diz que v3 vem depois: %v", versao, err)
		}
	}
}

func TestSNMPCommunityAusente(t *testing.T) {
	for _, valor := range []string{"", "   "} {
		_, err := Load(env(envSNMP(map[string]string{"SNMP_COMMUNITY": valor})))
		if !errors.Is(err, ErrMissingCommunity) {
			t.Errorf("community %q: erro = %v, esperado ErrMissingCommunity", valor, err)
		}
	}
}

func TestSNMPValoresInvalidos(t *testing.T) {
	casos := map[string]string{
		"SNMP_INTERVAL": "rapido",
		"SNMP_TIMEOUT":  "0",
		"SNMP_RETRIES":  "-1",
		"SNMP_PORT":     "70000",
	}
	for chave, valor := range casos {
		_, err := Load(env(envSNMP(map[string]string{chave: valor})))
		if err == nil || !strings.Contains(err.Error(), chave) {
			t.Errorf("%s=%q: erro = %v", chave, valor, err)
		}
	}
}

func TestSNMPCommunityNuncaApareceEmErroNemEmLog(t *testing.T) {
	casos := []map[string]string{
		{"SNMP_TARGETS": "nao-e-ip"},
		{"SNMP_VERSION": "3"},
		{"SNMP_INTERVAL": "x"},
		{"SNMP_TIMEOUT": "x"},
		{"SNMP_RETRIES": "x"},
		{"SNMP_PORT": "x"},
		{"COLLECTOR_SERVER_URL": "http://painel.exemplo"},
	}
	for _, extra := range casos {
		_, err := Load(env(envSNMP(extra)))
		if err == nil {
			t.Errorf("%v: esperava erro", extra)
			continue
		}
		if strings.Contains(err.Error(), communityDeTeste) {
			t.Errorf("%v: community vazou no erro: %v", extra, err)
		}
	}

	cfg, err := Load(env(envSNMP(nil)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, formato := range []string{"%v", "%+v", "%#v", "%s"} {
		if texto := fmt.Sprintf(formato, cfg); strings.Contains(texto, communityDeTeste) {
			t.Errorf("community vazou com %s: %s", formato, texto)
		}
	}
	if fmt.Sprint(cfg.SNMP.Community) != "<oculto>" {
		t.Errorf("Segredo impresso como %q", fmt.Sprint(cfg.SNMP.Community))
	}
}
