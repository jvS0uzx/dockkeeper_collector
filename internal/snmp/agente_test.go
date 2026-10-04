package snmp

import (
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gosnmp/gosnmp"
)

type agenteFalso struct {
	conn      *net.UDPConn
	community string

	mu      sync.Mutex
	ordem   []string
	valores map[string]gosnmp.SnmpPDU

	requisicoes atomic.Int32
}

func novoAgente(t *testing.T, community string) *agenteFalso {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	a := &agenteFalso{conn: conn, community: community, valores: map[string]gosnmp.SnmpPDU{}}
	t.Cleanup(func() { conn.Close() })
	go a.atender()
	return a
}

func (a *agenteFalso) porta() uint16 {
	return uint16(a.conn.LocalAddr().(*net.UDPAddr).Port)
}

func (a *agenteFalso) definir(pdus ...gosnmp.SnmpPDU) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.valores = map[string]gosnmp.SnmpPDU{}
	a.ordem = a.ordem[:0]
	for _, p := range pdus {
		a.valores[p.Name] = p
		a.ordem = append(a.ordem, p.Name)
	}
	sort.Slice(a.ordem, func(i, j int) bool { return compararOID(a.ordem[i], a.ordem[j]) < 0 })
}

func (a *agenteFalso) atender() {
	buf := make([]byte, 65535)
	decodificador := &gosnmp.GoSNMP{Version: gosnmp.Version2c}
	for {
		n, origem, err := a.conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		a.requisicoes.Add(1)
		pedido, err := decodificador.SnmpDecodePacket(buf[:n])
		if err != nil || pedido.Community != a.community {
			continue
		}
		resposta := &gosnmp.SnmpPacket{
			Version:   gosnmp.Version2c,
			Community: pedido.Community,
			PDUType:   gosnmp.GetResponse,
			RequestID: pedido.RequestID,
			Variables: a.responder(pedido),
		}
		saida, err := resposta.MarshalMsg()
		if err != nil {
			continue
		}
		_, _ = a.conn.WriteToUDP(saida, origem)
	}
}

func (a *agenteFalso) responder(pedido *gosnmp.SnmpPacket) []gosnmp.SnmpPDU {
	a.mu.Lock()
	defer a.mu.Unlock()
	var vars []gosnmp.SnmpPDU
	for _, v := range pedido.Variables {
		switch pedido.PDUType {
		case gosnmp.GetRequest:
			if p, ok := a.valores[v.Name]; ok {
				vars = append(vars, p)
			} else {
				vars = append(vars, gosnmp.SnmpPDU{Name: v.Name, Type: gosnmp.NoSuchObject})
			}
		case gosnmp.GetNextRequest, gosnmp.GetBulkRequest:
			limite := 1
			if pedido.PDUType == gosnmp.GetBulkRequest {
				limite = int(pedido.MaxRepetitions)
			}
			proximos := a.seguintes(v.Name, limite)
			if len(proximos) == 0 {
				proximos = []gosnmp.SnmpPDU{{Name: v.Name, Type: gosnmp.EndOfMibView}}
			}
			vars = append(vars, proximos...)
		}
	}
	return vars
}

func (a *agenteFalso) seguintes(oid string, limite int) []gosnmp.SnmpPDU {
	i := sort.Search(len(a.ordem), func(i int) bool { return compararOID(a.ordem[i], oid) > 0 })
	var out []gosnmp.SnmpPDU
	for ; i < len(a.ordem) && len(out) < limite; i++ {
		out = append(out, a.valores[a.ordem[i]])
	}
	return out
}

func compararOID(x, y string) int {
	px := strings.Split(strings.TrimPrefix(x, "."), ".")
	py := strings.Split(strings.TrimPrefix(y, "."), ".")
	for i := 0; i < len(px) && i < len(py); i++ {
		a, _ := strconv.ParseUint(px[i], 10, 64)
		b, _ := strconv.ParseUint(py[i], 10, 64)
		if a != b {
			if a < b {
				return -1
			}
			return 1
		}
	}
	return len(px) - len(py)
}

func pduTexto(oid, v string) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: oid, Type: gosnmp.OctetString, Value: []byte(v)}
}

func pduInteiro(oid string, v int) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: oid, Type: gosnmp.Integer, Value: v}
}

func pduGauge(oid string, v uint32) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: oid, Type: gosnmp.Gauge32, Value: v}
}

func pduC32(oid string, v uint32) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: oid, Type: gosnmp.Counter32, Value: v}
}

func pduC64(oid string, v uint64) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: oid, Type: gosnmp.Counter64, Value: v}
}

func pduTicks(oid string, v uint32) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: oid, Type: gosnmp.TimeTicks, Value: v}
}

func col(oid string, indice int) string {
	return oid + "." + strconv.Itoa(indice)
}
