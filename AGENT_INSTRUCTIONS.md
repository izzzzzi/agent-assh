# assh — Инструкция для LLM-агента

## Что это

`assh` — CLI-инструмент для SSH, решающий две главные проблемы агентов:

1. **Token economy**: команда возвращает метаданные (`stdout_lines: 4327`), а не 1 MB текста.
   Агент решает — читать 20 строк или не читать вообще.

2. **Persistent sessions**: tmux-сессия на сервере. `cd /app` сохраняется, `git pull` выполняется в `/app`.
   Не нужно клеить команды через `&&`.

Текущая основная версия — Go-бинарь `assh`: JSON по умолчанию, system OpenSSH transport, безопасный lifecycle для `tmux`-сессий. Старый Bash MVP сохранён как `assh.bash` для сравнения поведения.

## Где находится

`~/agent_ssh/bin/assh` после сборки. Исходный entrypoint: `~/agent_ssh/cmd/assh`.

## Установка

```bash
cd ~/agent_ssh
go build -o ./bin/assh ./cmd/assh
export PATH="$HOME/agent_ssh/bin:$PATH"
# или
ln -sf ~/agent_ssh/bin/assh /usr/local/bin/assh
```

## Алгоритм работы

```
нужен SSH?
  ├─ серия связанных команд?
  │    assh session open → assh session exec → assh session read → assh session close
  │
  ├─ одна команда?
  │    assh exec → {"stdout_lines": N}
  │    if N > 50: assh read --limit 20 --offset 0 (или --offset end-20)
  │    if N <= 50: assh read --id ID
  │
  ├─ информация о сервере?
  │    assh scan
  │
  └─ первый раз?
       assh key-deploy → ssh-ключ → далее без пароля
```

## Команды

### exec — выполнить команду (metadata only)

```bash
assh exec -H 10.0.0.1 -u root -i ~/.ssh/id_agent_ed25519 -- "df -h"
# → {"ok":true,"exit_code":0,"output_id":"a1b2c3","stdout_lines":10,"stderr_lines":0,"attempt":1,"cwd":"/root"}
```

### read — прочитать вывод с пагинацией

```bash
# Прочитать первые 10 строк
assh read --id a1b2c3 --limit 10 --offset 0

# Прочитать последние 20 строк из 4327
assh read --id a1b2c3 --limit 20 --offset 4307

# Прочитать stderr
assh read --id a1b2c3 --stream stderr --limit 5

# Raw-вывод (для пайпов)
assh read --id a1b2c3 --raw | grep ERROR
```

### session — persistent tmux-сессия

```bash
# Открыть
assh session open -H 10.0.0.1 -u root -i ~/.ssh/id_ed25519 -n deploy
# → {"ok":true,"session":"deploy","sid":"f7a2b3c4"}

# Выполнить команду (cwd сохраняется!)
assh session exec -s f7a2b3c4 -- "cd /var/log"
# → {"ok":true,"rc":0,"seq":1,"stdout_lines":0,"cwd":"/var/log"}

assh session exec -s f7a2b3c4 -- "ls *.log | wc -l"
# → {"ok":true,"rc":0,"seq":2,"stdout_lines":1,"cwd":"/var/log"}   # ЕЩЁ В /var/log!

# Прочитать вывод команды
assh session read -s f7a2b3c4 --seq 2 --limit 10
# → {"ok":true,"content":"142\n","total_lines":1}

# Закрыть
assh session close -s f7a2b3c4
```

### scan — информация о сервере

```bash
assh scan -H 10.0.0.1 -u root -i ~/.ssh/id_ed25519
# → {"hostname":"web01","os":"Ubuntu 22.04","kernel":"5.15.0","arch":"x86_64",
#     "cpu_cores":"4","mem_total_mb":"8192","mem_used_mb":"5120",
#     "disk_root_pct":"45","docker":"24.0.7","ip":"10.0.0.1"}
```

### key-deploy — задеплоить SSH-ключ (один раз)

```bash
export TARGET_PASS="secret"
assh key-deploy -H 10.0.0.1 -u root -E TARGET_PASS
unset TARGET_PASS

# Далее — без пароля
assh exec -H 10.0.0.1 -u root -i ~/.ssh/id_agent_ed25519 -- "hostname"
```

### audit — аудит-лог

```bash
assh audit --last 20          # последние 20 записей
assh audit --failed           # только ошибки
assh audit --host 10.0.0.1   # фильтр по хосту
```

## Ошибки

```json
{"ok":false,"error":"auth_failed"}         → неверный пароль/ключ, спроси у пользователя
{"ok":false,"error":"host_key_failed"}     → новый хост, нужно принять
{"ok":false,"error":"connection_error"}     → хост недоступен, попробуй позже
{"ok":false,"error":"timeout"}              → команда зависла, уменьши или разбей
{"ok":false,"error":"all_retries_failed"}   → 3 попытки не удались
```

## Token Economy — почему это важно

Без assh: `ssh host "journalctl -p warning"` вливает 4327 строк (1.1 MB, 282K токенов) в контекст агента.
Агент теряет способность рассуждать после 3-4 таких команд.

С assh: агент видит `{"stdout_lines":4327}` (50 байт) и решает прочитать только последние 20 строк.
Экономия: **99.3% токенов** (3.3K вместо 282K для 8-командной диагностики).

## Безопасность

1. Пароль — ТОЛЬКО через env (`-E`), никогда в командной строке
2. SSH_ASKPASS — пароль не виден в `ps aux`
3. После `key-deploy`: `unset` переменную — пароль живёт секунды
4. Аудит-лог в `~/.agent_ssh/audit.jsonl` — без паролей

## Требования

- Go 1.22+ для сборки
- ssh, ssh-keygen
- tmux на удалённом сервере (для session, необязательно для exec)
