# Changelog

Mudanças relevantes do coletor, no formato do
[Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/). O projeto ainda não
publica versões numeradas; cada entrada leva a data em que chegou à `main`.

## [Não lançado]

### Alterado

- Cada envio declara o intervalo configurado em `report_interval_sec`. O painel
  usa o valor para decidir quando o coletor está ausente; sem ele, assume 15 min.
- **O nome de instalação passou de `vd-collector` para `dockkeeper-collector`**:
  unit systemd, binário, `/etc/dockkeeper-collector.env` e
  `/var/lib/dockkeeper-collector/credential.json`. Máquina já instalada precisa
  dos passos da seção "Migração do nome antigo" do README antes de atualizar.
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

### Descontinuado

- O token compartilhado (`COLLECTOR_TOKEN`) continua funcionando no coletor, mas
  o painel só o aceita com `ALLOW_LEGACY_INGEST_TOKEN=true`. O aviso no log agora
  diz isso. Use o convite de enrollment.

### Adicionado

- Leitura de interfaces por SNMP v2c (`SNMP_TARGETS`, `SNMP_COMMUNITY`,
  `SNMP_VERSION`, `SNMP_INTERVAL`, `SNMP_TIMEOUT`, `SNMP_RETRIES`,
  `SNMP_PORT`). O coletor calcula bps, erros e descartes por porta e envia para
  `POST /api/ingest/network-metrics` num laço próprio, independente do
  inventário. Alvo inválido, versão diferente de `2c` ou community ausente
  impedem a subida. Primeira dependência externa: `github.com/gosnmp/gosnmp`
  v1.45.0, Go puro.
- CI no GitHub Actions: `gofmt`, `go vet`, `go build`, `go test -race` e
  `gitleaks` sobre o histórico completo.
- Teste que falha se aparecer comentário no código.
- `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md` e este changelog.

### Corrigido

- README: a chave do inventário no painel é `(site_id, ip)`, o convite pede
  `site_id` e a auditoria registra `inventory.site_mismatch`.

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
