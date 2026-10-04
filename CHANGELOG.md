# Changelog

Mudanças relevantes do coletor, no formato do
[Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/), com versão em
[SemVer](https://semver.org/lang/pt-BR/). A primeira versão numerada é a 1.0.0;
as seções datadas abaixo dela são o histórico anterior, quando cada entrada
levava a data em que chegou à `main`.

## [Não lançado]

## [1.0.0] - 2026-10-04

Primeira versão numerada. Reúne tudo o que entrou depois de 10/09/2026.

### Adicionado

- Leitura de interfaces por SNMP v2c (`SNMP_TARGETS`, `SNMP_COMMUNITY`,
  `SNMP_VERSION`, `SNMP_INTERVAL`, `SNMP_TIMEOUT`, `SNMP_RETRIES`,
  `SNMP_PORT`). O coletor calcula bps, erros e descartes por porta e envia para
  `POST /api/ingest/network-metrics` num laço próprio, independente do
  inventário. Alvo inválido, versão diferente de `2c` ou community ausente
  impedem a subida. Primeira dependência externa: `github.com/gosnmp/gosnmp`
  v1.45.0, Go puro.
- Versão injetada no build por `-ldflags "-X main.Version=<versão>"` e flag
  `--version`. Sem `-ldflags`, o binário se reporta como `dev`.
- `"schema": 1` no corpo de `POST /api/ingest/inventory`, como já ia no envio
  de métricas de rede.
- `AVISO` no log quando o painel aceita o lote SNMP mas descarta equipamentos
  (`rejeitados` maior que zero na resposta).
- `SNMP_PERMITIR_PUBLICO` e `ALLOW_INSECURE_HTTP` no
  `deploy/collector.env.exemplo`.
- CI no GitHub Actions: `gofmt`, `go vet`, `go build`, `go test -race`,
  conferência da versão injetada e `gitleaks` sobre o histórico completo.
- Dependabot semanal para os módulos Go e para as actions.
- Teste que falha se aparecer comentário no código.
- `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md` e este changelog.

### Alterado

- **Mudança incompatível: alvo SNMP fora da rede interna impede o boot.**
  `SNMP_TARGETS` só aceita RFC 1918, `fc00::/7`, loopback e link-local;
  endereço público, inclusive a faixa CGNAT `100.64.0.0/10`, recusa a subida até
  `SNMP_PERMITIR_PUBLICO=true`, que libera com `AVISO` no log. `0.0.0.0`, `::`,
  multicast e broadcast são recusados sempre.
- **Mudança incompatível: o nome de instalação passou de `vd-collector` para
  `dockkeeper-collector`**: unit systemd, binário,
  `/etc/dockkeeper-collector.env` e
  `/var/lib/dockkeeper-collector/credential.json`. Máquina já instalada precisa
  dos passos da seção "Migração do nome antigo" do README antes de atualizar.
- Cada envio declara o intervalo configurado em `report_interval_sec`. O painel
  usa o valor para decidir quando o coletor está ausente; sem ele, assume 15 min.
- O pacote de varredura saiu de `internal/scan` para `scan`, público, para o
  painel poder usar a mesma implementação e a mesma lista de portas.
- O envio só repete o que pode dar certo na tentativa seguinte: falha de rede,
  `5xx` e `429`, três vezes com 5 s de intervalo. `401` e `403` viram credencial
  recusada; `400`, `404`, `409`, `413` e `422` fazem uma tentativa só. Antes, um
  `409` de unidade divergente era repetido três vezes e gravava três linhas de
  auditoria por ciclo.
- `503` deixou de ser tratado como credencial recusada e passou a ser falha
  transitória.
- A recusa definitiva do painel passou a ser registrada como `painel recusou o
  envio (HTTP n)`, e não mais `painel recusou o inventário`, porque vale também
  para as métricas de rede.
- CI fixado em `ubuntu-24.04`, com `actions/checkout` v7.0.1 e
  `actions/setup-go` v7.0.0 (Node 24), fixadas por SHA.

### Descontinuado

- O token compartilhado (`COLLECTOR_TOKEN`) continua funcionando no coletor, mas
  o painel só o aceita com `ALLOW_LEGACY_INGEST_TOKEN=true`. O aviso no log diz
  isso. Use o convite de enrollment.

### Corrigido

- Velocidade e bps negativos, `NaN` ou infinitos são trocados por `null` antes
  do envio, em vez de seguirem como valor bruto para o painel.
- README: a chave do inventário no painel é `(site_id, ip)`, o convite pede
  `site_id` e a auditoria registra `inventory.site_mismatch`.

### Segurança

- `http://` para painel remoto é recusado no boot, salvo
  `ALLOW_INSECURE_HTTP=true`.
- Actions fixadas por SHA e download do `gitleaks` conferido por SHA-256 no CI.
- A community SNMP nunca aparece em log, mensagem de erro ou envio.

## 2026-09-10

### Alterado

- Module path passou a ser `github.com/jvS0uzx/dockkeeper_collector`.

### Segurança

- O modelo de configuração deixou de ser um `*.env` rastreado e virou
  `collector.env.exemplo`; o `.gitignore` recusa qualquer `*.env` que não seja
  modelo.
- Endereços dos testes trocados pelas faixas que a RFC 5737 reserva para
  documentação.
- README ganhou a seção "Modelo de ameaça", com o arquivo que sustenta cada
  afirmação.

## 2026-08-25

### Alterado

- Licença trocada para MIT; o `NOTICE`, que só fazia sentido com a Apache 2.0,
  saiu.

## 2026-08-23

### Adicionado

- Primeira versão: varredura TCP das faixas privadas da unidade, leitura da
  tabela ARP para o MAC, DNS reverso para o nome e envio do inventário ao painel.
- Identidade por dispositivo, com paridade com o painel: o convite de uso único
  (`COLLECTOR_ENROLL_TOKEN`) é trocado por uma credencial própria, gravada com
  modo `0600`, e os envios saem com `X-Device-Id` e `X-Device-Token`.
- Unit systemd com `DynamicUser` e `ProtectSystem=strict`.
