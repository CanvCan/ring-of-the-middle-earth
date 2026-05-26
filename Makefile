.PHONY: up down test race logs topics schemas pdf

up:
	docker compose up --build -d

down:
	docker compose down -v

# Run unit tests without Docker (CGO_ENABLED=0 — works on Windows without GCC).
test:
	cd option-b && set "CGO_ENABLED=0" && go test ./tests/... -v

# Run tests with race detector (requires CGO / GCC — Linux/Mac or WSL).
# Used to satisfy B7 (router_test -race) and B9 (goroutine leak detection).
race:
	cd option-b && set "CGO_ENABLED=1" && go test ./tests/... -v -race

# Regenerate architecture.pdf (requires: npm install --prefix ./scripts)
pdf:
	node scripts/gen-arch-pdf.js

logs:
	docker compose logs -f go-1 go-2 go-3

# Show Kafka topic details (K1 evidence)
topics:
	docker compose exec kafka-1 kafka-topics --bootstrap-server kafka-1:29092 --describe

# Show Schema Registry subjects (K2 evidence)
schemas:
	curl -s http://localhost:8081/subjects | python3 -m json.tool
