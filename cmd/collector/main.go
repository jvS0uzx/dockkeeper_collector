package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jvS0uzx/dockkeeper_collector/internal/config"
	"github.com/jvS0uzx/dockkeeper_collector/internal/identity"
	"github.com/jvS0uzx/dockkeeper_collector/internal/push"
	"github.com/jvS0uzx/dockkeeper_collector/internal/snmp"
	"github.com/jvS0uzx/dockkeeper_collector/scan"
)

var Version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println(Version)
		return
	}

	log.SetPrefix("[collector] ")
	log.SetFlags(log.LstdFlags)

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatalf("configuração inválida: %v", err)
	}

	if cfg.Inseguro {
		log.Printf("AVISO: %s usa http:// e ALLOW_INSECURE_HTTP=true; a credencial do coletor viaja em claro nesta rede", cfg.ServerURL)
	}

	if len(cfg.SNMP.Publicos) > 0 {
		log.Printf("AVISO: SNMP_PERMITIR_PUBLICO=true e alvos SNMP fora da rede privada (%s); a community viaja em claro até eles",
			strings.Join(cfg.SNMP.Publicos, ","))
	}

	log.Printf("v%s unidade=%q faixas=%v destino=%s intervalo=%s",
		Version, cfg.SiteCode, cfg.CIDRs, cfg.ServerURL, cfg.Interval)
	log.Print(resumoSNMP(cfg.SNMP))

	hostname, _ := os.Hostname()
	cred, legado, err := identity.Resolve(&http.Client{Timeout: 30 * time.Second}, cfg.ServerURL, hostname)
	if err != nil {
		log.Fatalf("%v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := push.New(cfg.ServerURL, push.Identity{
		DeviceID:    cred.DeviceID,
		DeviceToken: cred.Token,
		LegacyToken: legado,
	})

	var coletor *snmp.Coletor
	if cfg.SNMP.Ativo() {
		coletor = snmp.NovoColetor(snmp.Config{
			Porta:     cfg.SNMP.Porta,
			Community: string(cfg.SNMP.Community),
			Timeout:   cfg.SNMP.Timeout,
			Retries:   cfg.SNMP.Retries,
		})
	}

	if cfg.Once {
		falhou := false
		if err := cycle(ctx, cfg, client); err != nil {
			log.Printf("ciclo falhou: %v", err)
			falhou = true
		}
		if coletor != nil {
			if err := cicloSNMP(ctx, cfg, coletor, client); err != nil {
				log.Printf("ciclo SNMP falhou: %v", err)
				falhou = true
			}
		}
		if falhou {
			os.Exit(1)
		}
		return
	}

	var wg sync.WaitGroup
	if coletor != nil {
		wg.Go(func() { lacoSNMP(ctx, cfg, coletor, client) })
	}
	defer wg.Wait()

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		if err := cycle(ctx, cfg, client); err != nil {
			log.Printf("ciclo falhou: %v", err)
		}
		select {
		case <-ctx.Done():
			log.Println("encerrado")
			return
		case <-ticker.C:
		}
	}
}

func cycle(ctx context.Context, cfg config.Config, client *push.Client) error {
	started := time.Now()

	hosts, errs := scan.Run(ctx, scan.Config{CIDRs: cfg.CIDRs, Ports: cfg.Ports})
	for _, err := range errs {
		log.Printf("faixa ignorada: %v", err)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	log.Printf("varredura: %d hosts em %s", len(hosts), time.Since(started).Round(time.Millisecond))

	if len(hosts) == 0 && len(errs) > 0 {
		return nil
	}

	return client.Send(ctx, montarPayload(cfg, hosts))
}

func montarPayload(cfg config.Config, hosts []scan.Host) push.Payload {
	return push.Payload{
		SiteCode:         cfg.SiteCode,
		CollectorVersion: Version,
		Hosts:            hosts,

		ReportIntervalSec: int(cfg.Interval / time.Second),
	}
}

func resumoSNMP(s config.SNMP) string {
	if !s.Ativo() {
		return "snmp: desligado (SNMP_TARGETS vazio)"
	}
	return fmt.Sprintf("snmp: v2c alvos=%s porta=%d intervalo=%s timeout=%s retries=%d",
		strings.Join(s.Alvos, ","), s.Porta, s.Intervalo, s.Timeout, s.Retries)
}

func lacoSNMP(ctx context.Context, cfg config.Config, coletor *snmp.Coletor, client *push.Client) {
	ticker := time.NewTicker(cfg.SNMP.Intervalo)
	defer ticker.Stop()

	for {
		if err := cicloSNMP(ctx, cfg, coletor, client); err != nil && ctx.Err() == nil {
			log.Printf("ciclo SNMP falhou: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func cicloSNMP(ctx context.Context, cfg config.Config, coletor *snmp.Coletor, client *push.Client) error {
	inicio := time.Now()
	dispositivos := coletor.Coletar(ctx, cfg.SNMP.Alvos)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	alcancaveis, interfaces := 0, 0
	for _, d := range dispositivos {
		if d.Reachable {
			alcancaveis++
			interfaces += len(d.Interfaces)
			continue
		}
		log.Printf("snmp: %s inalcançável: %s", d.IP, d.Error)
	}
	log.Printf("snmp: %d de %d equipamentos, %d interfaces em %s",
		alcancaveis, len(dispositivos), interfaces, time.Since(inicio).Round(time.Millisecond))

	resposta, err := client.SendMetricasDeRede(ctx, montarMetricas(cfg, inicio, dispositivos))
	if errors.Is(err, push.ErrRecusado) {
		return fmt.Errorf("%w; o lote foi descartado e o próximo ciclo envia leitura nova", err)
	}
	if err != nil {
		return err
	}
	if resposta.Rejeitados > 0 {
		log.Printf("AVISO: o painel aceitou o lote SNMP mas descartou %d de %d equipamentos por dados inválidos; confira SNMP_TARGETS e a versão do coletor",
			resposta.Rejeitados, len(dispositivos))
	}
	return nil
}

func montarMetricas(cfg config.Config, coletadoEm time.Time, dispositivos []snmp.Dispositivo) push.MetricasDeRede {
	return push.MetricasDeRede{
		SiteCode:         cfg.SiteCode,
		CollectorVersion: Version,
		IntervalSec:      int(cfg.SNMP.Intervalo / time.Second),
		CollectedAt:      coletadoEm,
		Devices:          dispositivos,
	}
}
