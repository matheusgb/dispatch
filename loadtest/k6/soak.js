// Soak test: carga baixa e constante sustentada por uma duração longa,
// procurando degradação lenta (memory leak, conexão vazando, cache
// crescendo sem TTL) que um teste curto não pega. Complementa
// docs/benchmarks/tier-5-soak/ (que usou o LoadGen para uma corrida
// longa orientada a jornada completa incluindo tracking); este script
// cobre o mesmo tipo de cenário só com o delivery-api via k6, mais barato
// de rodar quando o objetivo é só "isso aguenta N minutos sem degradar".
//
// Uso:
//   BASE_URL=http://localhost:8083 DURATION=10m VUS=10 k6 run loadtest/k6/soak.js
import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8083";
const DURATION = __ENV.DURATION || "5m";
const VUS = parseInt(__ENV.VUS || "10", 10);

export const options = {
  scenarios: {
    soak: {
      executor: "constant-vus",
      vus: VUS,
      duration: DURATION,
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<800"],
  },
};

export default function () {
  const key = `k6-soak-${__VU}-${__ITER}-${Date.now()}`;

  let res = http.post(`${BASE_URL}/deliveries`, null, {
    headers: { "X-Caller": "k6-soak", "Idempotency-Key": key },
  });
  check(res, { "criar entrega: 201": (r) => r.status === 201 });
  if (res.status !== 201) {
    sleep(0.2);
    return;
  }
  const delivery = res.json();

  res = http.post(`${BASE_URL}/deliveries/${delivery.id}/ready`);
  check(res, { "marcar pronta: 204": (r) => r.status === 204 });

  res = http.post(`${BASE_URL}/deliveries/${delivery.id}/offer`, JSON.stringify({ ttl_seconds: 30 }), {
    headers: { "Content-Type": "application/json" },
  });
  check(res, { "oferecer: 204": (r) => r.status === 204 });

  sleep(0.5);
}
