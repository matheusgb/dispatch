// Teste de breakpoint: carga crescente (arrival rate) até violar um SLO
// de forma segura, com stop condition automática (k6 aborta o teste
// inteiro assim que o threshold com abortOnFail estoura, não deixa a
// carga continuar subindo sobre um sistema já degradado).
//
// SLO usado como linha de corte: http_req_failed rate < 5% OU
// http_req_duration p(95) > 2000ms — bem mais frouxo que o smoke (500ms)
// de propósito, porque o objetivo aqui é achar o ponto de quebra, não
// validar operação normal.
//
// Uso:
//   BASE_URL=http://localhost:8083 k6 run loadtest/k6/breakpoint.js
import http from "k6/http";
import { check, sleep } from "k6";

const BASE_URL = __ENV.BASE_URL || "http://localhost:8083";

export const options = {
  scenarios: {
    breakpoint: {
      executor: "ramping-arrival-rate",
      startRate: 500,
      timeUnit: "1s",
      preAllocatedVUs: 600,
      maxVUs: 3000,
      stages: [
        { target: 1500, duration: "15s" },
        { target: 3000, duration: "15s" },
        { target: 5000, duration: "15s" },
        { target: 8000, duration: "15s" },
        { target: 12000, duration: "20s" },
      ],
    },
  },
  thresholds: {
    http_req_failed: [{ threshold: "rate<0.05", abortOnFail: true, delayAbortEval: "5s" }],
    http_req_duration: [{ threshold: "p(95)<2000", abortOnFail: true, delayAbortEval: "5s" }],
  },
};

export default function () {
  const key = `k6-breakpoint-${__VU}-${__ITER}-${Date.now()}`;

  const res = http.post(`${BASE_URL}/deliveries`, null, {
    headers: { "X-Caller": "k6-breakpoint", "Idempotency-Key": key },
    timeout: "5s",
  });
  check(res, { "criar entrega: 201": (r) => r.status === 201 });

  sleep(0.05);
}
