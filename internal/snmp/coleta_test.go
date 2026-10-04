package snmp

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

const communityAgente = "c0mmun1ty-d0-t3st3"

type relogio struct{ agora time.Time }

func (r *relogio) ler() time.Time { return r.agora }

func coletorDeTeste(porta uint16, community string, r *relogio) *Coletor {
	c := NovoColetor(Config{Porta: porta, Community: community, Timeout: 300 * time.Millisecond, Retries: 0})
	c.agora = r.ler
	return c
}

type contagemSwitch struct {
	uptime                       uint32
	hcIn2, hcOut2                uint64
	in3, out3, inErr3, outDisc3  uint32
	inErr2, outErr2, inDisc2, o2 uint32
}

func tabelaSwitch(c contagemSwitch) []gosnmp.SnmpPDU {
	return []gosnmp.SnmpPDU{
		pduTexto(oidSysDescr, "Switch de teste\x00"),
		pduTicks(oidSysUpTime, c.uptime),
		pduTexto(oidSysName, "sw-core"),

		pduTexto(col(oidIfDescr, 1), "lo"),
		pduTexto(col(oidIfDescr, 2), "GigabitEthernet0/1"),
		pduTexto(col(oidIfDescr, 3), "FastEthernet0/3"),
		pduInteiro(col(oidIfType, 1), 24),
		pduInteiro(col(oidIfType, 2), 6),
		pduInteiro(col(oidIfType, 3), 6),
		pduGauge(col(oidIfSpeed, 1), 10000000),
		pduGauge(col(oidIfSpeed, 2), 4294967295),
		pduGauge(col(oidIfSpeed, 3), 100000000),
		pduInteiro(col(oidIfAdminStatus, 1), 1),
		pduInteiro(col(oidIfAdminStatus, 2), 1),
		pduInteiro(col(oidIfAdminStatus, 3), 99),
		pduInteiro(col(oidIfOperStatus, 1), 1),
		pduInteiro(col(oidIfOperStatus, 2), 1),
		pduInteiro(col(oidIfOperStatus, 3), 7),
		pduC32(col(oidIfInOctets, 1), 1),
		pduC32(col(oidIfInOctets, 2), 1),
		pduC32(col(oidIfInOctets, 3), c.in3),
		pduC32(col(oidIfInDiscards, 2), c.inDisc2),
		pduC32(col(oidIfInErrors, 2), c.inErr2),
		pduC32(col(oidIfInErrors, 3), c.inErr3),
		pduC32(col(oidIfOutOctets, 2), 1),
		pduC32(col(oidIfOutOctets, 3), c.out3),
		pduC32(col(oidIfOutDiscards, 2), c.o2),
		pduC32(col(oidIfOutDiscards, 3), c.outDisc3),
		pduC32(col(oidIfOutErrors, 2), c.outErr2),

		pduTexto(col(oidIfName, 1), "lo"),
		pduTexto(col(oidIfName, 2), "ge-0/0/1"),
		pduC64(col(oidIfHCInOctets, 2), c.hcIn2),
		pduC64(col(oidIfHCOutOctets, 2), c.hcOut2),
		pduGauge(col(oidIfHighSpeed, 2), 1000),
		pduTexto(col(oidIfAlias, 2), "uplink "+strings.Repeat("x", 200)),
	}
}

