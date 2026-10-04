package snmp

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gosnmp/gosnmp"
)

const (
	oidSysDescr  = ".1.3.6.1.2.1.1.1.0"
	oidSysUpTime = ".1.3.6.1.2.1.1.3.0"
	oidSysName   = ".1.3.6.1.2.1.1.5.0"

	oidIfDescr       = ".1.3.6.1.2.1.2.2.1.2"
	oidIfType        = ".1.3.6.1.2.1.2.2.1.3"
	oidIfSpeed       = ".1.3.6.1.2.1.2.2.1.5"
	oidIfAdminStatus = ".1.3.6.1.2.1.2.2.1.7"
	oidIfOperStatus  = ".1.3.6.1.2.1.2.2.1.8"
	oidIfInOctets    = ".1.3.6.1.2.1.2.2.1.10"
	oidIfInDiscards  = ".1.3.6.1.2.1.2.2.1.13"
	oidIfInErrors    = ".1.3.6.1.2.1.2.2.1.14"
	oidIfOutOctets   = ".1.3.6.1.2.1.2.2.1.16"
	oidIfOutDiscards = ".1.3.6.1.2.1.2.2.1.19"
	oidIfOutErrors   = ".1.3.6.1.2.1.2.2.1.20"
	oidIfName        = ".1.3.6.1.2.1.31.1.1.1.1"
	oidIfHCInOctets  = ".1.3.6.1.2.1.31.1.1.1.6"
	oidIfHCOutOctets = ".1.3.6.1.2.1.31.1.1.1.10"
	oidIfHighSpeed   = ".1.3.6.1.2.1.31.1.1.1.15"
	oidIfAlias       = ".1.3.6.1.2.1.31.1.1.1.18"

	tipoSoftwareLoopback = 24

	Concorrencia   = 4
	maxRepeticoes  = 25
	limiteSysDescr = 255
	limiteNome     = 128
	limiteErro     = 200
)

var nomesDeStatus = map[uint64]string{
	1: "up",
	2: "down",
	3: "testing",
	4: "unknown",
	5: "dormant",
	6: "notPresent",
	7: "lowerLayerDown",
}

type Interface struct {
	IfIndex     int      `json:"if_index"`
	IfName      string   `json:"if_name"`
	IfDescr     string   `json:"if_descr"`
	IfAlias     string   `json:"if_alias"`
	SpeedMbps   *float64 `json:"speed_mbps"`
	OperStatus  string   `json:"oper_status"`
	AdminStatus string   `json:"admin_status"`
	InBps       *float64 `json:"in_bps"`
	OutBps      *float64 `json:"out_bps"`
	InErrors    *uint64  `json:"in_errors"`
	OutErrors   *uint64  `json:"out_errors"`
	InDiscards  *uint64  `json:"in_discards"`
	OutDiscards *uint64  `json:"out_discards"`
}

type Dispositivo struct {
	IP         string      `json:"ip"`
	Reachable  bool        `json:"reachable"`
	Error      string      `json:"error"`
	SysName    string      `json:"sys_name"`
	SysDescr   string      `json:"sys_descr"`
	UptimeSec  *uint64     `json:"uptime_sec"`
	Interfaces []Interface `json:"interfaces"`
}

type Config struct {
	Porta     uint16
	Community string
	Timeout   time.Duration
	Retries   int
}

type Coletor struct {
	cfg   Config
	calc  *Calculadora
	agora func() time.Time
}

func NovoColetor(cfg Config) *Coletor {
	return &Coletor{cfg: cfg, calc: NovaCalculadora(), agora: time.Now}
}

func (c *Coletor) Coletar(ctx context.Context, alvos []string) []Dispositivo {
	saida := make([]Dispositivo, len(alvos))
	vagas := make(chan struct{}, Concorrencia)
	var wg sync.WaitGroup
	for i, ip := range alvos {
		wg.Go(func() {
			select {
			case vagas <- struct{}{}:
			case <-ctx.Done():
				saida[i] = c.inalcancavel(ip, ctx.Err())
				return
			}
			defer func() { <-vagas }()
			saida[i] = c.coletarUm(ctx, ip)
		})
	}
	wg.Wait()
	return saida
}

type leitura struct {
	tipo      *uint64
	nome      string
	descr     string
	alias     string
	ifSpeed   *uint64
	highSpeed *uint64
	admin     *uint64
	oper      *uint64

	entradaHC contador
	saidaHC   contador
	contadores
}

