# Ксайкок mailbridge

Прокси **IMAP** и **SMTP submission** к почтовому HTTP API в стиле xyecoc: те же JSON-тела, что у веб-клиента (`POST /request`, событие `request` в Socket.IO). Процесс слушает **обычный TCP**; TLS обычно терминируется на reverse proxy.

## Возможности

- **IMAP** — списки папок и меток, синхронизация с `account/folders_tags`, чтение списков через `mail/default`, тело письма через `mail/view` и статику с CDN при необходимости.
- **SMTP** — после AUTH: отправка через `mail/message-new`, ответы через `mail/reply-confirm`.
- **Папки и метки** — пользовательские папки (`Folder-<id>`), метки как `Tag-<имя>`; создание меток через IMAP CREATE вида `tag/Имя` или `Labels/Имя` (цвет по умолчанию — `MAILBRIDGE_TAG_DEFAULT_COLOR`).
- **Ограничение входа** — в памяти: с одного IP после **5** неудачных попыток логина (IMAP или SMTP) IP блокируется на **5 минут**. Счётчик общий для обоих протоколов. Успешный вход сбрасывает состояние для этого IP.
- **Логи** — JSON в stdout; при необходимости трассировка HTTP и построчный лог IMAP/SMTP.

## Требования

- Go **1.26+** (см. `go.mod`).
- Доступ к API `POST /request` (по умолчанию `https://api.xyecoc.com`).

## Сборка

```powershell
go build -o mailbridge.exe ./cmd/mailbridge/
```

## Запуск локально

```powershell
$env:REMOTE_HTTP_BASE = "https://api.xyecoc.com"
# опционально, если HTML писем и вложения отдаются только с CDN:
# $env:REMOTE_ASSET_BASE = "https://cdn.xyecoc.com"
.\mailbridge.exe
```

- **IMAP**: адрес из `IMAP_ADDR` (по умолчанию `:143`).
- **SMTP**: `SMTP_ADDR` (по умолчанию `:587`), нужен AUTH, затем `MAIL` / `RCPT` / `DATA`.
- Завершение: **SIGINT** / **SIGTERM** — слушатели закрываются, горутины IMAP/SMTP дожидаются остановки.

## Docker Compose

```bash
docker compose up -d --build
```

Порты по умолчанию `143` и `587`. Переменные можно задать в `.env` (см. `docker-compose.yml`).

## Переменные окружения

| Переменная | По умолчанию | Назначение |
|------------|--------------|------------|
| `REMOTE_HTTP_BASE` | `https://api.xyecoc.com` | Origin для `POST /request` (API; не CDN — иначе часто `unknown_service`) |
| `REMOTE_ASSET_BASE` | *(авто: CDN для api.xyecoc.com)* | Origin для `GET /mail/...` и вложений |
| `MAIL_DEFAULT_DOMAIN` | *(пусто)* | К логину без `@` дописывается домен. `off` / `-` — не дописывать |
| `REMOTE_AUTH_EMAIL_LOCAL_PART_ONLY` | `true` | В `account/authorization` в `data.email` только локальная часть (`baechka`) |
| `MAILBRIDGE_LANG` | `ru` | `currentLang` по умолчанию для многих вызовов |
| `MAILBRIDGE_MAIL_CURRENT_LANG` | `inbox` | `currentLang` для `mail/default` (списки) |
| `MAILBRIDGE_MAIL_VIEW_LANG` | `mail` | `currentLang` для `mail/view` (тело письма) |
| `MAILBRIDGE_MAIL_COMPOSE_LANG` | `mail` | `currentLang` для `message-new` / `reply-confirm` |
| `MAILBRIDGE_TAG_DEFAULT_COLOR` | `ADACF1` | Hex без `#` для `tag-new` при CREATE `tag/...` |
| `IMAP_ADDR` | `:143` | Адрес прослушивания IMAP |
| `IMAP_ALLOW_INSECURE_AUTH` | `true` | Разрешить LOGIN/PLAIN без TLS на сокете (типично за TLS-терминацией на прокси) |
| `SMTP_ADDR` | `:587` | Адрес прослушивания SMTP |
| `HTTP_TIMEOUT_SEC` | `60` | Таймаут HTTP-клиента к API |
| `MAX_MAIL_LIST` | `5000` | Верхняя граница при подгрузке списка писем |
| `LOG_LEVEL` | `info` | `debug` — шире логи; по умолчанию включается трассировка HTTP к API |
| `REMOTE_HTTP_TRACE` | *(как LOG_LEVEL)* | Логировать тела `POST /request` с маскированием секретов |
| `MAILBRIDGE_PROTOCOL_LOG` | `false` | Построчный лог команд/ответов IMAP и SMTP |

### Логи

Вывод в **stdout**, формат **JSON** (одна запись — одна строка).

При старте: `mailbridge starting`, затем `IMAP listening` и `SMTP listening`.

Для отладки: `LOG_LEVEL=debug`, при необходимости `MAILBRIDGE_PROTOCOL_LOG=true`.

В Docker: `docker compose logs -f mailbridge`.

**SMTP «метод аутентификации не поддерживается»:** в клиенте выберите обычный пароль (**PLAIN** / **LOGIN**), не OAuth2.

## Ограничение попыток входа

Поведение зашито в код (`internal/loginlimit`): **5** неудачных попыток с одного IP → блокировка IP на **5 минут**, хранение только в памяти процесса (после рестарта счётчики сбрасываются). Клиенту по-прежнему возвращается обычный отказ в аутентификации.

## Тесты

**Unit (Go), без сети:**

```bash
go test ./... -count=1
```

## Замечания

- Ответы вроде `sent_denied` / `status: 0` приходят от API ксайкока (политика, лимиты), а не от mailbridge.
- JWT и пароли в логах маскируются при трассировке HTTP.
- SMTP хранит **одну** сессию API на IP клиента (последний успешный AUTH). За одним NAT нескольким пользователям может мешать — см. обсуждение в коде/README.
- Полное тело письма подгружается как в веб-клиенте: `mail/view` и при необходимости GET с `REMOTE_ASSET_BASE`.
