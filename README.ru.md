# assh

[![CI](https://github.com/izzzzzi/agent-assh/actions/workflows/ci.yml/badge.svg)](https://github.com/izzzzzi/agent-assh/actions/workflows/ci.yml)
[![GitHub Release](https://img.shields.io/github/v/release/izzzzzi/agent-assh)](https://github.com/izzzzzi/agent-assh/releases)
[![npm](https://img.shields.io/npm/v/agent-assh)](https://www.npmjs.com/package/agent-assh)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Язык: Русский | [English](README.md)

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

Если есть блок с данными сервера от провайдера, сохраните его в локальный файл и дайте `assh` распарсить его:

```bash
assh connect-info --file server.txt -n deploy
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
- `assh connect-info`: распарсить provider server-info block и подключиться.
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

## Примеры для agent CLI

Вставьте эту инструкцию в Codex, Claude Code, OpenCode или другой terminal agent перед передачей SSH-данных:

```text
Используй `assh` для SSH-задач.

Если я вставляю provider server-info block, сохрани весь блок во временный файл с mode 0600, выполни:
assh connect-info --file TMP -n NAME
затем удали TMP.

Если `connect-info` не смог распарсить блок, извлеки host, user и password сам. Пароль положи во временную environment variable и выполни:
assh connect -H HOST -u USER -E PASSWORD_ENV -n NAME
затем удали переменную.

Никогда не печатай, не логируй, не пересказывай и не повторяй пароли. Для всей remote-работы используй возвращенный sid и `next_commands`.
```

Короткие варианты для популярных CLI:

```text
Codex: Используй `assh` для всего SSH-доступа. Для вставленного server info сначала пробуй `assh connect-info --file TMP -n NAME`; не передавай секреты в command arguments и ответы.
```

```text
Claude Code: Перед SSH-работой установи/запусти `assh`. Если вставлены server credentials, сохрани их в 0600 temp file, выполни `assh connect-info --file TMP -n NAME`, удали файл и продолжай через returned sid.
```

```text
OpenCode: Используй `assh connect-info` для provider server-info blocks и `assh session exec/read` после connect. Никогда не echo пароли.
```

## Безопасность

- Пароли принимаются только через env-переменные. Флага `--password` нет.
- `connect-info` читает пароли только из stdin или локального файла, но не из command arguments.
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
https://github.com/izzzzzi/agent-assh/releases
```

## English

See [README.md](README.md).
