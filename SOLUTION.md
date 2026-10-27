# Решение тестового задания: сервис управления задачами

Этот файл объясняет, **как** реализован сервис и **почему** приняты те или иные
решения, с привязкой к пунктам ТЗ.

---

## 1. Общая архитектура

Классическая трёхслойная структура с разделением ответственности:

```
HTTP (handler)  →  бизнес-логика (service)  →  хранилище (store)  →  MySQL / Redis
```

- **`internal/handler`** — только транспорт: парсинг запроса, вызов сервиса,
  сериализация ответа и маппинг ошибок в HTTP-коды. Никакой бизнес-логики.
- **`internal/service`** — все правила: роли, права доступа, валидация,
  оркестрация транзакций и кеша. Не знает про HTTP и SQL.
- **`internal/store`** — SQL, транзакции, маппинг строк в сущности. Не знает про
  HTTP и правила доступа.
- **`internal/domain`** — сущности, роли, статусы и sentinel-ошибки. Без внешних
  зависимостей, поэтому на него безопасно ссылаться отовсюду.
- **`internal/cache`** — кеш списка задач в Redis.
- **`internal/middleware`** — JWT-аутентификация.

Ключевое преимущество: **права доступа живут в service** (бизнес-логика), а не
размазаны по SQL-запросам, поэтому их легко покрыть unit-тестами
(`internal/service/rbac_test.go`) и невозможно «забыть» в каком-то из запросов.

---

## 2. Модель данных

Таблицы соответствуют ТЗ (см. `migrations/0001_init.sql`):

- `users(id, email, password_hash, name, created_at)`
- `teams(id, name, created_by, created_at)`
- `team_members(team_id, user_id, role)` — `PRIMARY KEY(team_id, user_id)` даёт
  уникальную пару
- `tasks(id, team_id, title, description, status, created_by, assignee_id,
  created_at, updated_at, closed_at, version)`
- `task_history(id, task_id, changed_by, changes, created_at)` — `changes` это
  JSON-массив `[{"field":"status","old":"todo","new":"done"}, ...]`
- `task_comments(id, task_id, user_id, content, created_at)`

Миграции встроены в бинарник через `//go:embed` и применяются при старте
идемпотентно (`CREATE TABLE IF NOT EXISTS`). Статусы — строковые
(`todo`, `in_progress`, `done`), роли — (`owner`, `admin`, `member`), что
упрощает чтение и тестирование.

---

## 3. Аутентификация

- `POST /api/v1/register` — хеширует пароль через **bcrypt** (`x/crypto`) и
  создаёт пользователя. Повторный email → 409 (`ErrEmailTaken`).
- `POST /api/v1/login` — проверяет bcrypt и выдаёт **JWT** (HS256). В `sub`
  лежит `user_id`.
- Middleware парсит `Authorization: Bearer <token>`, валидирует подпись и срок
  действия, кладёт `user_id` в контекст запроса.

Роль в JWT **не хранится**: роль — это атрибут пары «пользователь+команда»
(`team_members.role`), а не пользователя глобально. Роль всегда читается из БД
в момент проверки, поэтому смена роли вступает в силу сразу, без перевыпуска
токена.

---

## 4. Ролевая модель и права доступа (RBAC)

Логика прав — чистые функции в `internal/service/rbac.go`:

```go
func editScope(role Role, isCreator, isAssignee bool) EditScope
func canInvite(role Role) bool
func canViewStats(role Role) bool
```

Правила из ТЗ разложены так:

| Действие | owner | admin | member |
|---|---|---|---|
| Видеть свою команду / задачи / историю / комментарии | ✓ | ✓ | ✓ |
| Создавать задачи | ✓ | ✓ | ✓ |
| Редактировать **любую** задачу | ✓ | ✓ | — |
| Редактировать **свою** задачу (title/desc/status/assignee) | ✓ | ✓ | ✓ |
| Редактировать **назначенную** задачу (только status) | ✓ | ✓ | ✓ |
| Приглашать / менять роли | ✓ | ✓ | — |
| Смотреть аналитику | ✓ | ✓ | — |

Важные нюансы:

1. **Никаких утечек между командами.** Каждый метод сервиса сначала грузит
   задачу/команду, затем проверяет членство вызывающего через `GetMember` и
   только потом отдаёт данные. `GET /tasks` требует `team_id` и проверку
   членства; история и комментарии проверяют членство в команде задачи;
   статистика — только owner/admin.
