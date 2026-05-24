.PHONY: proto help seed seed-build dev-up dev-down dev-reset

help:
	@echo "Common targets:"
	@echo "  make proto       - regenerate protobuf bindings"
	@echo "  make dev-up      - start the full stack with seed data via docker compose"
	@echo "  make dev-down    - stop the stack"
	@echo "  make dev-reset   - tear down (incl. volumes) and bring back up clean"
	@echo "  make seed        - run cmd/seed against the local dev database directly"
	@echo "  make seed-build  - build the seed binary into ./bin/seed"

proto:
	protoc --proto_path=internal/tasks/proto --go_out=internal/tasks/proto --go_opt=paths=source_relative internal/tasks/proto/*.proto

seed:
	PRIMARY_DB=$${PRIMARY_DB:-postgres://warmbly:warmbly@localhost:5432/warmbly_dev?sslmode=disable} \
	APP_ENV=dev AWS_CONFIG_ENABLED=false \
	go run ./cmd/seed

seed-build:
	mkdir -p bin
	go build -o bin/seed ./cmd/seed

dev-up:
	cd deploy/docker && docker compose up -d

dev-down:
	cd deploy/docker && docker compose down

dev-reset:
	cd deploy/docker && docker compose down -v
	cd deploy/docker && docker compose up -d
