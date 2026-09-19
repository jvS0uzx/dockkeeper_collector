# Política de segurança

## Como reportar

Reporte vulnerabilidades de forma privada pelo GitHub Security Advisories:
[abrir um reporte](https://github.com/jvS0uzx/dockkeeper_collector/security/advisories/new).

Não abra issue pública nem PR que descreva a falha antes da correção sair. O
reporte privado fica visível só para você e para o mantenedor até ser publicado.

Ajuda muito trazer:

- a versão ou o commit afetado;
- o passo a passo para reproduzir, de preferência com o menor cenário possível;
- o que um atacante consegue fazer com a falha e de que posição (dentro da LAN da
  unidade, na rede entre coletor e painel, com acesso à máquina do coletor).

## O que acontece depois

O projeto é mantido por uma pessoa, em melhor esforço. A meta é confirmar o
recebimento em até 7 dias e combinar com você o prazo da correção e da
divulgação. Quem reporta recebe o crédito no aviso publicado, se quiser.

## O que conta como vulnerabilidade

O [modelo de ameaça](README.md#modelo-de-ameaça) no README diz o que o coletor
protege e o que ele assume. Estão no escopo, por exemplo:

- vazamento da credencial do dispositivo ou do convite (log, arquivo com
  permissão aberta, mensagem de erro);
- coletor enviando inventário em nome de outra unidade;
- varredura saindo das faixas privadas configuradas;
- execução de código ou escrita fora do diretório de estado a partir de uma
  resposta do painel ou de algo encontrado na rede.

Fora do escopo: problemas que exigem root na máquina do coletor, e falhas do
painel, que têm o próprio canal em
[jvS0uzx/dock_keeper](https://github.com/jvS0uzx/dock_keeper/security/advisories/new).

## Versões cobertas

Só a versão mais recente da branch `main` recebe correção.
