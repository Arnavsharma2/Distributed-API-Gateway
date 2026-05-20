import http from 'k6/http';
import { check } from 'k6';

http.setResponseCallback(http.expectedStatuses(500, 503));

export const options = {
  scenarios: {
    constant_request_rate: {
      executor: 'constant-arrival-rate',
      rate: 100,
      timeUnit: '1s',
      duration: '30s',
      preAllocatedVUs: 20,
      maxVUs: 100,
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<500'],
    checks: ['rate>0.80'],
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  const res = http.get(`${BASE_URL}/api/error`);
  check(res, {
    'gateway returns expected degraded statuses': (r) => r.status === 500 || r.status === 503,
  });
}