func (c *Coletor) coletarUm(ctx context.Context, ip string) Dispositivo {
	cliente := &gosnmp.GoSNMP{
		Target:         ip,
		Port:           c.cfg.Porta,
		Transport:      "udp",
		Community:      c.cfg.Community,
		Version:        gosnmp.Version2c,
		Timeout:        c.cfg.Timeout,
		Retries:        c.cfg.Retries,
		Context:        ctx,
		MaxRepetitions: maxRepeticoes,
	}
	if err := cliente.Connect(); err != nil {
		return c.inalcancavel(ip, err)
	}
	defer cliente.Conn.Close()

	resp, err := cliente.Get([]string{oidSysName, oidSysDescr, oidSysUpTime})
	if err != nil {
		return c.inalcancavel(ip, err)
	}

	d := Dispositivo{IP: ip, Reachable: true, Interfaces: []Interface{}}
	var uptime *uint32
	for _, pdu := range resp.Variables {
		switch normalizarOID(pdu.Name) {
		case oidSysName:
			d.SysName = texto(pdu, limiteNome)
		case oidSysDescr:
			d.SysDescr = texto(pdu, limiteSysDescr)
		case oidSysUpTime:
			if v, ok := numero(pdu); ok && v <= 0xFFFFFFFF {
				ticks := uint32(v)
				uptime = &ticks
				segundos := v / 100
				d.UptimeSec = &segundos
			}
		}
	}

	leituras := map[int]*leitura{}
	obter := func(indice int) *leitura {
		l, ok := leituras[indice]
		if !ok {
			l = &leitura{}
			leituras[indice] = l
		}
		return l
	}

	colunas := []struct {
		oid      string
		registra func(*leitura, gosnmp.SnmpPDU)
	}{
		{oidIfHCInOctets, func(l *leitura, p gosnmp.SnmpPDU) { l.entradaHC = paraContador(p) }},
		{oidIfHCOutOctets, func(l *leitura, p gosnmp.SnmpPDU) { l.saidaHC = paraContador(p) }},
		{oidIfInOctets, func(l *leitura, p gosnmp.SnmpPDU) { l.entrada = paraContador(p) }},
		{oidIfOutOctets, func(l *leitura, p gosnmp.SnmpPDU) { l.saida = paraContador(p) }},
		{oidIfInErrors, func(l *leitura, p gosnmp.SnmpPDU) { l.errosEntrada = paraContador(p) }},
		{oidIfOutErrors, func(l *leitura, p gosnmp.SnmpPDU) { l.errosSaida = paraContador(p) }},
		{oidIfInDiscards, func(l *leitura, p gosnmp.SnmpPDU) { l.descartesEntrada = paraContador(p) }},
		{oidIfOutDiscards, func(l *leitura, p gosnmp.SnmpPDU) { l.descartesSaida = paraContador(p) }},
		{oidIfDescr, func(l *leitura, p gosnmp.SnmpPDU) { l.descr = texto(p, limiteNome) }},
		{oidIfType, func(l *leitura, p gosnmp.SnmpPDU) { l.tipo = ponteiro(numero(p)) }},
		{oidIfSpeed, func(l *leitura, p gosnmp.SnmpPDU) { l.ifSpeed = ponteiro(numero(p)) }},
		{oidIfAdminStatus, func(l *leitura, p gosnmp.SnmpPDU) { l.admin = ponteiro(numero(p)) }},
		{oidIfOperStatus, func(l *leitura, p gosnmp.SnmpPDU) { l.oper = ponteiro(numero(p)) }},
		{oidIfName, func(l *leitura, p gosnmp.SnmpPDU) { l.nome = texto(p, limiteNome) }},
		{oidIfHighSpeed, func(l *leitura, p gosnmp.SnmpPDU) { l.highSpeed = ponteiro(numero(p)) }},
		{oidIfAlias, func(l *leitura, p gosnmp.SnmpPDU) { l.alias = texto(p, limiteNome) }},
	}

	em := c.agora()
	for _, coluna := range colunas {
		err := cliente.BulkWalk(coluna.oid, func(pdu gosnmp.SnmpPDU) error {
			sufixo, ok := strings.CutPrefix(normalizarOID(pdu.Name), coluna.oid+".")
			if !ok {
				return nil
			}
			indice, err := strconv.Atoi(sufixo)
			if err != nil || indice <= 0 {
				return nil
			}
			coluna.registra(obter(indice), pdu)
			return nil
		})
		if err != nil {
			return c.inalcancavel(ip, err)
		}
	}

	contagem := map[int]contadores{}
	for indice, l := range leituras {
		if l.tipo != nil && *l.tipo == tipoSoftwareLoopback {
			delete(leituras, indice)
			continue
		}
		if l.entradaHC.ok {
			l.entrada = l.entradaHC
		}
		if l.saidaHC.ok {
			l.saida = l.saidaHC
		}
		contagem[indice] = l.contadores
	}

	taxas := c.calc.aplicar(ip, uptime, em, contagem)

	indices := make([]int, 0, len(leituras))
	for indice := range leituras {
		indices = append(indices, indice)
	}
	sort.Ints(indices)

	for _, indice := range indices {
		l := leituras[indice]
		t := taxas[indice]
		d.Interfaces = append(d.Interfaces, Interface{
			IfIndex:     indice,
			IfName:      l.nome,
			IfDescr:     l.descr,
			IfAlias:     l.alias,
			SpeedMbps:   velocidade(l.highSpeed, l.ifSpeed),
			OperStatus:  status(l.oper),
			AdminStatus: status(l.admin),
			InBps:       t.EntradaBps,
			OutBps:      t.SaidaBps,
			InErrors:    t.ErrosEntrada,
			OutErrors:   t.ErrosSaida,
			InDiscards:  t.DescartesEntrada,
			OutDiscards: t.DescartesSaida,
		})
	}
	return d
}