2. **Исполнитель из чужой команды запрещён.** При создании/обновлении задачи
   `assignee_id` валидируется через `GetMember(teamID, assigneeID)`; если
   пользователя нет в команде — 400 (`ErrAssigneeNotInTeam`).
3. **Исполнитель не может переназначать задачу.** Для scope `StatusOnly`
   (пользователь = исполнитель, но не создатель) любое изменение
   `title`/`description`/`assignee_id` отклоняется с 403 — разрешён только
   `status`.
4. **Роль `owner` не выдаётся через приглашение.** `Invite` сразу возвращает
   403 для `role=owner`.
5. **Создателя команды нельзя понизить.** Если `team.created_by == target`,
   смена роли запрещена (даже самому себе) — команда не может остаться без
   владельца.

---

## 5. История изменений

Каждое изменение задачи пишет в `task_history` старые и новые значения,
автора (`changed_by`) и время. Формат одной записи:

```json
{
  "id": 1,
  "task_id": 3,
  "changed_by": 2,
  "changes": [
    {"field": "status",  "old": "todo",        "new": "in_progress"},
    {"field": "assignee_id", "old": null,      "new": 5}
  ],
  "created_at": "2026-10-27T10:00:00Z"
}
```

Изменения вычисляются как **diff** между старым и новым состоянием в
`store.UpdateTaskWithHistory`, поэтому в истории только реально изменившиеся
поля. При создании задачи пишется стартовое состояние (все поля с `old=null`).

Переход в `done` автоматически проставляет `closed_at`, уход из `done` —
сбрасывает его в `NULL`.

---

## 6. Транзакционность

Все операции, затрагивающие несколько таблиц, атомарны (обёрнуты в `tx`):

- **Создание команды + owner** — `CreateTeamWithOwner`: один `tx`, два INSERT.
  Команда не может появиться без владельца.
- **Создание задачи + история** — `CreateTaskWithHistory`: один `tx`.
- **Обновление задачи + история** — `UpdateTaskWithHistory`: один `tx`.
  Если `UPDATE` по версии не затронул строку — откат и `ErrConflict`, история
  при этом не пишется.

`defer tx.Rollback()` после каждого `BeginTx` гарантирует, что при любой ошибке
транзакция не останется висеть.

---

## 7. Защита от конкурентных обновлений (optimistic locking)

В таблице `tasks` есть колонка `version`. Обновление выглядит так:

```sql
UPDATE tasks
SET title = ?, ..., version = version + 1
WHERE id = ? AND version = ?   -- ожидаемая версия
```

Если два клиента читают задачу с `version=1` и оба пытаются обновить, то
`WHERE version = 1` выполнится только у первого: у второго `RowsAffected()==0`,
и он получит **409 Conflict**. Так потеря изменений исключена — проигравший
клиент должен перечитать актуальное состояние и повторить запрос с новой
версией.

История при этом всегда строится **от актуального состояния**: внутри
транзакции сначала читается текущая задача, по ней считается diff, и только
потом выполняется `UPDATE ... WHERE version = ?`. Победитель гарантированно
пишет историю относительно того состояния, которое реально изменил.

---

## 8. Кеширование списка задач (Redis)

Кешируется `GET /tasks` с TTL **5 минут**. Ключ учитывает команду **и фильтры**:

```
tasks:list:<team_id>:<generation>:<status>:<assignee_id>:<limit>:<offset>
```

**Инвалидация через счётчик поколения** (generation counter):

- В Redis хранится `tasks:gen:<team_id>` — номер поколения команды.
- Ключи кеша включают этот номер.
- При любом создании/обновлении задачи вызывается `cache.Invalidate(teamID)`,
  который делает `INCR tasks:gen:<team_id>`.
- После инкремента старые ключи **никогда больше не запрашиваются** (новый
  номер поколения даёт новый ключ), а сами устаревшие записи умирают по TTL.

Это лучше, чем `DEL` по маске через `SCAN`: нет O(N) обхода ключей и нет
гонок «кто-то успел перечитать устаревший кеш между обновлением и удалением».

**Graceful degradation:** Redis опционален. Если он недоступен, `Get` вернёт
«промах», `Set` молча пропустит запись, а сервис продолжит работать напрямую с
MySQL. Старт не падает (в `main.go` только warning).

---

## 9. SQL-отчёт (`GET /teams/{team_id}/stats`)