func TestColetaContraAgenteV2c(t *testing.T) {
	agente := novoAgente(t, communityAgente)
	r := &relogio{agora: time.Date(2026, 10, 3, 15, 0, 0, 0, time.UTC)}
	coletor := coletorDeTeste(agente.porta(), communityAgente, r)

	agente.definir(tabelaSwitch(contagemSwitch{
		uptime: 100000, hcIn2: 1 << 40, hcOut2: 5000,
		in3: 4294967000, out3: 10, inErr3: 7, outDisc3: 0,
		inErr2: 1, outErr2: 2, inDisc2: 3, o2: 4,
	})...)

	primeira := coletor.Coletar(context.Background(), []string{"127.0.0.1"})
	if len(primeira) != 1 || !primeira[0].Reachable {
		t.Fatalf("primeira coleta: %+v", primeira)
	}
	d := primeira[0]
	if d.SysName != "sw-core" || d.SysDescr != "Switch de teste" {
		t.Errorf("sys_name/sys_descr = %q/%q", d.SysName, d.SysDescr)
	}
	if d.UptimeSec == nil || *d.UptimeSec != 1000 {
		t.Errorf("uptime_sec = %v, esperado 1000", d.UptimeSec)
	}
	if len(d.Interfaces) != 2 || d.Interfaces[0].IfIndex != 2 || d.Interfaces[1].IfIndex != 3 {
		t.Fatalf("interfaces = %+v, esperado 2 e 3 sem o loopback", d.Interfaces)
	}
	for _, itf := range d.Interfaces {
		if itf.InBps != nil || itf.OutBps != nil || itf.InErrors != nil || itf.OutDiscards != nil {
			t.Errorf("primeira amostra de %d deveria ser nula: %+v", itf.IfIndex, itf)
		}
	}

	r.agora = r.agora.Add(60 * time.Second)
	agente.definir(tabelaSwitch(contagemSwitch{
		uptime: 106000, hcIn2: 1<<40 + 7_500_000, hcOut2: 5000,
		in3: 704, out3: 760, inErr3: 9, outDisc3: 0,
		inErr2: 1, outErr2: 5, inDisc2: 3, o2: 4,
	})...)

	segunda := coletor.Coletar(context.Background(), []string{"127.0.0.1"})[0]
	ge, fa := segunda.Interfaces[0], segunda.Interfaces[1]

	if ge.IfName != "ge-0/0/1" || ge.IfDescr != "GigabitEthernet0/1" || !strings.HasPrefix(ge.IfAlias, "uplink") {
		t.Errorf("nomes de ge = %q/%q/%q", ge.IfName, ge.IfDescr, ge.IfAlias)
	}
	if n := len([]rune(ge.IfAlias)); n != 128 {
		t.Errorf("if_alias com %d caracteres, esperado o limite 128", n)
	}
	if ge.SpeedMbps == nil || *ge.SpeedMbps != 1000 {
		t.Errorf("speed_mbps de ge = %v, esperado 1000 de ifHighSpeed", ge.SpeedMbps)
	}
	if ge.InBps == nil || *ge.InBps != 1_000_000 {
		t.Errorf("in_bps de ge = %v, esperado 1e6 pelos contadores HC", ge.InBps)
	}
	if ge.OutBps == nil || *ge.OutBps != 0 {
		t.Errorf("out_bps de ge = %v, esperado 0", ge.OutBps)
	}
	if ge.OutErrors == nil || *ge.OutErrors != 3 || ge.InErrors == nil || *ge.InErrors != 0 {
		t.Errorf("erros de ge = in %v out %v", ge.InErrors, ge.OutErrors)
	}
	if ge.OperStatus != "up" || ge.AdminStatus != "up" {
		t.Errorf("status de ge = %s/%s", ge.OperStatus, ge.AdminStatus)
	}

	if fa.IfName != "" || fa.IfDescr != "FastEthernet0/3" {
		t.Errorf("nomes de fa = %q/%q", fa.IfName, fa.IfDescr)
	}
	if fa.SpeedMbps == nil || *fa.SpeedMbps != 100 {
		t.Errorf("speed_mbps de fa = %v, esperado 100 de ifSpeed", fa.SpeedMbps)
	}
	if fa.InBps == nil || *fa.InBps != 1000*8/60.0 {
		t.Errorf("in_bps de fa = %v, esperada a volta de 2^32", fa.InBps)
	}
	if fa.OutBps == nil || *fa.OutBps != 750*8/60.0 {
		t.Errorf("out_bps de fa = %v", fa.OutBps)
	}
	if fa.InErrors == nil || *fa.InErrors != 2 {
		t.Errorf("in_errors de fa = %v, esperado 2", fa.InErrors)
	}
	if fa.InDiscards != nil || fa.OutErrors != nil {
		t.Errorf("contador ausente em fa deveria ser nulo: in_discards %v out_errors %v", fa.InDiscards, fa.OutErrors)
	}
	if fa.OperStatus != "lowerLayerDown" || fa.AdminStatus != "unknown" {
		t.Errorf("status de fa = %s/%s", fa.OperStatus, fa.AdminStatus)
	}

	r.agora = r.agora.Add(60 * time.Second)
	agente.definir(tabelaSwitch(contagemSwitch{uptime: 50, hcIn2: 10, in3: 10})...)
	reiniciado := coletor.Coletar(context.Background(), []string{"127.0.0.1"})[0]
	for _, itf := range reiniciado.Interfaces {
		if itf.InBps != nil || itf.InErrors != nil {
			t.Errorf("após reinício (sysUpTime menor), %d deveria ser nulo: %+v", itf.IfIndex, itf)
		}
	}
}