func (c *Coletor) inalcancavel(ip string, err error) Dispositivo {
	return Dispositivo{
		IP:         ip,
		Reachable:  false,
		Error:      c.mensagem(err),
		Interfaces: []Interface{},
	}
}

func (c *Coletor) mensagem(err error) string {
	bruta := err.Error()
	var msg string
	switch {
	case errors.Is(err, context.Canceled):
		msg = "coleta interrompida pelo encerramento do coletor"
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(bruta, "timeout"):
		msg = "sem resposta SNMP no tempo limite: equipamento desligado, community errada ou ACL bloqueando o coletor"
	case strings.Contains(bruta, "refused"):
		msg = "conexão recusada: o equipamento não aceita SNMP nesta porta"
	default:
		msg = "falha SNMP: " + bruta
	}
	if c.cfg.Community != "" {
		msg = strings.ReplaceAll(msg, c.cfg.Community, "<COMMUNITY>")
	}
	return limitar(msg, limiteErro)
}

func normalizarOID(oid string) string {
	if strings.HasPrefix(oid, ".") {
		return oid
	}
	return "." + oid
}

func numero(pdu gosnmp.SnmpPDU) (uint64, bool) {
	switch pdu.Type {
	case gosnmp.Integer, gosnmp.Counter32, gosnmp.Gauge32, gosnmp.TimeTicks, gosnmp.Counter64, gosnmp.Uinteger32:
	default:
		return 0, false
	}
	v := gosnmp.ToBigInt(pdu.Value)
	if v.Sign() < 0 || !v.IsUint64() {
		return 0, false
	}
	return v.Uint64(), true
}

func paraContador(pdu gosnmp.SnmpPDU) contador {
	v, ok := numero(pdu)
	if !ok {
		return contador{}
	}
	bits := 32
	if pdu.Type == gosnmp.Counter64 {
		bits = 64
	}
	return contador{valor: v, bits: bits, ok: true}
}

func ponteiro(v uint64, ok bool) *uint64 {
	if !ok {
		return nil
	}
	return &v
}

func texto(pdu gosnmp.SnmpPDU, limite int) string {
	var s string
	switch v := pdu.Value.(type) {
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		return ""
	}
	s = strings.ToValidUTF8(s, "�")
	s = strings.TrimSpace(strings.Trim(s, "\x00"))
	return limitar(s, limite)
}

func limitar(s string, limite int) string {
	if utf8.RuneCountInString(s) <= limite {
		return s
	}
	return string([]rune(s)[:limite])
}

func velocidade(highSpeed, ifSpeed *uint64) *float64 {
	if highSpeed != nil && *highSpeed > 0 {
		v := float64(*highSpeed)
		return &v
	}
	if ifSpeed != nil {
		v := float64(*ifSpeed) / 1e6
		return &v
	}
	return nil
}

func status(v *uint64) string {
	if v == nil {
		return "unknown"
	}
	if nome, ok := nomesDeStatus[*v]; ok {
		return nome
	}
	return "unknown"
}
