# Aprendizado do tier 6 (e fechamento do roadmap)

1. **O que eu não sabia no início?** Que "não reconstruir a imagem em
   `cloud-b`" só vira prova de portabilidade quando é verificada por comando
   (`docker inspect --format '{{.Image}}'`), não por intenção declarada em
   comentário. A primeira versão do `docker-compose.cloud-b.yml` já nascia
   sem `build:`, mas isso só deixou de ser alegação e virou evidência depois
   de rodar `docker inspect` nos dois containers e ver o mesmo `sha256:...`.

2. **Que hipótese testei?** Que a dependência oculta mais provável entre
   `cloud-a` e `cloud-b`, apesar de redes e bancos completamente isolados,
   seria alguma coisa de rede ou de porta. Errado. A primeira tentativa de
   derrubar essa dependência (`docker rmi lunchrush-delivery-api`) nem
   conseguiu remover a imagem, porque o Docker recusa remover uma imagem
   enquanto qualquer container a referencia, e o container de `cloud-a`
   ainda estava rodando. Só depois de parar os containers dos dois lados a
   dependência real apareceu: build/registry de imagem compartilhado, não
   rede nem dado.

3. **Que evidência mudou minha opinião?** O RTO medido do failover
   (~11,5s) surpreendeu por ser dominado inteiramente por overhead de
   processo (`docker compose stop`, `dropdb`/`createdb`, `pg_restore`), não
   por lentidão do protocolo de fencing em si (as chamadas
   `Promote`/`CreateAssignment` continuam na casa de milissegundos, como no
   tier 5). Isso mudou a conclusão: o gargalo do failover cross-cloud,
   nesta escala de laboratório, é operacional, não de protocolo, e
   provavelmente continuaria operacional, com números maiores, mesmo contra
   um provedor real.

4. **Onde o sistema quebrou?** Na primeira tentativa do experimento de
   failover, o script usava `epoch=1` fixo no writer de teste, mas uma
   promoção manual anterior (feita durante a exploração, antes do script
   final) já tinha deixado o shard em `epoch=2` no banco. O resultado foi
   `0 sucessos / 9 outros erros` em vez de `10 sucessos`, porque a query SQL
   de seleção de pares livres também tinha um bug de cross join (repetia
   `delivery_id`/`courier_id` entre linhas). Eram dois bugs diferentes
   mascarados num único resultado ruim. Só depois de separar os dois
   (resetar o estado do shard, depois corrigir a query de pareamento com
   duas listas ordenadas zipadas em vez de `SELECT d.id, c.id FROM
   deliveries d, couriers c WHERE ...`) o experimento voltou a produzir um
   resultado limpo e interpretável.

5. **Como diagnostiquei?** Lendo o JSON de saída de cada subcomando do
   `cmd/cloudfailover` (successes/stale_rejections/other_errors) em vez de
   só olhar o exit code. `other_errors: 9`, em vez de `stale_rejections: 9`,
   foi o sinal de que o problema não era o fencing rejeitando escrita antiga
   (o que seria esperado e correto): era outra classe de erro (constraint
   única de `active_assignments`). O bug ficou óbvio assim que o "outro
   erro" não bateu com a hipótese do teste.

6. **Qual solução considerei e rejeitei?** Simular uma réplica contínua
   entre os dois Postgres (CDC/`pg_logical`) para reduzir o RPO medido a
   quase zero. Rejeitada por escopo: o roadmap já aceita RPO diferente de
   zero para planos de dados assíncronos, e o objetivo deste tier é provar
   o protocolo de fencing sobrevivendo à troca de provedor, não construir um
   pipeline de replicação novo. Fica candidato em
   `docs/benchmarks/tier-6-what-breaks-next.md`.

7. **Que complexidade aceitei?** Um segundo LocalStack inteiro (`cloud-b`,
   porta 14566) só para provar que dois roots Terraform independentes
   funcionam contra duas contas simuladas diferentes. Seria mais barato em
   tempo rodar os dois ambientes Terraform contra o mesmo LocalStack com
   nomes de bucket diferentes, mas isso não provaria isolamento de conta
   nenhum, só nomes diferentes no mesmo container.

8. **O que eu faria diferente em um sistema real?** Não tentaria provar
   "portabilidade" com um único experimento de failover manual. Um sistema
   real precisaria do runbook de promoção automatizado, com stop condition
   e alarme, exercitado em game day
   (o termo geral para esse tipo de exercício é engenharia do caos: testar
   falhas de propósito, em produção ou perto dela, para validar que o
   sistema e a equipe reagem bem), não só chamado por um operador que sabe
   exatamente a sequência de comandos. Também não deixaria a autoridade de
   fencing depender de `pg_dump`/`pg_restore` periódico: um plano de dados
   real usaria replicação contínua para o ledger de assignment, mesmo que o
   resto do sistema aceitasse RPO maior.

9. **Qual é o próximo limite conhecido?** Duas clouds reais, com um segundo
   provedor pago, seria o próximo limite óbvio, e é exatamente o que este
   laboratório decidiu nunca fazer (regra não-negociável do projeto). Dentro
   do que ainda seria possível sem gastar dinheiro real: réplica Kafka
   cross-stack, Helm chart num segundo `kind`, e um dataset de failover em
   escala realista (milhões de linhas, não dezenas) para medir RTO/RPO que
   generalizem além deste laboratório pequeno.

## Fechamento do roadmap: o que os seis tiers provam, juntos

- **tier 1-2**: correção concorrente e idempotência num monólito modular
  (disputa por entregador, chave de idempotência, Redis como projeção
  descartável). É a base que todo tier seguinte reusa sem reescrever.
- **tier 3**: sistema distribuído de verdade com Kafka, outbox, inbox e
  reconciliação, provado com duplicatas e chegada fora de ordem reais.
- **tier 4**: plataforma "AWS" simulada, Terraform real contra LocalStack,
  Helm, KEDA, SBOM/assinatura de imagem, backup/recuperação distribuída
  coordenando banco e log de eventos.
- **tier 5**: arquitetura celular, protocolo de fencing com epoch e lease,
  TLA+ real com mutation test, simulador determinístico com rede/relógio
  virtuais.
- **tier 6**: o mesmo artefato, o mesmo schema e o mesmo protocolo de
  fencing atravessando a fronteira de "provedor", com failover medido e uma
  dependência oculta revelada em vez de escondida.

Nenhum tier terminou só porque as funcionalidades foram implementadas. Os
seis têm teste de corrida, benchmark reproduzível, pelo menos um experimento
de falha, ADR e ferramenta executável por outra pessoa: o critério de
conclusão que `lunch-rush.md` define para qualquer tier, agora fechado para
o roadmap inteiro.
