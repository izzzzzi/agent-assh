# assh

[![CI](https://github.com/agent-ssh/assh/actions/workflows/ci.yml/badge.svg)](https://github.com/agent-ssh/assh/actions/workflows/ci.yml)
[![GitHub Release](https://img.shields.io/github/v/release/agent-ssh/assh)](https://github.com/agent-ssh/assh/releases)
[![npm](https://img.shields.io/npm/v/agent-assh)](https://www.npmjs.com/package/agent-assh)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

SSH-инструмент для LLM-агентов.

`assh` подготавливает SSH-доступ, открывает persistent `tmux`-сессию на сервере и не тащит большой вывод в контекст агента. Команды сначала возвращают компактный JSON с метаданными, а агент читает только нужные строки.

## Быстрый старт

```bash
npm i -g agent-assh

export TARGET_PASS="..."
assh connect -H 203.0.113.10 -u root -E TARGET_PASS -n deploy
unset TARGET_PASS
```

Если вход по ключу уже работает, `assh connect` не читает `TARGET_PASS`.

```bash
assh connect -H 203.0.113.10 -u root -i ~/.ssh/id_agent_ed25519 -n deploy
```

`connect` возвращает session id и `next_commands`:

```json
{
  "ok": true,
  "sid": "f7a2b3c4",
  "session": "deploy",
  "tmux_name": "assh_f7a2b3c4",
  "next_commands": {
    "exec": "assh session exec -s f7a2b3c4 -- \"pwd\"",
    "read": "assh session read -s f7a2b3c4 --seq 1 --limit 50",
    "close": "assh session close -s f7a2b3c4"
  }
}
```

Дальше работайте через session API:

```bash
assh session exec -s f7a2b3c4 -- "pwd"
assh session read -s f7a2b3c4 --seq 1 --limit 50
assh session close -s f7a2b3c4
```

## Что делает connect

`assh connect`:

- создаёт или переиспользует `~/.ssh/id_agent_ed25519`, если не указан `--identity`;
- сначала пробует вход по ключу;
- использует `--password-env` только если вход по ключу не сработал;
- добавляет публичный ключ и проверяет повторный вход по ключу;
- проверяет capabilities сервера;
- ставит `tmux` неинтерактивно, если не указан `--no-install-tmux`;
- безопасно чистит старые доверенные `assh`-сессии, если не указан `--no-gc`;
- открывает доверенную `tmux`-сессию и сохраняет локальную registry metadata.

## Команды

- `assh connect`: первый bootstrap и открытие session.
- `assh session exec|read|close|gc`: persistent workflow через tmux.
- `assh exec`: выполнить одну remote-команду и сохранить вывод локально.
- `assh read`: прочитать сохранённый вывод с пагинацией или через `--raw`.
- `assh capabilities`: проверить поддержку session workflow на сервере.
- `assh scan`: вернуть JSON-инвентарь хоста.
- `assh key-deploy`: низкоуровневая установка ключа через пароль из env.
- `assh audit`: читать локальный audit через `--last`, `--host`, `--failed`.
- `assh version`: вывести метаданные версии.

## Экономия токенов

Сначала смотрите метаданные, потом читайте нужные окна вывода:

```bash
assh session exec -s f7a2b3c4 -- "journalctl -p warning"
assh session read -s f7a2b3c4 --seq 1 --limit 50
assh session read -s f7a2b3c4 --seq 1 --stream stderr --limit 50
```

`--raw` используйте только для пайпов или точного вывода:

```bash
assh session read -s f7a2b3c4 --seq 1 --raw
```

## Безопасность

- Пароли принимаются только через env-переменные. Флага `--password` нет.
- Если вход по ключу работает, `connect` не читает password env var.
- Значения паролей не пишутся в audit logs.
- Текст команд не пишется в audit logs; сохраняются hashes.
- SSH запускается неинтерактивно и отключает pseudo-terminal allocation.
- `--host-key-policy accept-new` используется по умолчанию. Для hardened окружений используйте `strict`.
- `--host-key-policy no-check` небезопасен и подходит только для одноразовых lab/dev хостов.
- Remote cleanup удаляет только sessions с доверенной metadata `assh`.

## Плюсы

- Одна команда делает первый вход, ключ, tmux, cleanup и открытие session.
- Большой вывод не попадает в контекст агента без явного чтения.
- Persistent sessions сохраняют рабочую директорию и окружение между командами.
- JSON-ответы стабильны для парсинга агентом.

## Ограничения

- `tmux` sessions рассчитаны на Unix-like remote hosts.
- Установка пакетов неинтерактивная; неподдерживаемые package managers возвращают machine-readable errors.
- Интерактивные password prompts не поддерживаются в v1.
- `assh` использует системный OpenSSH client.

## Ручная установка

`npm i -g agent-assh` ставит wrapper, который скачивает подходящий Go-бинарь из GitHub Releases. Архивы можно скачать вручную:

```text
https://github.com/agent-ssh/assh/releases
```

## English

See [README.md](README.md).
