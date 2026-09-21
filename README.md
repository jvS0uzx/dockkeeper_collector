# dockkeeper_collector

[![CI](https://github.com/jvS0uzx/dockkeeper_collector/actions/workflows/ci.yml/badge.svg)](https://github.com/jvS0uzx/dockkeeper_collector/actions/workflows/ci.yml)

Agente de inventário de rede que roda dentro de cada unidade, varre a LAN local
e faz push do que encontrou — quais máquinas estão ligadas, com que nome, MAC e
portas abertas — para o painel central
[DockKeeper](https://github.com/jvS0uzx/dock_keeper).

## Por que existe

O painel só enxerga a rede onde o processo dele roda. Com o painel numa VPS ou
na matriz, nenhuma varredura alcança a LAN das filiais: pacote de descoberta não
atravessa a internet.

O coletor faz o papel do *proxy* do Zabbix. Roda dentro da unidade, varre
localmente e faz push do inventário para fora. Uma instância por unidade,
apontando todas para o mesmo painel.

```
Unidade A ── coletor ──┐
Unidade B ── coletor ──┼──► painel central ──► operador
Unidade C ── coletor ──┘
```

É um projeto separado de propósito: o que se instala nos servidores das
unidades não precisa carregar o painel, o banco nem as credenciais SSH.

## O fluxo

O coletor nunca recebe uma credencial de longa duração pela configuração. Ele
recebe um convite descartável e o troca por uma identidade própria no primeiro
boot.

```
1. EMISSÃO         admin global no painel emite um convite
   (no painel)     POST /api/enroll/tokens  {"kind":"collector","site_id":N}
                   → uso único, validade 24 h
                            │
                            ▼  o convite vai em COLLECTOR_ENROLL_TOKEN
2. PRIMEIRO BOOT   coletor troca convite por credencial própria
   (na unidade)    POST /api/enroll  {enrollment_token, machine_id, hostname, kind:"collector"}
                   ← {device_id, device_token, site_id}
                   grava /var/lib/dockkeeper-collector/credential.json  (0600)
                   convite queimado no painel; a variável pode sair da config
                            │
                            ▼
3. BOOTS SEGUINTES credencial persistida vence tudo — sem rede, sem troca
                            │
                            ▼
4. INGESTÃO        a cada 15 min: TCP connect scan das faixas privadas,
   (contínua)      leitura do cache ARP, DNS reverso
                   POST /api/ingest/inventory
                   headers X-Device-Id / X-Device-Token
                   a unidade sai da credencial no lado do painel;
                   o site_code do corpo é conferência, não fonte
```

Revogar o dispositivo no painel derruba só este coletor. Perdeu o arquivo da
credencial? Emite-se outro convite e refaz-se o primeiro boot: não existe
releitura, porque o painel guarda apenas o hash do segredo.

## Modelo de ameaça

O coletor roda em servidor de filial, fora do datacenter, numa máquina que o
time de infraestrutura não vê todo dia. O desenho parte disso: **a máquina onde
ele roda é o ativo menos confiável do sistema**, e cada decisão abaixo limita o
que se perde quando ela é comprometida.

### Por que o convite é de uso único

O modo anterior, hoje descontinuado, era um `COLLECTOR_TOKEN` compartilhado — o
mesmo valor em todos os coletores de todas as unidades. Com ele, qualquer máquina comprometida
declarava a filial que quisesse, porque a unidade vinha do corpo da requisição
e não havia nada amarrando o remetente a ela. É exatamente o que está registrado
no cabeçalho de [`internal/identity/identity.go`](internal/identity/identity.go).

O convite inverte isso. Ele nasce amarrado a uma unidade (`"kind": "collector"` +
`"site_id"`), vale 24 horas, e o painel o queima na primeira troca. O que a unidade
guarda em disco depois disso não é mais o convite: é uma credencial de
dispositivo que só serve para aquele dispositivo, e cuja revogação individual
não afeta nenhum outro coletor.

O ganho concreto é o raio de alcance. Roubar o disco de uma filial hoje entrega
a ingestão de inventário de **uma** unidade, revogável em um clique. Antes,
entregava a de todas.

### O que acontece se o convite vazar antes do primeiro boot

Vaza uma janela, não um acesso permanente. Nessa janela — 24 horas, ou menos, se
o coletor legítimo já subiu — quem tiver o convite consegue enrolar um
dispositivo falso e passar a empurrar inventário para aquela unidade.

O que impede o vazamento de ser silencioso é a corrida pelo uso único. Se o
atacante enrola primeiro, o coletor legítimo tenta a troca, o painel recusa o
convite já consumido, e `Resolve` devolve `enrollment falhou: ...`
([`internal/identity/identity.go`](internal/identity/identity.go)). Esse erro é
fatal em [`cmd/collector/main.go`](cmd/collector/main.go) — o serviço não sobe.
A consequência aparece no painel imediatamente: a unidade para de reportar. Não
existe o cenário em que o coletor legítimo e o falso conviveram e ninguém notou.

Há um caso de borda tratado explicitamente, porque é o pior momento possível
para falhar: o convite foi trocado com sucesso mas a gravação em disco falhou.
A credencial existe em memória e o ciclo atual funciona, mas o próximo reinício
não conseguirá outra, porque o convite já foi queimado. `Resolve` registra isso
como `AVISO GRAVE` com a instrução de emitir outro convite antes de reiniciar,
em vez de deixar o operador descobrir na próxima janela de manutenção. É também
a razão de a credencial morar em `/var/lib` e não em `/etc`: o serviço roda com
`ProtectSystem=strict`, que deixa `/etc` somente-leitura, e `StateDirectory=`
garante que o único caminho gravável exista com o dono certo mesmo sob
`DynamicUser=yes` ([`deploy/dockkeeper-collector.service`](deploy/dockkeeper-collector.service)).

Duas contenções reduzem a janela na origem: a validade de 24 horas, e o fato de
o painel recusar unidade não cadastrada — criar unidade automaticamente
permitiria que um convite vazado poluísse o cadastro com filiais inventadas.

### Por que o segredo do dispositivo é guardado como SHA-256 e nunca em claro

Do lado do painel, `device_credentials` guarda `SHA-256(segredo)`, e o mesmo
vale para o hash do próprio convite. Um dump da tabela — backup mal guardado,
SQL injection, acesso de leitura ao banco — não entrega credencial nenhuma.

Do lado do coletor, o segredo em claro existe em exatamente um lugar: o arquivo
`credential.json`, escrito com modo `0600`. Não é conveniência de permissão, é o
reconhecimento de que o segredo vale por si só — um arquivo legível por todos
entrega a identidade do dispositivo a qualquer processo da máquina. O teste
`TestCredencialSalvaECarregada` afirma `0600` como asserção, não como detalhe de
implementação ([`internal/identity/identity_test.go`](internal/identity/identity_test.go)).

Consequência que vale enunciar: **não existe endpoint de releitura**. O painel
não sabe o segredo, só o hash dele. Perder o arquivo não é recuperável — é
motivo para emitir novo convite. Isso é o comportamento desejado; um endpoint
"me mostre o segredo deste dispositivo" seria uma porta de escalação de
privilégio dentro do próprio painel.

A escolha de SHA-256 em vez de bcrypt é deliberada e está registrada na
[ADR 003](https://github.com/jvS0uzx/dock_keeper/blob/main/docs/adr/003-segredo-de-dispositivo-usa-sha256.md)
do painel. O resumo: bcrypt existe para encarecer ataque de dicionário contra
segredo escolhido por humano. Aqui o segredo tem 32 bytes de `crypto/rand` —
256 bits de entropia, sem dicionário e sem reúso — então bcrypt não compraria
nada, e custaria caro: a ingestão é caminho quente, e 60 a 100 ms de CPU por
verificação com centenas de dispositivos reportando seria negação de serviço
embutida. A ADR também marca a condição de reversão: se algum dia o segredo
passar a ser escolhido por gente, a decisão se inverte.

O `DeviceID` viaja separado do segredo (`X-Device-Id` / `X-Device-Token`, em
[`internal/push/push.go`](internal/push/push.go)) para o painel achar a linha
por chave primária. Sem ele, verificar uma credencial exigiria varrer a tabela
comparando hash linha a linha, a cada push.

Fecha o cerco a redação de log. O `*url.Error` do Go embute a URL inteira na
mensagem, então qualquer erro de transporte é um vazamento em potencial:
`Client.redact` substitui o segredo da credencial e o token legado por
`<CREDENCIAL>` e `<COLLECTOR_TOKEN>` antes de a mensagem virar log
([`internal/push/push.go`](internal/push/push.go)). Na mesma linha, o corpo de
uma resposta de erro do enrollment é lido com teto de 512 bytes: um painel mal
configurado pode devolver uma página inteira, e despejá-la no journal não ajuda
ninguém.

### Por que a ingestão é fail-closed

Um coletor que roda sem conseguir enviar é pior que um coletor que não roda,
porque a unidade desaparece do painel sem ninguém perceber. Um painel silencioso
não é a mesma coisa que um painel dizendo "está tudo bem", mas é assim que ele é
lido. Então a falha é sempre ruidosa, em quatro pontos:

**No transporte.** `http://` para painel remoto é recusado; use `https://` ou assuma o risco
com `ALLOW_INSECURE_HTTP=true`, que registra aviso a cada subida.

**Na configuração.** `config.Load` recusa subir sem `COLLECTOR_SERVER_URL`,
`COLLECTOR_SITE` ou `COLLECTOR_CIDRS`
([`internal/config/config.go`](internal/config/config.go)); em
[`cmd/collector/main.go`](cmd/collector/main.go) isso é `log.Fatalf`. Não há
valor padrão para nenhuma das três — um padrão aqui produziria um coletor
subindo e varrendo a rede errada.

**Na identidade.** Sem credencial persistida, sem convite e sem token legado,
`Resolve` devolve erro em vez de operar anônimo, e o serviço não sobe.

**Na resposta do painel.** Cada resposta cai numa de três classes
([`internal/push/push.go`](internal/push/push.go)):

| Resposta | Classe | O coletor faz |
|---|---|---|
| falha de rede, `5xx`, `429` | transitória | tenta 3 vezes, com 5 s de intervalo |
| `401`, `403` | credencial recusada (`ErrUnauthorized`) | uma tentativa; o ciclo registra a recusa |
| `400`, `404`, `409`, `413`, `422` e demais `4xx` | recusa definitiva (`ErrRecusado`) | uma tentativa; o ciclo registra o status |

Credencial recusada e envio recusado são configuração, não instabilidade:
repetir não muda a resposta e só produz ruído na auditoria do painel — um `409`
de unidade divergente repetido três vezes gravava três linhas de
`inventory.site_mismatch` por ciclo. A assimetria é o ponto — o
coletor distingue "estou errado" de "a rede está ruim". Do lado do painel, a
recusa é auditada e o sucesso não; o raciocínio está na
[ADR 002](https://github.com/jvS0uzx/dock_keeper/blob/main/docs/adr/002-ingestao-audita-so-recusa.md),
junto do sinal mais direto de comprometimento que o sistema produz:
`inventory.site_mismatch`, um coletor com credencial válida declarando outra
unidade. O agente de estação gera o equivalente na rota de métricas, com a ação
`ingest.site_mismatch`.

**No conteúdo do envio.** Inventário vazio é informação legítima — significa
rede fora do ar. Inventário vazio **com falha em todas as faixas** não é: é o
coletor não sabendo nada. Enviar isso apagaria o último estado bom no painel, e
o operador veria uma unidade sem máquina nenhuma em vez de uma unidade sem
leitura. O ciclo em [`cmd/collector/main.go`](cmd/collector/main.go) descarta
esse caso e espera o próximo.

### Contenção do próprio coletor como arma

Um scanner de rede rodando como serviço é uma ferramenta ofensiva se apontada
para fora. `ExpandCIDR` recusa, em
[`scan/scan.go`](scan/scan.go):

- faixa que não seja privada (RFC 1918, link-local, loopback);
- faixa maior que `/16` — 65 mil hosts é varredura longa demais para rede de
  escritório e provavelmente erro de digitação;
- endereço IPv6.

Recusar endereço público é o que impede que uma configuração alterada — por
descuido ou por quem comprometeu a máquina — transforme o coletor em scanner
apontável para rede de terceiros. `TestExpandCIDRRecusaFaixaPublica` prova a
recusa, e uma faixa inválida no meio da lista é registrada e ignorada sem abortar
as outras.

E o coletor não precisa de privilégio para nada disso: a varredura é TCP connect,
não socket raw. Por isso o serviço roda com `DynamicUser=yes`,
`NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=yes`,
`MemoryDenyWriteExecute=yes` e `RestrictAddressFamilies=` limitado
([`deploy/dockkeeper-collector.service`](deploy/dockkeeper-collector.service)). Um binário de
varredura comprometido rodando como root na filial seria o pior resultado
possível deste projeto; a escolha do TCP connect scan é o que permite evitá-lo.

## Como varre

TCP connect scan. Para cada endereço da faixa, tenta abrir conexão numa lista
curta de portas comuns — 22, 80, 135, 139, 443, 445, 515, 631, 3389, 5000, 8080,
9100 — e quem aceita está ligado. A lista é curta de propósito: cobre estação
Windows, Linux, impressora, NAS e web, sem virar auditoria de porta.

O MAC vem do cache ARP do kernel, lido **depois** da varredura: foram as
conexões TCP dela que preencheram a tabela. Isso sai de graça, sem gerar um
único pacote a mais, e serve para identificar o equipamento mesmo quando o IP
muda por DHCP.

O DNS reverso tem prazo de 1 segundo por host, porque numa rede sem PTR cada
consulta esperaria o timeout inteiro do resolver.

## O que o painel faz com o envio

Upsert pela chave `(site_id, ip)`: o mesmo `192.168.0.10` em duas unidades são dois
hosts, não um. Preserva `first_seen` e o cadastro que o operador preencheu no
painel — sala, responsável, patrimônio — que nunca é sobrescrito por um coletor.
`hostname` e `mac` só substituem o valor guardado quando vieram preenchidos: um
DNS reverso que falhou não pode apagar o nome já conhecido.

## Instalação

```bash
go build -o collector ./cmd/collector

sudo cp collector /usr/local/bin/dockkeeper-collector
sudo cp deploy/dockkeeper-collector.service /etc/systemd/system/
sudo cp deploy/collector.env.exemplo /etc/dockkeeper-collector.env
sudo chmod 600 /etc/dockkeeper-collector.env
sudoedit /etc/dockkeeper-collector.env

sudo systemctl enable --now dockkeeper-collector
journalctl -u dockkeeper-collector -f
```

O modelo versionado é `deploy/collector.env.exemplo` e não recebe valor real
nenhum; o arquivo com segredo é o `/etc/dockkeeper-collector.env` da máquina, que fica
fora do repositório. O `.gitignore` recusa qualquer `*.env` que não termine em
`.exemplo`.

Para rodar por cron em vez de serviço, use `COLLECTOR_ONCE=true`.

### Migração do nome antigo

Até setembro de 2026 o coletor se chamava `vd-collector`. Numa máquina já
instalada com esse nome, rode como root, uma vez, antes de instalar a versão
nova. Os comandos são idempotentes e não sobrescrevem nada que já exista no
nome novo:

```bash
systemctl disable --now vd-collector.service || true

[ -f /etc/vd-collector.env ] && [ ! -e /etc/dockkeeper-collector.env ] \
  && mv /etc/vd-collector.env /etc/dockkeeper-collector.env
[ -f /etc/dockkeeper-collector.env ] \
  && sed -i 's#/var/lib/vd-collector/#/var/lib/dockkeeper-collector/#g' /etc/dockkeeper-collector.env

install -d -m 0700 /var/lib/private /var/lib/private/dockkeeper-collector
for f in credential.json machine-id; do
  for antigo in /var/lib/private/vd-collector /var/lib/vd-collector; do
    [ -f "$antigo/$f" ] && [ ! -L "$antigo" ] && [ ! -e "/var/lib/private/dockkeeper-collector/$f" ] \
      && mv "$antigo/$f" "/var/lib/private/dockkeeper-collector/$f"
  done
done
[ -L /var/lib/vd-collector ] && rm -f /var/lib/vd-collector
rmdir /var/lib/private/vd-collector /var/lib/vd-collector 2>/dev/null || true

rm -f /etc/systemd/system/vd-collector.service /usr/local/bin/vd-collector
systemctl daemon-reload
```

Depois siga a instalação acima. A credencial e o `machine-id` levados para
`/var/lib/private/dockkeeper-collector/` preservam a identidade: sem eles o
coletor pediria convite novo e a unidade ganharia um segundo dispositivo. O
systemd ajusta o dono dos arquivos para o usuário dinâmico no primeiro start.

## Configuração

Tudo por ambiente. Sem as três primeiras o coletor recusa subir.

| Variável | Obrigatória | Descrição |
|---|---|---|
| `COLLECTOR_SERVER_URL` | sim | URL do painel central. Precisa ser `https://` quando o painel é remoto: com `http://` o coletor recusa subir, porque a credencial viajaria em claro. `http://localhost`, `http://127.0.0.1` e `http://[::1]` continuam valendo, e `ALLOW_INSECURE_HTTP=true` libera o `http://` remoto com aviso no log |
| `COLLECTOR_SITE` | sim | código da unidade, cadastrado na tela Unidades |
| `COLLECTOR_CIDRS` | sim | faixas varridas, separadas por vírgula; só rede privada |
| `COLLECTOR_ENROLL_TOKEN` | identidade | convite de uso único emitido no painel; trocado por credencial própria no primeiro boot |
| `COLLECTOR_CREDENTIAL_PATH` | não | onde a credencial fica (padrão `/var/lib/dockkeeper-collector/credential.json`) |
| `COLLECTOR_MACHINE_ID` | não | identificador estável da máquina; vazio usa `/etc/machine-id` |
| `COLLECTOR_INTERVAL_MIN` | não | minutos entre varreduras (mínimo 1, padrão 15); o valor vai em `report_interval_sec` a cada envio, e é por ele que o painel decide quando avisar que o coletor sumiu |
| `COLLECTOR_PORTS` | não | portas sondadas; vazio usa a lista padrão |
| `COLLECTOR_ONCE` | não | `true` varre uma vez e encerra |

Identidade: o coletor precisa da credencial já persistida ou de um convite em
`COLLECTOR_ENROLL_TOKEN`. A credencial vence o convite, e sem nenhum dos dois ele
não sobe.

## Descontinuado: token compartilhado

O `COLLECTOR_TOKEN` era o mesmo segredo em todos os coletores, igual ao
`AGENT_INGEST_TOKEN` do painel, e não amarrava o coletor a uma unidade. O motivo
da troca está em "Por que o convite é de uso único", acima.

O painel **recusa esse token por padrão**, com `401`. Durante a migração de um
parque antigo ele o aceita só com `ALLOW_LEGACY_INGEST_TOKEN=true` no `.env` do
painel, com aviso a cada uso.

O coletor ainda usa `COLLECTOR_TOKEN` quando não há credencial persistida nem
convite, e avisa no log na subida. Se o painel estiver sem a flag, cada ciclo
registra `credencial recusada pelo painel (HTTP 401): o token compartilhado so e
aceito com ALLOW_LEGACY_INGEST_TOKEN=true no painel`.

Para migrar: emita um convite do tipo coletor para a unidade, coloque-o em
`COLLECTOR_ENROLL_TOKEN`, apague `COLLECTOR_TOKEN` e reinicie o serviço.

## Testes

```bash
go test -race ./...
```

## Contribuindo

- [Como contribuir](CONTRIBUTING.md): comandos do CI, regras do código e o
  contrato com o painel.
- [Segurança](SECURITY.md): vulnerabilidade se reporta em privado, pelo GitHub
  Security Advisories.
- [Código de conduta](CODE_OF_CONDUCT.md).
- [Changelog](CHANGELOG.md).

## Licença

MIT — ver [LICENSE](LICENSE).