func TestEquipamentoSemIfXTableUsaContadores32(t *testing.T) {
	agente := novoAgente(t, communityAgente)
	r := &relogio{agora: time.Date(2026, 10, 3, 15, 0, 0, 0, time.UTC)}
	coletor := coletorDeTeste(agente.porta(), communityAgente, r)

	tabela := func(octetos uint32) []gosnmp.SnmpPDU {
		return []gosnmp.SnmpPDU{
			pduTexto(oidSysName, "antigo"),
			pduTexto(col(oidIfDescr, 1), "eth0"),
			pduInteiro(col(oidIfType, 1), 6),
			pduInteiro(col(oidIfOperStatus, 1), 2),
			pduC32(col(oidIfInOctets, 1), octetos),
		}
	}
	agente.definir(tabela(0)...)
	coletor.Coletar(context.Background(), []string{"127.0.0.1"})
	r.agora = r.agora.Add(10 * time.Second)
	agente.definir(tabela(1250)...)
	d := coletor.Coletar(context.Background(), []string{"127.0.0.1"})[0]

	if !d.Reachable || d.UptimeSec != nil {
		t.Fatalf("dispositivo = %+v", d)
	}
	itf := d.Interfaces[0]
	if itf.InBps == nil || *itf.InBps != 1000 {
		t.Errorf("in_bps = %v, esperado 1000", itf.InBps)
	}
	if itf.SpeedMbps != nil || itf.OutBps != nil || itf.AdminStatus != "unknown" || itf.OperStatus != "down" {
		t.Errorf("campos não medidos deveriam ser nulos/unknown: %+v", itf)
	}
}

func portaFechada(t *testing.T) uint16 {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	porta := uint16(conn.LocalAddr().(*net.UDPAddr).Port)
	conn.Close()
	return porta
}

func TestAlvoInalcancavelNaoDerrubaOsOutros(t *testing.T) {
	agente := novoAgente(t, communityAgente)
	agente.definir(pduTexto(oidSysName, "vivo"))
	r := &relogio{agora: time.Now()}

	casos := []struct {
		nome    string
		coletor *Coletor
		alvos   []string
		trecho  string
	}{
		{"community errada", coletorDeTeste(agente.porta(), "errada-"+communityAgente, r), []string{"127.0.0.1"}, "sem resposta SNMP"},
		{"porta fechada", coletorDeTeste(portaFechada(t), communityAgente, r), []string{"127.0.0.1"}, "recusada"},
	}
	for _, caso := range casos {
		t.Run(caso.nome, func(t *testing.T) {
			d := caso.coletor.Coletar(context.Background(), caso.alvos)[0]
			if d.Reachable || d.Error == "" {
				t.Fatalf("esperava inalcançável com erro: %+v", d)
			}
			if d.Interfaces == nil || len(d.Interfaces) != 0 || d.UptimeSec != nil || d.SysName != "" {
				t.Errorf("inalcançável deveria vir vazio: %+v", d)
			}
			if len([]rune(d.Error)) > limiteErro {
				t.Errorf("erro longo demais: %q", d.Error)
			}
			if !strings.Contains(d.Error, caso.trecho) {
				t.Errorf("erro = %q, esperado conter %q", d.Error, caso.trecho)
			}
			if strings.Contains(d.Error, communityAgente) {
				t.Errorf("community vazou no erro: %q", d.Error)
			}
			bruto, _ := json.Marshal(d)
			if strings.Contains(string(bruto), communityAgente) {
				t.Errorf("community vazou no JSON: %s", bruto)
			}
		})
	}

	misto := coletorDeTeste(agente.porta(), communityAgente, r)
	saida := misto.Coletar(context.Background(), []string{"127.0.0.2", "127.0.0.1", "127.0.0.3", "127.0.0.4", "127.0.0.5", "127.0.0.6"})
	if !saida[1].Reachable || saida[1].SysName != "vivo" {
		t.Errorf("alvo vivo afetado pelos mortos: %+v", saida[1])
	}
	for i, d := range saida {
		if i != 1 && (d.Reachable || d.Error == "") {
			t.Errorf("%s: %+v", d.IP, d)
		}
	}
}

func TestContextoCanceladoEncerraAColeta(t *testing.T) {
	ctx, cancelar := context.WithCancel(context.Background())
	cancelar()
	r := &relogio{agora: time.Now()}
	saida := coletorDeTeste(portaFechada(t), communityAgente, r).Coletar(ctx, []string{"192.0.2.1", "192.0.2.2"})
	for _, d := range saida {
		if d.Reachable || d.Error == "" {
			t.Errorf("%s: %+v", d.IP, d)
		}
	}
}
