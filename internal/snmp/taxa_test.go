package snmp

import (
	"testing"
	"time"
)

func c32(v uint64) contador { return contador{valor: v, bits: 32, ok: true} }

func c64(v uint64) contador { return contador{valor: v, bits: 64, ok: true} }

func ticks(v uint32) *uint32 { return &v }

var inicio = time.Date(2026, 10, 3, 15, 0, 0, 0, time.UTC)

func leituraUnica(entrada, saida, erros contador) map[int]contadores {
	return map[int]contadores{1: {
		entrada:          entrada,
		saida:            saida,
		errosEntrada:     erros,
		errosSaida:       erros,
		descartesEntrada: erros,
		descartesSaida:   erros,
	}}
}

func TestPrimeiraAmostraNaoTemTaxa(t *testing.T) {
	calc := NovaCalculadora()
	r := calc.aplicar("192.0.2.1", ticks(100), inicio, leituraUnica(c64(1000), c64(2000), c32(5)))[1]
	if r.EntradaBps != nil || r.SaidaBps != nil || r.ErrosEntrada != nil || r.DescartesSaida != nil {
		t.Fatalf("primeira amostra deveria ser toda nula: %+v", r)
	}
}

func TestTaxaEDeltasEntreAmostras(t *testing.T) {
	calc := NovaCalculadora()
	calc.aplicar("192.0.2.1", ticks(100), inicio, leituraUnica(c64(1000), c64(2000), c32(5)))
	r := calc.aplicar("192.0.2.1", ticks(6100), inicio.Add(60*time.Second), leituraUnica(c64(1000+7500), c64(2000), c32(8)))[1]

	if r.EntradaBps == nil || *r.EntradaBps != 1000 {
		t.Errorf("in_bps = %v, esperado 1000", r.EntradaBps)
	}
	if r.SaidaBps == nil || *r.SaidaBps != 0 {
		t.Errorf("out_bps = %v, esperado 0 medido, não nulo", r.SaidaBps)
	}
	if r.ErrosEntrada == nil || *r.ErrosEntrada != 3 || r.DescartesEntrada == nil || *r.DescartesEntrada != 3 {
		t.Errorf("deltas de erro/descarte = %v/%v, esperado 3", r.ErrosEntrada, r.DescartesEntrada)
	}
}

func TestContador32VoltaUmaVez(t *testing.T) {
	calc := NovaCalculadora()
	calc.aplicar("192.0.2.1", ticks(100), inicio, leituraUnica(c32(4294967000), c32(10), c32(4294967295)))
	r := calc.aplicar("192.0.2.1", ticks(200), inicio.Add(10*time.Second), leituraUnica(c32(704), c32(10), c32(1)))[1]

	if r.EntradaBps == nil || *r.EntradaBps != 1000*8/10 {
		t.Errorf("in_bps após volta de 2^32 = %v, esperado 800", r.EntradaBps)
	}
	if r.ErrosEntrada == nil || *r.ErrosEntrada != 2 {
		t.Errorf("erros após volta = %v, esperado 2", r.ErrosEntrada)
	}
}

func TestContador64QueDiminuiEhReset(t *testing.T) {
	calc := NovaCalculadora()
	calc.aplicar("192.0.2.1", ticks(100), inicio, leituraUnica(c64(5000), c64(5000), c32(1)))
	r := calc.aplicar("192.0.2.1", ticks(200), inicio.Add(10*time.Second), leituraUnica(c64(10), c64(6000), c32(1)))[1]

	if r.EntradaBps != nil {
		t.Errorf("contador de 64 bits que diminuiu deveria dar nulo, deu %v", *r.EntradaBps)
	}
	if r.SaidaBps == nil || *r.SaidaBps != 800 {
		t.Errorf("out_bps = %v, esperado 800", r.SaidaBps)
	}
}

func TestReinicioPorSysUpTimeDescartaOEstado(t *testing.T) {
	calc := NovaCalculadora()
	calc.aplicar("192.0.2.1", ticks(900000), inicio, leituraUnica(c32(100), c32(100), c32(1)))
	r := calc.aplicar("192.0.2.1", ticks(500), inicio.Add(10*time.Second), leituraUnica(c32(5000), c32(5000), c32(2)))[1]
	if r.EntradaBps != nil || r.SaidaBps != nil || r.ErrosEntrada != nil {
		t.Fatalf("após reinício deveria ser nulo: %+v", r)
	}

	r = calc.aplicar("192.0.2.1", ticks(1500), inicio.Add(20*time.Second), leituraUnica(c32(6000), c32(5000), c32(2)))[1]
	if r.EntradaBps == nil || *r.EntradaBps != 800 {
		t.Errorf("a amostra seguinte ao reinício deveria ter taxa: %v", r.EntradaBps)
	}
}

func TestReinicioDeUmEquipamentoNaoAfetaOutro(t *testing.T) {
	calc := NovaCalculadora()
	calc.aplicar("192.0.2.1", ticks(1000), inicio, leituraUnica(c32(0), c32(0), c32(0)))
	calc.aplicar("192.0.2.2", ticks(1000), inicio, leituraUnica(c32(0), c32(0), c32(0)))
	calc.aplicar("192.0.2.1", ticks(10), inicio.Add(10*time.Second), leituraUnica(c32(10), c32(10), c32(0)))
	r := calc.aplicar("192.0.2.2", ticks(2000), inicio.Add(10*time.Second), leituraUnica(c32(10), c32(10), c32(0)))[1]
	if r.EntradaBps == nil {
		t.Error("reinício de 192.0.2.1 apagou o estado de 192.0.2.2")
	}
}

func TestValorNaoMedidoFicaNulo(t *testing.T) {
	calc := NovaCalculadora()
	semErros := map[int]contadores{1: {entrada: c32(0), saida: c32(0)}}
	calc.aplicar("192.0.2.1", nil, inicio, semErros)
	r := calc.aplicar("192.0.2.1", nil, inicio.Add(time.Second), semErros)[1]

	if r.EntradaBps == nil {
		t.Error("in_bps medido deveria existir sem sysUpTime")
	}
	if r.ErrosEntrada != nil || r.ErrosSaida != nil || r.DescartesEntrada != nil || r.DescartesSaida != nil {
		t.Errorf("contador que a fonte não mede deveria ser nulo, não zero: %+v", r)
	}
}

func TestTrocaDe32Para64BitsNaoGeraTaxa(t *testing.T) {
	calc := NovaCalculadora()
	calc.aplicar("192.0.2.1", ticks(1), inicio, leituraUnica(c32(100), c32(100), c32(0)))
	r := calc.aplicar("192.0.2.1", ticks(2), inicio.Add(time.Second), leituraUnica(c64(200), c32(200), c32(0)))[1]
	if r.EntradaBps != nil {
		t.Errorf("mudança de largura do contador deveria dar nulo: %v", *r.EntradaBps)
	}
	if r.SaidaBps == nil {
		t.Error("out_bps sem mudança de largura deveria ter taxa")
	}
}

func TestInterfaceNovaNaoTemTaxa(t *testing.T) {
	calc := NovaCalculadora()
	calc.aplicar("192.0.2.1", ticks(1), inicio, leituraUnica(c32(0), c32(0), c32(0)))
	r := calc.aplicar("192.0.2.1", ticks(2), inicio.Add(time.Second), map[int]contadores{2: {entrada: c32(10)}})[2]
	if r.EntradaBps != nil {
		t.Errorf("interface vista pela primeira vez deveria ser nula: %v", *r.EntradaBps)
	}
}
