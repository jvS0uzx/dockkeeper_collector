package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jvS0uzx/dockkeeper_collector/internal/config"
	"github.com/jvS0uzx/dockkeeper_collector/internal/identity"
	"github.com/jvS0uzx/dockkeeper_collector/internal/push"
	"github.com/jvS0uzx/dockkeeper_collector/scan"
)

const Version = "1.0.0"

func main() {
	log.SetPrefix("[collector] ")
	log.SetFlags(log.LstdFlags)

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Fatalf("configuração inválida: %v", err)
	}

	if cfg.Inseguro {
		log.Printf("AVISO: %s usa http:// e ALLOW_INSECURE_HTTP=true; a credencial do coletor viaja em claro nesta rede", cfg.ServerURL)
	}

	log.Printf("v%s unidade=%q faixas=%v destino=%s intervalo=%s",
		Version, cfg.SiteCode, cfg.CIDRs, cfg.ServerURL, cfg.Interval)

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

	if cfg.Once {
		if err := cycle(ctx, cfg, client); err != nil {
			log.Fatalf("ciclo falhou: %v", err)
		}
		return
	}

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
