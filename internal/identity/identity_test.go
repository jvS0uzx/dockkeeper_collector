package identity

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func isolarAmbiente(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"COLLECTOR_CREDENTIAL_PATH", "COLLECTOR_MACHINE_ID",
		"COLLECTOR_ENROLL_TOKEN", "COLLECTOR_TOKEN",
	} {
		t.Setenv(k, "")
	}
}

func TestCredencialSalvaECarregada(t *testing.T) {
	isolarAmbiente(t)
	caminho := filepath.Join(t.TempDir(), "sub", "credential.json")
	t.Setenv("COLLECTOR_CREDENTIAL_PATH", caminho)

	quer := Credential{DeviceID: "dev-1", Token: "seg-1", SiteID: 7, Kind: "collector"}
	if err := Save(quer); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(caminho)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permissão = %v, esperado 0600: o segredo vale por si só", info.Mode().Perm())
	}

	got, ok := Load()
	if !ok || got != quer {
		t.Errorf("Load = %+v ok=%v, esperado %+v", got, ok, quer)
	}
}

func TestLoadRecusaCredencialIncompleta(t *testing.T) {
	isolarAmbiente(t)
	caminho := filepath.Join(t.TempDir(), "credential.json")
	t.Setenv("COLLECTOR_CREDENTIAL_PATH", caminho)

	os.WriteFile(caminho, []byte(`{"device_id":"dev","device_token":""}`), 0o600)
	if _, ok := Load(); ok {
		t.Error("credencial sem token deveria ser descartada")
	}
}

func TestEnrollTrocaConvitePorCredencial(t *testing.T) {
	isolarAmbiente(t)
	t.Setenv("COLLECTOR_MACHINE_ID", "maquina-1")

	var recebido map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/enroll" {
			t.Errorf("chamada inesperada: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&recebido)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"device_id": "dev-9", "device_token": "seg-9", "site_id": 3, "kind": "collector",
		})
	}))
	defer srv.Close()

	c, err := Enroll(srv.Client(), srv.URL, "convite-1", "host-a")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if c.DeviceID != "dev-9" || c.Token != "seg-9" || c.SiteID != 3 {
		t.Errorf("credencial = %+v", c)
	}
	if recebido["kind"] != "collector" {
		t.Errorf("kind = %q, o coletor precisa se declarar collector", recebido["kind"])
	}
	if recebido["machine_id"] != "maquina-1" || recebido["enrollment_token"] != "convite-1" {
		t.Errorf("corpo = %+v", recebido)
	}
}

func TestEnrollRecusadoDevolveMotivo(t *testing.T) {
	isolarAmbiente(t)
	t.Setenv("COLLECTOR_MACHINE_ID", "maquina-1")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"convite inválido, expirado ou já utilizado"}`))
	}))
	defer srv.Close()

	_, err := Enroll(srv.Client(), srv.URL, "convite-morto", "host-a")
	if err == nil || !strings.Contains(err.Error(), "enrollment recusado (401)") {
		t.Errorf("erro = %v", err)
	}
}

func TestResolvePrefereCredencialPersistida(t *testing.T) {
	isolarAmbiente(t)
	caminho := filepath.Join(t.TempDir(), "credential.json")
	t.Setenv("COLLECTOR_CREDENTIAL_PATH", caminho)
	t.Setenv("COLLECTOR_TOKEN", "legado-que-nao-deve-ser-usado")

	Save(Credential{DeviceID: "dev-1", Token: "seg-1", SiteID: 2, Kind: "collector"})

	c, legado, err := Resolve(http.DefaultClient, "http://painel.invalid", "host-a")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.DeviceID != "dev-1" || legado != "" {
		t.Errorf("credencial persistida deveria vencer o token legado: %+v legado=%q", c, legado)
	}
}

func TestResolveCaiNoTokenLegado(t *testing.T) {
	isolarAmbiente(t)
	t.Setenv("COLLECTOR_CREDENTIAL_PATH", filepath.Join(t.TempDir(), "credential.json"))
	t.Setenv("COLLECTOR_TOKEN", "legado-1")

	c, legado, err := Resolve(http.DefaultClient, "http://painel.invalid", "host-a")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.DeviceID != "" || legado != "legado-1" {
		t.Errorf("esperado modo legado: %+v legado=%q", c, legado)
	}
}

func TestResolveEnrollaEPersiste(t *testing.T) {
	isolarAmbiente(t)
	caminho := filepath.Join(t.TempDir(), "credential.json")
	t.Setenv("COLLECTOR_CREDENTIAL_PATH", caminho)
	t.Setenv("COLLECTOR_MACHINE_ID", "maquina-1")
	t.Setenv("COLLECTOR_ENROLL_TOKEN", "convite-1")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"device_id": "dev-5", "device_token": "seg-5", "site_id": 1, "kind": "collector",
		})
	}))
	defer srv.Close()

	c, legado, err := Resolve(srv.Client(), srv.URL, "host-a")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.DeviceID != "dev-5" || legado != "" {
		t.Errorf("credencial = %+v legado=%q", c, legado)
	}
	if salva, ok := Load(); !ok || salva.DeviceID != "dev-5" {
		t.Errorf("credencial não foi persistida: %+v ok=%v", salva, ok)
	}
}

func TestResolveSemIdentidadeFalha(t *testing.T) {
	isolarAmbiente(t)
	t.Setenv("COLLECTOR_CREDENTIAL_PATH", filepath.Join(t.TempDir(), "credential.json"))

	_, _, err := Resolve(http.DefaultClient, "http://painel.invalid", "host-a")
	if err == nil || !strings.Contains(err.Error(), "sem identidade") {
		t.Errorf("erro = %v, o coletor sem identidade não pode subir em silêncio", err)
	}
}

func TestCaminhoPadraoDaCredencialUsaNomeDockKeeper(t *testing.T) {
	t.Setenv("COLLECTOR_CREDENTIAL_PATH", "")
	t.Setenv("ProgramData", `C:\ProgramData`)
	got := CredentialPath()
	esperado := "/var/lib/dockkeeper-collector/credential.json"
	if runtime.GOOS == "windows" {
		esperado = filepath.Join(`C:\ProgramData`, "dockkeeper-collector", "credential.json")
	}
	if got != esperado {
		t.Errorf("CredentialPath() = %q, esperado %q", got, esperado)
	}
}

func TestAvisoDoModoLegadoApontaAFlagDoPainel(t *testing.T) {
	isolarAmbiente(t)
	t.Setenv("COLLECTOR_CREDENTIAL_PATH", filepath.Join(t.TempDir(), "credential.json"))
	t.Setenv("COLLECTOR_TOKEN", "legado-1")
	var saida bytes.Buffer
	anterior := log.Writer()
	log.SetOutput(&saida)
	t.Cleanup(func() { log.SetOutput(anterior) })

	if _, _, err := Resolve(http.DefaultClient, "http://painel.invalid", "host-a"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.Contains(saida.String(), "ALLOW_LEGACY_INGEST_TOKEN=true") {
		t.Errorf("aviso do modo legado não cita a flag do painel: %q", saida.String())
	}
}
