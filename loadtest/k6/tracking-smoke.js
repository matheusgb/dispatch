// Smoke test do caminho de tracking: emissão de token, ingestão de GPS e
// leitura da última posição. Precisa de ADMIN_SECRET porque a emissão de
// token é administrativa no tier 2 (ver internal/platform/auth).
//
// Uso:
//   BASE_URL=http://localhost:8083 ADMIN_SECRET=compose-dev-admin-secret \
//     k6 run loadtest/k6/tracking-smoke.js
import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";
const ADMIN_SECRET = __ENV.ADMIN_SECRET;

export const options = {
  vus: 5,
  duration: "10s",
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<500"],
  },
};

export default function () {
  const caller = `k6-tracking-${__VU}`;
  const key = `k6-tracking-${__VU}-${__ITER}-${Date.now()}`;

  let res = http.post(`${BASE_URL}/deliveries`, null, {
    headers: { "X-Caller": caller, "Idempotency-Key": key },
  });
  check(res, { "criar entrega: 201": (r) => r.status === 201 });
  const delivery = res.json();

  res = http.post(`${BASE_URL}/auth/tokens`, JSON.stringify({ caller }), {
    headers: { "Content-Type": "application/json", "X-Admin-Secret": ADMIN_SECRET },
  });
  check(res, { "emitir token: 201": (r) => r.status === 201 });
  const token = res.json().token;
  const auth = { Authorization: `Bearer ${token}` };

  res = http.post(
    `${BASE_URL}/deliveries/${delivery.id}/positions`,
    JSON.stringify({ tracking_session_epoch: 1, sequence: __ITER + 1, latitude: -23.55, longitude: -46.63 }),
    { headers: { ...auth, "Content-Type": "application/json" } }
  );
  check(res, { "registrar posição: 202": (r) => r.status === 202 });

  res = http.get(`${BASE_URL}/deliveries/${delivery.id}/position`, { headers: auth });
  check(res, {
    "consultar posição: 200": (r) => r.status === 200,
    "sequence corresponde": (r) => r.json().sequence === __ITER + 1,
  });

  sleep(0.2);
}
