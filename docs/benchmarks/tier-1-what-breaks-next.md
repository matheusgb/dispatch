# O que quebra a seguir (tier 1)

Baseado no baseline em `tier-1-baseline.md`. Cada item é uma previsão a
testar, não um fato medido.

1. **Pool de conexões sem limite explícito.** `pgxpool.New` não define
   `MaxConns`: sob carga real e concorrente (diferente do benchmark
   sequencial), o pool cresce até o padrão da biblioteca ou até o
   PostgreSQL rejeitar novas conexões. O próximo experimento é fixar
   `MaxConns`, medir saturação do pool sob carga concorrente real (LoadGen
   ou k6) e expor isso como métrica, não só como log.
2. **Loop de expiração por polling fixo de 5 s.** `expireOffersLoop` varre
   todas as entregas em `offered` a cada 5 segundos com um `SELECT` sem
   índice dedicado em `offer_expires_at`. Com um volume alto de ofertas
   simultâneas, essa varredura cresce e passa a competir com o tráfego
   principal. Não há evidência ainda de que isso importe: falta o cenário de
   carga que prove o limite.
3. **PostgreSQL único, sem réplica.** Toda leitura e escrita do tier 1 passa
   pelo mesmo nó. Não há ainda motivo medido para separar leitura de
   escrita: isso só se justifica com o experimento do tier 2 ou 3, não antes.
4. **Nenhuma carga real ainda mediu o throughput sob concorrência.** Os
   números da baseline são de um benchmark sequencial de processo único.
   Falta o LoadGen e o k6 do backlog do tier 1 para produzir p50/p95/p99
   sob concorrência real, incluindo os cenários com falha injetada (matar o
   processo entre commit e resposta, saturar o pool deliberadamente).
5. **`ExpireOverdueOffers` não é atômico entre a leitura e a reciclagem.**
   Entre o `SELECT` que lista IDs vencidos e o `UPDATE` que reciclada cada
   um, outra rotina pode ter mudado o estado; o código já trata isso
   ignorando `ErrNotOffered`, mas não há teste de duas instâncias do loop
   competindo pela mesma entrega. Isso importa a partir do tier 3, quando
   `tracking-projector` e workers concorrentes existirem de fato.

Nenhum destes itens bloqueia a tag `tier-1.0.0`: o critério de conclusão do
tier 1 não exige carga sustentada, exige correção sob disputa concorrente e
um gargalo localizado com evidência, que é o que a baseline documenta.
