---
description: "assh session exec — run a command in an open assh session"
---

# assh session exec

Run a command in an open assh session and read its output.

```
assh session exec -s SID -- "command"
assh session read -s SID --seq 1 --limit 50
```

Without session: `assh exec -H HOST -u USER -- "command"`
