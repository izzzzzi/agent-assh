# assh — SSH для LLM-агентов

Две фичи, которые реально нужны:
1. **Token economy** — агент видит строки, а не мегабайты
2. **Persistent sessions** — cwd/env живёт между командами

Note: v2 targets a Go binary with JSON output by default, system OpenSSH transport, and safe `tmux` session lifecycle. During development, the existing Bash `assh` remains the reference implementation.

## Быстрый старт

```bash
# Token economy: exec → метаданные, read → только нужное
assh exec -H 10.0.0.1 -u root -i ~/.ssh/id_ed25519 -- "journalctl -p warning"
# → {"ok":true,"output_id":"a1b2c3","stdout_lines":4327}

assh read --id a1b2c3 --limit 20 --offset 4307
# → {"ok":true,"content":"last 20 lines","total_lines":4327,"has_more":false}

# Persistent session: cwd сохраняется
assh session open -H 10.0.0.1 -u root -i ~/.ssh/id_ed25519 -n deploy
# → {"ok":true,"session":"deploy","sid":"f7a2"}

assh session exec -s f7a2 -- "cd /var/log"
# → {"ok":true,"rc":0,"seq":1,"cwd":"/var/log"}

assh session exec -s f7a2 -- "ls *.log | wc -l"
# → {"ok":true,"rc":0,"seq":2,"stdout_lines":1,"cwd":"/var/log"}

assh session read -s f7a2 --seq 2 --limit 10
assh session close -s f7a2

# Key deploy (один раз → потом без пароля)
export MY_PASS="secret"
assh key-deploy -H 10.0.0.1 -u root -E MY_PASS && unset MY_PASS

# Server scan
assh scan -H 10.0.0.1 -u root -i ~/.ssh/id_ed25519
# → {"hostname":"web01","os":"Ubuntu 22.04","cpu_cores":4,...}
```

## Token Economy: до и после

```
Без assh (обычный SSH):
  dmesg         → 2,400 строк  → 620 KB в контекст
  journalctl    → 4,327 строк  → 1.1 MB
  free -h       → 3 строки     → OK
  df -h          → 10 строк    → OK
  ─────────────────────────────────────
  ИТОГО: ~1.9 MB / ~282,000 токенов → контекст переполнен

С assh (token economy):
  assh exec dmesg → {"stdout_lines":2400}              → 50 байт
  assh read --limit 20 --offset 2380                    → 5 KB
  assh exec journalctl → {"stdout_lines":4327}           → 50 байт
  assh read --limit 20 --stream stderr                  → 2 KB
  assh exec free → {"stdout_lines":3} + read            → 300 байт
  assh exec df → {"stdout_lines":10} + read             → 1 KB
  ─────────────────────────────────────
  ИТОГО: ~13 KB / ~3,300 токенов → 99.3% экономия
```

## Все команды

```
assh exec      — выполнить команду, вернуть метаданные
assh read      — прочитать вывод с пагинацией
assh scan      — собрать инфу о сервере
assh session   — persistent tmux-сессии (open/exec/read/close/list)
assh key-deploy — задеплоить SSH-ключ
assh audit     — аудит-лог
assh connections — активные подключения
```

## Формат ответа

```json
// exec — ТОЛЬКО метаданные
{"ok":true,"exit_code":0,"output_id":"a1b2c3","stdout_lines":4327,"stderr_lines":0,"attempt":1,"cwd":"/root"}

// read — пагинированный payload
{"ok":true,"output_id":"a1b2c3","stream":"stdout","offset":4300,"limit":20,"total_lines":4327,"has_more":false,"content":"..."}

// session exec — метаданные
{"ok":true,"rc":0,"seq":2,"stdout_lines":15,"stderr_lines":0,"cwd":"/var/log"}

// ошибки
{"ok":false,"error":"auth_failed"}
{"ok":false,"error":"host_key_failed"}
{"ok":false,"error":"connection_error"}
```

## Требования

- bash 4+, ssh, ssh-keygen
- tmux на сервере (для session)
- python3 (для JSON, вспомогательно)
