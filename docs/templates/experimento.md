# Experimento: título

Segue o formato de `docs/benchmarks/chaos-tier-*.md`, use como referência.

## Hipótese

O que se espera que aconteça, e por quê.

## Estado estável (steady state)

Métrica observável que define "sistema saudável" antes da injeção (latência
p99, taxa de erro, lag de consumer group etc), com o valor medido antes do
experimento.

## Injeção de falha

O que foi injetado, com qual ferramenta (Toxiproxy, `docker stop`, `kubectl
delete pod`, etc), por quanto tempo.

## Observação

O que foi medido durante a injeção. Sempre rotulado como "Medido", nunca
"deveria".

## Condição de parada (stop condition)

Sob qual condição o experimento seria abortado antes do fim, e se isso
aconteceu.

## Recuperação

O que aconteceu quando a falha foi removida: o sistema voltou sozinho? em
quanto tempo? precisou de ação manual?

## Conclusão

O que isso prova ou derruba sobre a hipótese, e o que fica como pendência.
