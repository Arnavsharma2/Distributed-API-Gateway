import http from 'k6/http';
import { check } from 'k6';

export const options = {
  scenarios: {
    ramping_rate: {
      executor: 'ramping-arrival-rate',
      startRate: 50,
      timeUnit: '1s',
      preAllocatedVUs: 50,
      maxVUs: 500,
      stages: [
        { duration: '15s', target: 200 },
        { duration: '30s', target: 500 },
        { duration: '15s', target: 0 },
      ],
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.02'],
    http_req_duration: ['p(95)<300', 'p(99)<600'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  const res = http.get(`${BASE_URL}/api/products?scale=${__ITER % 10}`, {
    headers: { 
      'X-User-ID': `scale-${__VU}`,
      'X-Forwarded-For': `192.168.1.${__ITER % 250}` 
    },
  });
  check(res, {
    'status is 200': (r) => r.status === 200,
  });
}
