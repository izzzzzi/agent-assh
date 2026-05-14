## assh — SSH для LLM-агентов

Текущая основная версия — Go-бинарь `assh`: JSON по умолчанию, system OpenSSH transport, безопасный lifecycle для `tmux`-сессий. Старый Bash MVP сохранён как `assh.bash` для сравнения поведения.

### Алгоритм для агента

```
нужен SSH?
  ├─ нужна серия связанных команд (cd потом ls потом cat)?
  │    → assh session open
  │    → assh session exec (cwd сохраняется!)
  │    → assh session read --seq N --limit 20
  │    → assh session close
  │
  ├─ одна команда?
  │    → assh exec (metadata only: output_id, stdout_lines)
  │    → if stdout_lines > 50:
  │         assh read --limit 20 --offset 0
  │         assh read --limit 20 --offset end-20
  │    → if stdout_lines <= 50:
  │         assh read (всё)
  │
  ├─ нужна инфа о сервере?
  │    → assh scan (OS, CPU, RAM, disk, Docker, IP)
  │
  └─ первый раз на сервере?
       → assh key-deploy (один раз, потом без пароля)
```

### Ключевые команды

```bash
# Token economy: агент видит СТРОКИ, не мегабайты
assh exec -H host -u root -i key -- "cmd"
  → {"ok":true, "output_id":"a1b2c3", "stdout_lines":4327}

assh read --id a1b2c3 --limit 20 --offset 4300
  → {"ok":true, "content":"...", "total_lines":4327, "has_more":false}

# Persistent sessions: cwd/env живёт
assh session open -H host -u root -i key -n deploy
assh session exec -s SID -- "cd /app"
assh session exec -s SID -- "git pull"   # ещё в /app!
assh session read -s SID --seq 2 --limit 10
assh session close -s SID

# Key deploy (один раз → потом без пароля)
export MY_PASS="..."; assh key-deploy -H host -u root -E MY_PASS; unset MY_PASS

# Scan (OS, CPU, RAM)
assh scan -H host -u root -i key
```

### Ошибки

```json
{"ok":false,"error":"auth_failed"}         → неверный пароль/ключ
{"ok":false,"error":"host_key_failed"}     → новый хост
{"ok":false,"error":"connection_error"}     → хост недоступен
{"ok":false,"error":"timeout"}              → команда зависла
```

### Правила

1. Пароль — ТОЛЬКО через `-E` (env), никогда в команде
2. После key-deploy: `unset` переменную с паролем
3. `auth_failed` → спроси пользователя, не перебирай
4. `stdout_lines > 50` → читай через `read --limit`, не тащи всё в контекст
5. Серия команд к одному хосту → `session` (один tmux, cwd сохраняется)
