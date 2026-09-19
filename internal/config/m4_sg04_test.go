package config

import (
	"strings"
	"testing"
)

func ambiente(valores map[string]string) Getenv {
	base := map[string]string{
		"COLLECTOR_SITE":  "matriz",
		"COLLECTOR_CIDRS": "192.168.0.0/24",
	}
	for k, v := range valores {
		base[k] = v
	}
	return func(chave string) string { return base[chave] }
}

func TestColetorRecusaHTTPParaPainelRemoto(t *testing.T) {
	_, err := Load(ambiente(map[string]string{"COLLECTOR_SERVER_URL": "http://painel.exemplo.com"}))
	if err == nil {
		t.Fatal("http:// para painel remoto deveria ser recusado: a credencial do coletor iria em claro")
	}
	if !strings.Contains(err.Error(), "https") || !strings.Contains(err.Error(), "ALLOW_INSECURE_HTTP") {
		t.Errorf("mensagem = %q, esperada citando https e ALLOW_INSECURE_HTTP", err.Error())
	}
}

func TestColetorAceitaHTTPNoLocalhost(t *testing.T) {
	for _, url := range []string{"http://127.0.0.1:8080", "http://localhost:8080"} {
		if _, err := Load(ambiente(map[string]string{"COLLECTOR_SERVER_URL": url})); err != nil {
			t.Errorf("%s recusado: %v", url, err)
		}
	}
}

func TestColetorAceitaHTTPComVariavelExplicita(t *testing.T) {
	cfg, err := Load(ambiente(map[string]string{
		"COLLECTOR_SERVER_URL": "http://painel.exemplo.com",
		"ALLOW_INSECURE_HTTP":  "true",
	}))
	if err != nil {
		t.Fatalf("com ALLOW_INSECURE_HTTP=true deveria passar: %v", err)
	}
	if !cfg.Inseguro {
		t.Error("a configuração precisa marcar o modo inseguro, para o aviso no log")
	}
}
