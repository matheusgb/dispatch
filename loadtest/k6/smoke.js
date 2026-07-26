// Smoke test do delivery-api: carga mínima, só para validar que o script e
// o ambiente funcionam antes de qualquer cenário maior (load, spike,
// breakpoint, soak). Roda a jornada feliz completa uma vez por VU.
//
// Uso:
//   BASE_URL=http://localhost:8082 k6 run loadtest/k6/smoke.js
import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

export const options = {
  vus: 5,
  duration: "10s",
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500"],
  },
};

export default function () {
  const key = `k6-smoke-${__VU}-${__ITER}-${Date.now()}`;

  let res = http.post(`${BASE_URL}/deliveries`, null, {
    headers: { "X-Caller": "k6-smoke", "Idempotency-Key": key },
  });
  check(res, { "criar entrega: 201": (r) => r.status === 201 });
  const delivery = res.json();

  res = http.post(`${BASE_URL}/deliveries/${delivery.id}/ready`);
  check(res, { "marcar pronta: 204": (r) => r.status === 204 });

  res = http.post(`${BASE_URL}/deliveries/${delivery.id}/offer`, JSON.stringify({ ttl_seconds: 30 }), {
    headers: { "Content-Type": "application/json" },
  });
  check(res, { "oferecer: 204": (r) => r.status === 204 });

  const courierName = `k6-courier-${__VU}-${__ITER}-${Date.now()}`;
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
