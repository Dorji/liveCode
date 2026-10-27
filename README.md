# Task Manager — сервис управления задачами

REST API сервис для управления задачами внутри команд: ролевая модель
(owner / admin / member), история изменений, кеширование списка задач в Redis
и SQL-отчёт по команде.

Стек: **Go, MySQL, Redis, Docker Compose, Swagger/OpenAPI**.

## Быстрый старт

```bash
docker compose up --build
```

Сервис поднимется на `http://localhost:8080`. При первом старте он автоматически
применит миграции (idempotent `CREATE TABLE IF NOT EXISTS`).

- Swagger UI: http://localhost:8080/swagger
- OpenAPI spec: http://localhost:8080/openapi.yaml
- Healthcheck: `GET /healthz`

## Конфигурация

Всё настраивается через переменные окружения (без хардкода):

| Переменная    | По умолчанию                                          | Описание                    |
|---------------|-------------------------------------------------------|-----------------------------|
| `SERVER_PORT` | `8080`                                                | Порт HTTP                   |
| `MYSQL_DSN`   | `taskuser:taskpass@tcp(localhost:3306)/taskdb?...`     | DSN MySQL                   |
| `REDIS_ADDR`  | `localhost:6379`                                      | Адрес Redis                 |
| `JWT_SECRET`  | `dev-secret-change-me`                                | Секрет для подписи JWT      |
| `JWT_EXPIRY`  | `24h`                                                 | Время жизни токена          |
| `CACHE_TTL`   | `5m`                                                  | TTL кеша списка задач       |

Redis опционален: если он недоступен, сервис продолжает работать без кеша.

## Миграции

Миграции находятся в `migrations/0001_init.sql` и встроены в бинарник
(`//go:embed`). Применяются автоматически при старте. Для локального запуска
без Docker нужен заранее созданный MySQL:

```bash
docker compose up -d mysql redis
MYSQL_DSN="taskuser:taskpass@tcp(127.0.0.1:3306)/taskdb?parseTime=true&loc=UTC" \
REDIS_ADDR="127.0.0.1:6379" \
  go run ./cmd/server
```

## Примеры запросов

```bash
BASE=http://localhost:8080

# Регистрация и получение токена
curl -s -X POST $BASE/api/v1/register -H 'Content-Type: application/json' \
  -d '{"email":"a@x.com","password":"secret","name":"Alice"}'
# -> {"data":{"user":{...},"token":"<JWT>"}}

TOKEN=<JWT из ответа>

# Создание команды (текущий пользователь становится owner)
curl -s -X POST $BASE/api/v1/teams -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"name":"Backend"}'

# Приглашение участника
curl -s -X POST $BASE/api/v1/teams/1/invite -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"user_id":2,"role":"member"}'

# Создание задачи
curl -s -X POST $BASE/api/v1/tasks -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"team_id":1,"title":"Fix auth","description":"...","assignee_id":2}'

# Список задач с фильтрами и пагинацией
curl -s "$BASE/api/v1/tasks?team_id=1&status=todo&limit=20&offset=0" \
  -H "Authorization: Bearer $TOKEN"

# Обновление задачи (version для optimistic locking берётся из текущего состояния)
curl -s -X PUT $BASE/api/v1/tasks/1 -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"status":"in_progress"}'

# История и комментарии
curl -s $BASE/api/v1/tasks/1/history -H "Authorization: Bearer $TOKEN"
curl -s -X POST $BASE/api/v1/tasks/1/comments -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"content":"looking into it"}'

# SQL-отчёт (только owner/admin)
curl -s $BASE/api/v1/teams/1/stats -H "Authorization: Bearer $TOKEN"
```

## Тестирование

```bash
# unit-тесты (права доступа) — без внешних зависимостей
go test ./...

# интеграционный тест SQL-отчёта — нужен MySQL
docker compose up -d mysql
docker compose exec mysql mysql -uroot -prootpass -e "CREATE DATABASE IF NOT EXISTS taskdb_test"
TEST_MYSQL_DSN="taskuser:taskpass@tcp(127.0.0.1:3306)/taskdb_test?parseTime=true&loc=UTC" \
  go test -tags integration ./internal/store/ -run TestTeamStats -v
```

## Структура проекта

```
cmd/server/          точка входа
internal/config/     конфигурация из env
internal/domain/     сущности, роли, статусы, ошибки
internal/store/      MySQL: SQL, транзакции, миграции, SQL-отчёт
internal/service/    бизнес-логика и RBAC
internal/cache/      Redis-кеш списка задач
internal/handler/    HTTP-обработчики и роутер
internal/middleware/ JWT-аутентификация
migrations/          SQL-миграции (embed)
docs/                OpenAPI spec (embed)
```

Детальное объяснение решений и соответствия ТЗ — в [SOLUTION.md](SOLUTION.md).
