// Teste de carga dedicado do tier 4, pendente de uma sessão anterior (ver
// docs/adr/0016 e docs/benchmarks/tier-4-load/README.md): steady state
// seguido por um spike de ~3x, contra o delivery-api real do docker
// compose. Mede a jornada feliz completa (criar -> pronta -> ofertar ->
// cadastrar entregador -> disponibilizar -> atribuir -> coletar -> entregar
// -> consultar), a mesma do smoke.js, com estágios de VUs em vez de VUs
// fixo.
//
// Uso:
//   BASE_URL=http://localhost:8083 k6 run loadtest/k6/tier-4-steady-spike.js
import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

export const options = {
  scenarios: {
    steady_then_spike: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "15s", target: 10 }, // ramp até o steady state
        { duration: "60s", target: 10 }, // steady state: 10 VUs
        { duration: "10s", target: 30 }, // spike: ~3x o steady state
        { duration: "30s", target: 30 }, // sustenta o spike
        { duration: "15s", target: 0 }, // cooldown
      ],
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<1000"],
  },
};

export default function () {
  const key = `k6-tier4-${__VU}-${__ITER}-${Date.now()}`;

  let res = http.post(`${BASE_URL}/deliveries`, null, {
    headers: { "X-Caller": "k6-tier4-load", "Idempotency-Key": key },
  });
  check(res, { "criar entrega: 201": (r) => r.status === 201 });
  if (res.status !== 201) {
    sleep(0.1);
    return;
  }
  const delivery = res.json();

  res = http.post(`${BASE_URL}/deliveries/${delivery.id}/ready`);
  check(res, { "marcar pronta: 204": (r) => r.status === 204 });

  res = http.post(`${BASE_URL}/deliveries/${delivery.id}/offer`, JSON.stringify({ ttl_seconds: 30 }), {
    headers: { "Content-Type": "application/json" },
  });
  check(res, { "oferecer: 204": (r) => r.status === 204 });

  const courierName = `k6-tier4-courier-${__VU}-${__ITER}-${Date.now()}`;
  res = http.post(`${BASE_URL}/couriers`, JSON.stringify({ name: courierName }), {
    headers: { "Content-Type": "application/json" },
  });
  check(res, { "cadastrar entregador: 201": (r) => r.status === 201 });
  const courier = res.json();

  res = http.post(`${BASE_URL}/couriers/${courier.id}/availability`, JSON.stringify({ available: true }), {
    headers: { "Content-Type": "application/json" },
  });
  check(res, { "disponibilizar entregador: 204": (r) => r.status === 204 });

  res = http.post(`${BASE_URL}/deliveries/${delivery.id}/assign`, JSON.stringify({ courier_id: courier.id }), {
    headers: { "Content-Type": "application/json" },
  });
  check(res, { "atribuir: 204": (r) => r.status === 204 });

  res = http.post(`${BASE_URL}/deliveries/${delivery.id}/pickup`);
  check(res, { "coletar: 204": (r) => r.status === 204 });

  res = http.post(`${BASE_URL}/deliveries/${delivery.id}/deliver`);
  check(res, { "concluir: 204": (r) => r.status === 204 });

  res = http.get(`${BASE_URL}/deliveries/${delivery.id}`);
  check(res, {
    "consultar: 200": (r) => r.status === 200,
    "estado final: delivered": (r) => r.json().state === "delivered",
  });

  sleep(0.1);
}
