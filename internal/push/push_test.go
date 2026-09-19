package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func clienteDeTeste(url string, id Identity) *Client {
	c := New(url, id)
	c.delay = time.Millisecond
	return c
}

func TestEnviaHeadersDaCredencialPropria(t *testing.T) {
	var got http.Header
	var body Payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := clienteDeTeste(srv.URL, Identity{DeviceID: "dev-1", DeviceToken: "segredo-1"})
	err := c.Send(context.Background(), Payload{SiteCode: "norte", CollectorVersion: "1.0.0"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.Get("X-Device-Id") != "dev-1" || got.Get("X-Device-Token") != "segredo-1" {
		t.Errorf("headers de dispositivo = %q/%q", got.Get("X-Device-Id"), got.Get("X-Device-Token"))
	}
	if got.Get("X-Agent-Token") != "" {
		t.Errorf("X-Agent-Token não deveria ir junto da credencial própria")
	}
	if body.SiteCode != "norte" {
		t.Errorf("site_code = %q", body.SiteCode)
	}
}

func TestEnviaTokenLegadoSemCredencial(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := clienteDeTeste(srv.URL, Identity{LegacyToken: "token-legado"})
	if err := c.Send(context.Background(), Payload{}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got.Get("X-Agent-Token") != "token-legado" {
		t.Errorf("X-Agent-Token = %q", got.Get("X-Agent-Token"))
	}
	if got.Get("X-Device-Id") != "" || got.Get("X-Device-Token") != "" {
		t.Errorf("headers de dispositivo deveriam estar vazios no modo legado")
	}
}

func contarRequisicoes(t *testing.T, status int) (int32, error) {
	t.Helper()
	var chamadas atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamadas.Add(1)
		w.WriteHeader(status)
	}))
	defer srv.Close()

	err := clienteDeTeste(srv.URL, Identity{DeviceID: "d", DeviceToken: "t"}).Send(context.Background(), Payload{})
	return chamadas.Load(), err
}

func TestTransitorioRepeteTresVezes(t *testing.T) {
	for _, status := range []int{
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusTooManyRequests,
	} {
		n, err := contarRequisicoes(t, status)
		if n != int32(maxAttempts) {
			t.Errorf("HTTP %d: %d requisições, esperado %d", status, n, maxAttempts)
		}
		if err == nil || errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrRecusado) {
			t.Errorf("HTTP %d: erro = %v, esperada falha transitória esgotada", status, err)
		}
	}
}

func TestRecusaDefinitivaFazUmaTentativaSo(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusRequestEntityTooLarge,
		http.StatusUnprocessableEntity,
	} {
		n, err := contarRequisicoes(t, status)
		if n != 1 {
			t.Errorf("HTTP %d: %d requisições, esperado 1", status, n)
		}
		if !errors.Is(err, ErrRecusado) || errors.Is(err, ErrUnauthorized) {
			t.Errorf("HTTP %d: erro = %v, esperado ErrRecusado", status, err)
		}
		if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status)) {
			t.Errorf("HTTP %d: mensagem sem o status: %v", status, err)
		}
	}
}

func TestCredencialRecusadaNaoRepete(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		var chamadas atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			chamadas.Add(1)
			w.WriteHeader(status)
		}))

		c := clienteDeTeste(srv.URL, Identity{LegacyToken: "x"})
		err := c.Send(context.Background(), Payload{})
		srv.Close()

		if !errors.Is(err, ErrUnauthorized) {
			t.Errorf("HTTP %d: erro = %v, esperado ErrUnauthorized", status, err)
		}
		if n := chamadas.Load(); n != 1 {
			t.Errorf("HTTP %d: %d tentativas, esperado 1", status, n)
		}
	}
}

func TestRepeteEmFalhaTransitoriaEEntrega(t *testing.T) {
	var chamadas atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if chamadas.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := clienteDeTeste(srv.URL, Identity{LegacyToken: "x"})
	if err := c.Send(context.Background(), Payload{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if n := chamadas.Load(); n != 3 {
		t.Errorf("%d tentativas, esperado 3", n)
	}
}

func TestDesisteAposEsgotarTentativas(t *testing.T) {
	var chamadas atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamadas.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := clienteDeTeste(srv.URL, Identity{LegacyToken: "x"})
	err := c.Send(context.Background(), Payload{})
	if err == nil || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("erro = %v, esperada falha transitória esgotada", err)
	}
	if !strings.Contains(err.Error(), "3 tentativas") {
		t.Errorf("mensagem sem o total de tentativas: %v", err)
	}
	if n := chamadas.Load(); n != int32(maxAttempts) {
		t.Errorf("%d tentativas, esperado %d", n, maxAttempts)
	}
}

func TestFalhaDeRedeNaoEhCredencialRecusada(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := clienteDeTeste(url, Identity{LegacyToken: "x"})
	err := c.Send(context.Background(), Payload{})
	if err == nil {
		t.Fatal("esperava erro com o painel fora do ar")
	}
	if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrRecusado) {
		t.Errorf("falha de rede classificada como recusa: %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d tentativas", maxAttempts)) {
		t.Errorf("falha de rede deveria ser repetida %d vezes: %v", maxAttempts, err)
	}
}

func TestSegredosNaoVazamNaMensagemDeErro(t *testing.T) {
	casos := []struct {
		nome    string
		id      Identity
		segredo string
		marca   string
	}{
		{"token legado", Identity{LegacyToken: "tok3n-sup3r-s3cr3to"}, "tok3n-sup3r-s3cr3to", "<COLLECTOR_TOKEN>"},
		{"credencial propria", Identity{DeviceID: "dev", DeviceToken: "s3gr3do-d3-d3vic3"}, "s3gr3do-d3-d3vic3", "<CREDENCIAL>"},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			c := clienteDeTeste("http://"+caso.segredo+".invalid:0", caso.id)
			err := c.Send(context.Background(), Payload{})
			if err == nil {
				t.Fatal("esperava erro de rede")
			}
			if strings.Contains(err.Error(), caso.segredo) {
				t.Fatalf("segredo vazou na mensagem: %v", err)
			}
			if !strings.Contains(err.Error(), caso.marca) {
				t.Errorf("mensagem sem a marca de redação %q: %v", caso.marca, err)
			}
		})
	}
}

func TestRecusaDoTokenLegadoExplicaAFlagDoPainel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := clienteDeTeste(srv.URL, Identity{LegacyToken: "x"}).Send(context.Background(), Payload{})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("erro = %v, esperado ErrUnauthorized", err)
	}
	if !strings.Contains(err.Error(), "ALLOW_LEGACY_INGEST_TOKEN=true") || !strings.Contains(err.Error(), "COLLECTOR_ENROLL_TOKEN") {
		t.Errorf("mensagem não orienta a migração: %v", err)
	}
}

func TestRecusaDeCredencialPropriaNaoFalaDeTokenLegado(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := clienteDeTeste(srv.URL, Identity{DeviceID: "d", DeviceToken: "t"}).Send(context.Background(), Payload{})
	if strings.Contains(err.Error(), "ALLOW_LEGACY_INGEST_TOKEN") {
		t.Errorf("credencial própria recebeu orientação do modo legado: %v", err)
	}
}
