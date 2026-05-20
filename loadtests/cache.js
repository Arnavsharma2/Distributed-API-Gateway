import http from 'k6/http';
import { check } from 'k6';

export const options = {
  scenarios: {
    constant_request_rate: {
      executor: 'constant-arrival-rate',
      rate: 1000,
      timeUnit: '1s',
      duration: '30s',
      preAllocatedVUs: 50,
      maxVUs: 500,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<150', 'p(99)<300'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  const res = http.get(`${BASE_URL}/api/products?cache_key=portfolio-demo`, {
    headers: { 
      'X-User-ID': 'cache-demo-user',
      'X-Forwarded-For': `10.0.0.${__ITER % 250}`
    },
  });
  check(res, {
    'status is 200': (r) => r.status === 200,
    'cache header present': (r) => r.headers['X-Cache'] !== undefined,
  });
}
