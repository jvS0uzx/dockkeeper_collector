#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../workflows"

falhas=0

soltas=$(grep -nE '^\s*-?\s*uses:' ./*.yml | grep -vE 'uses:\s*\S+@[0-9a-f]{40}\s*$' || true)
if [ -n "$soltas" ]; then
  echo "action sem SHA de commit:"
  echo "$soltas"
  falhas=1
fi

for arquivo in ./*.yml; do
  baixa=$(grep -cE 'curl .*releases/download' "$arquivo" || true)
  confere=$(grep -cE 'sha256sum (-c|--check)' "$arquivo" || true)
  if [ "$baixa" -gt "$confere" ]; then
    echo "$arquivo: $baixa download(s) de release e $confere conferência(s) de SHA-256"
    falhas=1
  fi
  if grep -nE 'curl [^|]*\|\s*(tar|sh|bash)' "$arquivo"; then
    echo "$arquivo: download executado ou extraído direto do pipe, antes de conferir"
    falhas=1
  fi
done

if [ "$falhas" -ne 0 ]; then
  exit 1
fi
echo "ok: actions fixadas por SHA e downloads conferidos"
