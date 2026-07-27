# ADR 0008: realtime-gateway não vira um serviço próprio no tier 3

## Contexto

O roadmap lista `realtime-gateway` (SSE e fallback de leitura) como um
deployable distinto de `tracking-projector` (última posição, histórico e
replay), justificado por "muitas conexões longas, perfil distinto de
recurso".

## Decisão

O tier 3 mantém as duas responsabilidades no mesmo binário
(`cmd/tracking-projector`). Nenhuma medição deste laboratório mostrou
conexões SSE competindo por recurso com o consumo de Kafka ou a escrita em
PostgreSQL/Redis a ponto de justificar dois deployables, dois Services e
duas HPAs monitorando perfis diferentes.

## Alternativas consideradas

- **Extrair já, seguindo o diagrama do roadmap ao pé da letra:** rejeitada.
  O princípio do próprio roadmap (`lunch-rush.md`, "Princípio central de
  evolução") exige uma limitação medida ou um experimento de aprendizagem
  explícito antes de adicionar um deployable. Nenhum dos dois existe ainda
  aqui: seria copiar a arquitetura do diagrama final sem o motivo.

## Consequências

- se um teste de carga futuro (LoadGen ou k6) mostrar que muitas conexões
  SSE abertas degradam a latência de ingestão de posição no mesmo processo,
  isso vira o gatilho para a extração, com número e relatório, não com
  "porque o diagrama já previa";
- enquanto continuam juntos, o HPA de `tracking-projector` escala pelos dois
  motivos ao mesmo tempo (CPU do consumo Kafka e número de conexões SSE),
  o que é uma imprecisão aceita e documentada, não uma omissão.

## Status

Aceita. Revisão obrigatória se/quando houver medição de contenção.
