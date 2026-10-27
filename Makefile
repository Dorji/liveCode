# Task Manager — build / test / run targets.
.DEFAULT_GOAL := help

.PHONY: help build run test test-integration lint fmt tidy up down db-up db-down test-db

help: ## Показать список целей
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Собрать все пакеты
	go build ./...

run: ## Локальный запуск сервиса (нужны MySQL/Redis на localhost)
	go run ./cmd/server

test: ## Unit-тесты без внешних зависимостей (RBAC, rate limiter)
	go test ./...

test-integration: ## Интеграционные тесты (нужен MySQL: make db-up и make test-db)
	TEST_MYSQL_DSN="$${TEST_MYSQL_DSN:-taskuser:taskpass@tcp(127.0.0.1:3306)/taskdb_test?parseTime=true&loc=UTC}" \
		go test -tags integration -count=1 -p 1 ./...

lint: ## Статическая проверка: go vet + gofmt
	go vet ./...
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi

fmt: ## Отформатировать все .go файлы
	gofmt -w .

tidy: ## Обновить зависимости и go.sum
	go mod tidy

up: ## Поднять весь стек (app + mysql + redis)
	docker compose up -d --build

down: ## Остановить и удалить контейнеры
	docker compose down

db-up: ## Поднять только MySQL и Redis (для go run и интеграционных тестов)
	docker compose up -d mysql redis

db-down: ## Остановить MySQL и Redis
	docker compose stop mysql redis

test-db: ## Создать тестовую БД и выдать права (однократно)
	docker compose exec -T mysql mysql -uroot -prootpass \
		-e "CREATE DATABASE IF NOT EXISTS taskdb_test CHARACTER SET utf8mb4; GRANT ALL PRIVILEGES ON taskdb_test.* TO 'taskuser'@'%'; FLUSH PRIVILEGES;"
