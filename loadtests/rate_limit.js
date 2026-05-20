import http from 'k6/http';
import { check } from 'k6';

http.setResponseCallback(http.expectedStatuses({ min: 200, max: 399 }, 429, 503));

export const options = {
  scenarios: {
    constant_request_rate: {
      executor: 'constant-arrival-rate',
      rate: 200,
      timeUnit: '1s',
      duration: '30s',
      preAllocatedVUs: 50,
      maxVUs: 100,
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<300'],
    checks: ['rate>0.90'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  const res = http.get(`${BASE_URL}/api/flaky`, {
    headers: { 'X-User-ID': 'shared-rate-limit-key' },
  });

  check(res, {
    'allowed or limited': (r) => r.status === 200 || r.status === 429 || r.status === 503,
    'rate limit headers exist on 429': (r) => r.status !== 429 || r.headers['X-Ratelimit-Limit'] !== undefined,
  });
}
