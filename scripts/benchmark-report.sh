#!/usr/bin/env bash
# Agrega os benchmarks mais recentes de docs/benchmarks/ num resumo único,
# para não precisar abrir arquivo por arquivo. Não gera número novo, só
# lista e formata o que já existe (todo número real já está rotulado
# Medido dentro de cada arquivo original).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BENCH_DIR="$ROOT/docs/benchmarks"

echo "# Relatório agregado de benchmarks"
echo
echo "Gerado em $(date -u +"%Y-%m-%dT%H:%M:%SZ") a partir de $BENCH_DIR."
echo

for tier_file in "$BENCH_DIR"/tier-*-baseline.md; do
  [ -e "$tier_file" ] || continue
  name=$(basename "$tier_file")
  echo "## $name"
  echo
  # primeiras linhas não-vazias após o título, como resumo.
  awk 'NR>1 && NF {print; c++} c>=6 {exit}' "$tier_file"
  echo
  echo "(completo em docs/benchmarks/$name)"
  echo
done

echo "## Todos os arquivos de benchmark disponíveis"
echo
find "$BENCH_DIR" -type f \( -name "*.md" -o -name "*.json" -o -name "*.txt" \) | sed "s|$ROOT/||" | sort
