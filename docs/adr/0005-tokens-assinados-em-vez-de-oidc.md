# ADR 0005: tokens assinados em vez de um servidor OIDC local

## Contexto

O tracking precisa de autenticação (invariante de segurança: impedir que
um caller consulte o tracking de uma entrega alheia). O roadmap aceita
"OIDC local ou tokens assinados" como alternativas equivalentes no tier 2.

## Decisão

Tokens JWT HS256 (`internal/platform/auth`), emitidos por um endpoint
administrativo (`POST /auth/tokens`) protegido por um segredo
compartilhado, sem servidor de identidade separado.

## Alternativas consideradas

- **Keycloak ou Dex local:** rejeitado por agora. Um servidor OIDC exigiria
  subir e operar mais um serviço (com seu próprio banco, configuração de
  realm, clientes registrados) para resolver o mesmo problema que um HS256
  simples resolve no tier 2: identificar o caller. Isso entra quando
  houver um motivo medido, por exemplo múltiplos serviços precisando
  confiar na mesma autoridade sem compartilhar segredo, ou um requisito
  real de login de usuário final.
- **mTLS entre serviços:** fora de escopo. Não há múltiplos serviços
  ainda; o tier 2 continua monólito modular.

## Consequências

- o segredo compartilhado (`DISPATCH_JWT_SECRET`) precisa ser gerenciado
  como qualquer outro segredo de produção nos tiers seguintes: fora do
  código, com rotação. No tier 2 local ele é só uma variável de ambiente;
- não existe fluxo de login nem revogação de token antes da expiração: um
  token comprometido continua válido até o TTL (1 hora) esgotar. Isso é
  uma limitação aceitável no tier 2 e fica documentada, não escondida;
- emitir um token exige o segredo administrativo, então o LunchRush e o k6
  precisam dele para testar tracking (`DISPATCH_ADMIN_SECRET`). Isso é
  intencional: simula a fronteira entre "quem pode emitir identidade" e
  "quem usa a identidade emitida".

## Status

Aceita.
