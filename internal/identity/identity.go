package identity

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Credential struct {
	DeviceID string `json:"device_id"`
	Token    string `json:"device_token"`
	SiteID   uint   `json:"site_id"`
	Kind     string `json:"kind"`
}

func CredentialPath() string {
	if v := strings.TrimSpace(os.Getenv("COLLECTOR_CREDENTIAL_PATH")); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("ProgramData"), "dockkeeper-collector", "credential.json")
	}
	return "/var/lib/dockkeeper-collector/credential.json"
}

func machineID() string {
	if v := strings.TrimSpace(os.Getenv("COLLECTOR_MACHINE_ID")); v != "" {
		return v
	}

	for _, caminho := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(caminho); err == nil {
			if id := strings.TrimSpace(string(b)); id != "" {
				return id
			}
		}
	}

	return persistedFallbackID()
}

func persistedFallbackID() string {
	caminho := filepath.Join(filepath.Dir(CredentialPath()), "machine-id")

	if b, err := os.ReadFile(caminho); err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id
		}
	}

	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		log.Printf("AVISO: nao foi possivel gerar identificador de maquina: %v", err)
		return ""
	}
	id := hex.EncodeToString(buf)

	if err := os.MkdirAll(filepath.Dir(caminho), 0o755); err == nil {
		if err := os.WriteFile(caminho, []byte(id+"\n"), 0o600); err != nil {
			log.Printf("AVISO: identificador de maquina nao foi persistido: %v", err)
		}
	}
	return id
}

func Load() (Credential, bool) {
	b, err := os.ReadFile(CredentialPath())
	if err != nil {
		return Credential{}, false
	}

	var c Credential
	if err := json.Unmarshal(b, &c); err != nil {
		log.Printf("AVISO: credencial ilegivel em %s: %v", CredentialPath(), err)
		return Credential{}, false
	}
	if c.DeviceID == "" || c.Token == "" {
		return Credential{}, false
	}
	return c, true
}

func Save(c Credential) error {
	caminho := CredentialPath()
	if err := os.MkdirAll(filepath.Dir(caminho), 0o755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(caminho, append(b, '\n'), 0o600)
}

func Enroll(client *http.Client, serverURL, conviteToken, hostname string) (Credential, error) {
	corpo, err := json.Marshal(map[string]string{
		"enrollment_token": conviteToken,
		"machine_id":       machineID(),
		"hostname":         hostname,
		"kind":             "collector",
	})
	if err != nil {
		return Credential{}, err
	}

	req, err := http.NewRequest(http.MethodPost, serverURL+"/api/enroll", bytes.NewReader(corpo))
	if err != nil {
		return Credential{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return Credential{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Credential{}, fmt.Errorf("enrollment recusado (%d): %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}

	var c Credential
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return Credential{}, err
	}
	if c.DeviceID == "" || c.Token == "" {
		return Credential{}, errors.New("painel devolveu credencial incompleta")
	}
	return c, nil
}

func Resolve(client *http.Client, serverURL, hostname string) (Credential, string, error) {
	if c, ok := Load(); ok {
		log.Printf("credencial propria em uso (device=%s unidade=%d)", c.DeviceID, c.SiteID)
		return c, "", nil
	}

	if convite := strings.TrimSpace(os.Getenv("COLLECTOR_ENROLL_TOKEN")); convite != "" {
		c, err := Enroll(client, serverURL, convite, hostname)
		if err != nil {
			return Credential{}, "", fmt.Errorf("enrollment falhou: %w", err)
		}
		if err := Save(c); err != nil {
			log.Printf("AVISO GRAVE: credencial obtida mas NAO gravada em %s (%v). "+
				"O convite ja foi consumido; emita outro antes de reiniciar este coletor.",
				CredentialPath(), err)
		} else {
			log.Printf("enrollment concluido (device=%s unidade=%d). "+
				"Remova COLLECTOR_ENROLL_TOKEN da configuracao.", c.DeviceID, c.SiteID)
		}
		return c, "", nil
	}

	legado := strings.TrimSpace(os.Getenv("COLLECTOR_TOKEN"))
	if legado == "" {
		return Credential{}, "", errors.New("sem identidade: defina COLLECTOR_ENROLL_TOKEN para se cadastrar, " +
			"ou COLLECTOR_TOKEN para o modo compartilhado (em descontinuacao)")
	}
	log.Printf("AVISO: usando COLLECTOR_TOKEN compartilhado, descontinuado. O painel so aceita esse " +
		"token com ALLOW_LEGACY_INGEST_TOKEN=true no .env dele; sem isso todo envio volta 401. " +
		"Migre para credencial propria com COLLECTOR_ENROLL_TOKEN.")
	return Credential{}, legado, nil
}
