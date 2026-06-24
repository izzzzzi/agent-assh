---
description: "assh exec — run a command on a remote host"
---

# assh exec

Run a one-off command on a remote host via assh.

```
assh session exec -s SID -- "command"
assh session read -s SID --seq 1 --limit 50
```

Without session: `assh exec -H HOST -u USER -- "command"`
