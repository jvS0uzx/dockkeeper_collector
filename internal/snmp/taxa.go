package snmp

import (
	"sync"
	"time"
)

type contador struct {
	valor uint64
	bits  int
	ok    bool
}

type contadores struct {
	entrada          contador
	saida            contador
	errosEntrada     contador
	errosSaida       contador
	descartesEntrada contador
	descartesSaida   contador
}

type amostra struct {
	em time.Time
	contadores
}

type estadoEquipamento struct {
	uptime    uint32
	temUptime bool
	amostras  map[int]amostra
}

type Calculadora struct {
	mu           sync.Mutex
	equipamentos map[string]*estadoEquipamento
}

func NovaCalculadora() *Calculadora {
	return &Calculadora{equipamentos: map[string]*estadoEquipamento{}}
}

type taxasInterface struct {
	EntradaBps       *float64
	SaidaBps         *float64
	ErrosEntrada     *uint64
	ErrosSaida       *uint64
	DescartesEntrada *uint64
	DescartesSaida   *uint64
}

func (c *Calculadora) aplicar(ip string, uptime *uint32, em time.Time, leituras map[int]contadores) map[int]taxasInterface {
	c.mu.Lock()
	defer c.mu.Unlock()

	anterior := c.equipamentos[ip]
	if anterior != nil && uptime != nil && anterior.temUptime && *uptime < anterior.uptime {
		anterior = nil
	}

	novo := &estadoEquipamento{amostras: make(map[int]amostra, len(leituras))}
	if uptime != nil {
		novo.uptime = *uptime
		novo.temUptime = true
	}

	saida := make(map[int]taxasInterface, len(leituras))
	for indice, atual := range leituras {
		novo.amostras[indice] = amostra{em: em, contadores: atual}

		var taxas taxasInterface
		if anterior != nil {
			if ant, ok := anterior.amostras[indice]; ok {
				if segundos := em.Sub(ant.em).Seconds(); segundos > 0 {
					taxas.EntradaBps = bps(ant.entrada, atual.entrada, segundos)
					taxas.SaidaBps = bps(ant.saida, atual.saida, segundos)
					taxas.ErrosEntrada = diferenca(ant.errosEntrada, atual.errosEntrada)
					taxas.ErrosSaida = diferenca(ant.errosSaida, atual.errosSaida)
					taxas.DescartesEntrada = diferenca(ant.descartesEntrada, atual.descartesEntrada)
					taxas.DescartesSaida = diferenca(ant.descartesSaida, atual.descartesSaida)
				}
			}
		}
		saida[indice] = taxas
	}

	c.equipamentos[ip] = novo
	return saida
}

func delta(anterior, atual contador) (uint64, bool) {
	if !anterior.ok || !atual.ok || anterior.bits != atual.bits {
		return 0, false
	}
	if atual.valor >= anterior.valor {
		return atual.valor - anterior.valor, true
	}
	if atual.bits == 32 {
		return atual.valor + (1 << 32) - anterior.valor, true
	}
	return 0, false
}

func diferenca(anterior, atual contador) *uint64 {
	d, ok := delta(anterior, atual)
	if !ok {
		return nil
	}
	return &d
}

func bps(anterior, atual contador, segundos float64) *float64 {
	d, ok := delta(anterior, atual)
	if !ok {
		return nil
	}
	v := float64(d) * 8 / segundos
	return &v
}
