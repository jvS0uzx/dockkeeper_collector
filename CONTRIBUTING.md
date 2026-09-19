# Como contribuir

O coletor é o satélite de inventário do [DockKeeper](https://github.com/jvS0uzx/dock_keeper).
Correção, teste e relato de problema são bem-vindos. Esta página diz o que um PR
precisa para ser aceito sem ida e volta.

## Rodar e testar

Só a biblioteca padrão do Go, na versão declarada no `go.mod`.

```bash
go build ./...
go vet ./...
go test -race ./...
gofmt -l .
```

O CI roda exatamente isso, mais o `gitleaks` sobre o histórico inteiro. PR com
qualquer um deles vermelho não entra.

## Regras do código

- **Nenhum comentário em código.** O porquê vai para a mensagem de commit, para o
  README ou para uma ADR no repositório do painel. Um teste em
  `internal/semcomentario` falha se aparecer comentário. Diretivas como
  `//go:build` continuam valendo.
- **Nenhuma dependência nova sem motivo forte.** O coletor roda dentro da rede de
  cada unidade; cada dependência é superfície a mais nesse lugar.
- **Teste antes da correção.** Todo PR que muda comportamento traz o teste que
  falha antes e passa depois.
- Mensagens de log e de erro em português, no tom das que já existem. Nenhum
  segredo em log: credencial e token passam por redação.
- `gofmt` sem exceção.

## Contrato com o painel

O coletor fala com o painel por `POST /api/enroll` e `POST /api/ingest/inventory`.
Mudança que toca esse contrato (campos do envio, cabeçalhos, códigos de resposta)
precisa do PR correspondente no [painel](https://github.com/jvS0uzx/dock_keeper),
revisado junto. Um lado sozinho quebra as unidades já instaladas.

O pacote `scan` é público porque o painel usa a mesma varredura. Mudar a API dele
ou a lista `DefaultPorts` também exige o PR do outro lado.

## Commits e PR

- Mensagem no formato `tipo: descrição` (`feat`, `fix`, `docs`, `chore`,
  `refactor`, `test`), em português, dizendo o que muda para quem usa.
- Um assunto por PR. Refatoração e mudança de comportamento em PRs separados.
- Registre a mudança em `CHANGELOG.md`, na seção "Não lançado".

## Segurança

Vulnerabilidade não vai em issue pública. Veja [SECURITY.md](SECURITY.md).

## Convivência

Ao participar você concorda com o [código de conduta](CODE_OF_CONDUCT.md).
