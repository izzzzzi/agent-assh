# assh

[![CI](https://github.com/agent-ssh/assh/actions/workflows/ci.yml/badge.svg)](https://github.com/agent-ssh/assh/actions/workflows/ci.yml)
[![GitHub Release](https://img.shields.io/github/v/release/agent-ssh/assh)](https://github.com/agent-ssh/assh/releases)
[![npm](https://img.shields.io/npm/v/agent-assh)](https://www.npmjs.com/package/agent-assh)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

SSH-инструмент для рабочих процессов LLM-агентов.

`assh` не отправляет большой SSH-вывод в контекст агента. Команды сначала возвращают метаданные, а агент читает только нужные строки. Persistent sessions используют remote `tmux`, поэтому рабочая директория и окружение сохраняются между связанными командами без поля `cwd` в ответах.

## Установка

```bash
npm i -g agent-assh
assh version
```

Архивы GitHub Releases доступны для Linux, macOS и Windows на поддерживаемых amd64/arm64 платформах.

## Быстрый старт

```bash
assh exec -H 10.0.0.1 -u root -i ~/.ssh/id_ed25519 -- "journalctl -p warning"
assh read --id OUTPUT_ID --limit 20 --offset 0
assh read --id OUTPUT_ID --stream stderr --raw
```

## Persistent Session

```bash
assh session open -H 10.0.0.1 -u root -i ~/.ssh/id_ed25519 -n deploy
assh session exec -s SID -- "cd /app"
assh session exec -s SID --timeout 600 -- "git pull"
assh session read -s SID --seq 2 --limit 20
assh session read -s SID --seq 2 --stream stderr --raw
assh session gc --older-than 24h --execute
assh session close -s SID
```

## Команды

- `assh exec`: выполнить одну remote-команду и сохранить вывод локально.
- `assh read`: прочитать сохранённый вывод с пагинацией или через `--raw`.
- `assh session open|exec|read|close|gc`: persistent workflow через tmux. `session exec` поддерживает `--timeout`.
- `assh capabilities`: проверить поддержку session workflow на сервере.
- `assh scan`: вернуть JSON-инвентарь хоста.
- `assh key-deploy`: поставить SSH-ключ, используя пароль из env.
- `assh audit`: читать локальный аудит через `--last`, `--host`, `--failed`.
- `assh version`: вывести метаданные версии.

## JSON-контракт

Операционные команды по умолчанию печатают один JSON-объект. `read --raw` и `session read --raw` печатают только сохранённый content.

```json
{"ok":true,"exit_code":0,"output_id":"a1b2c3d4","stdout_lines":4327,"stderr_lines":0}
```

```json
{"ok":true,"output_id":"a1b2c3d4","stream":"stdout","offset":0,"limit":20,"total_lines":4327,"has_more":true,"content":"..."}
```

```json
{"ok":true,"rc":0,"seq":2,"stdout_lines":15,"stderr_lines":0,"sid":"f7a2b3c4","session":"deploy"}
```

Ошибки имеют форму:

```json
{"ok":false,"error":"tmux_missing","message":"tmux is not installed"}
```

`exec` и `session exec` считают ненулевой remote status результатом команды, а не transport failure. В объектах ответа нет полей `cwd` и `attempt`.

## Рабочий процесс агента

```bash
assh audit --last 20 --host HOST --failed
```

Используйте `read --raw` для пайпов и точного remote-вывода. Используйте JSON-режим, когда агенту нужны метаданные пагинации.

## Безопасность

- Пароли принимаются только через env-переменные в `key-deploy`.
- Текст команд не пишется в audit log; сохраняется hash.
- Remote cleanup удаляет только sessions с доверенной metadata `assh`.

## English

See [README.md](README.md).