Один запрос с **CTE**, без N+1. В нём есть `JOIN`, `GROUP BY`, агрегаты и работа
с датами (`NOW()`, `DATE_SUB`, `TIMESTAMPDIFF`):

```sql
WITH team_tasks AS (              -- 1) ограничение по team_id (фильтр доступа)
    SELECT id, status, assignee_id, created_at, closed_at
    FROM tasks WHERE team_id = ?
),
status_stats AS (                 -- задачи по статусам
    SELECT status, COUNT(*) cnt FROM team_tasks GROUP BY status
),
top_assignees AS (                -- топ-3 исполнителя по закрытым за 30 дней
    SELECT tt.assignee_id, u.name, COUNT(*) closed_count
    FROM team_tasks tt JOIN users u ON u.id = tt.assignee_id
    WHERE tt.status = 'done' AND tt.closed_at >= DATE_SUB(NOW(), INTERVAL 30 DAY)
    GROUP BY tt.assignee_id, u.name ORDER BY closed_count DESC LIMIT 3
),
avg_close AS (                    -- среднее время закрытия
    SELECT AVG(TIMESTAMPDIFF(SECOND, created_at, closed_at)) avg_close_seconds
    FROM team_tasks WHERE status = 'done' AND closed_at IS NOT NULL
),
comment_stats AS (                -- комментарии по задачам команды
    SELECT COUNT(*) total_comments
    FROM task_comments tc JOIN team_tasks tt ON tt.id = tc.task_id
)
SELECT
    COALESCE((SELECT JSON_OBJECTAGG(status, cnt) FROM status_stats), JSON_OBJECT()),
    COALESCE((SELECT JSON_ARRAYAGG(...) FROM top_assignees), JSON_ARRAY()),
    COALESCE((SELECT avg_close_seconds FROM avg_close), 0),
    COALESCE((SELECT total_comments FROM comment_stats), 0);
```

Метрики собираются в одной SQL-команде и сериализуются в JSON-колонки
(`JSON_OBJECTAGG`, `JSON_ARRAYAGG`), поэтому Go просто разбирает готовый
результат.

**Доступ:** перед вызовом SQL сервис проверяет, что вызывающий — owner/admin
команды (`canViewStats`), иначе 403. Сам запрос жёстко фильтрует по `team_id`,
поэтому данные других команд физически не попадают в ответ.

Ответ:

```json
{
  "status_counts":     {"todo": 1, "in_progress": 2, "done": 4},
  "top_assignees":     [{"assignee_id": 2, "name": "Bob", "closed_count": 3}],
  "avg_close_seconds": 1231200.0,
  "total_comments":    7
}
```

---

## 10. Тестирование

- **Unit-тесты на RBAC** (`internal/service/rbac_test.go`) — табличные тесты
  чистых функций `editScope`, `canInvite`, `canViewStats`. Запускаются без
  внешних зависимостей: `go test ./...`.
- **Интеграционный тест SQL-отчёта** (`internal/store/stats_integration_test.go`,
  build-tag `integration`) — поднимает схему, сеет детерминированные данные и
  проверяет все четыре метрики отчёта. Скипается, если MySQL недоступен.

---

## 11. Соответствие чек-листу ТЗ

| Требование | Где реализовано |
|---|---|
| Аутентификация, команды, задачи, комментарии, история, аналитика | все эндпоинты из ТЗ в `handler/router.go` |
| Права на уровне бизнес-логики, нет утечек между командами | `service/rbac.go` + проверки членства в каждом методе |
| Транзакции для задачи+истории | `store.CreateTaskWithHistory`, `store.UpdateTaskWithHistory` |
| Защита от конкурентных обновлений | optimistic locking по `version` → 409 |
| Кеш списка задач в Redis + инвалидация | `cache/redis.go`, счётчик поколения |
| Swagger/OpenAPI, README, Docker Compose | `docs/openapi.yaml`, `README.md`, `docker-compose.yml` |

## 12. Что из «плюсов» сделано / не сделано

- ✅ Unit-тесты на бизнес-логику прав.
- ✅ Структурированные логи (простой `log`-middleware с методом/путём/временем).
- ⬜ Rate limiting, курсорная пагинация, circuit breaker — не реализованы (это
  плюсы, а не обязательные пункты). Их несложно добавить поверх текущей
  архитектуры: rate limit — middleware, курсорная пагинация — замена
  `LIMIT/OFFSET` на `WHERE id < :cursor`, circuit breaker — обёртка над Redis.
