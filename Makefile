.PHONY: test run compose-up compose-down load-baseline load-rate-limit load-cache load-failure

test:
	go test ./...

run:
	go run ./cmd/gateway -config deploy/docker/gateway.yaml

compose-up:
	docker compose up --build --scale gateway=3

compose-down:
	docker compose down --remove-orphans

load-baseline:
	k6 run loadtests/baseline.js

load-rate-limit:
	k6 run loadtests/rate_limit.js

load-cache:
	k6 run loadtests/cache.js

load-failure:
	k6 run loadtests/upstream_failure.js
