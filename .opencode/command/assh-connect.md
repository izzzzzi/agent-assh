---
description: "assh connect — bootstrap SSH access and open tmux session"
---

# assh connect

Connect to a server using assh.

```
assh connect -H HOST -u USER [-i KEY|-E PASS_ENV] [--force-pty] -n NAME
```

For provider server-info blocks: `assh connect-info --file TMP -n NAME`

Returns JSON with `sid` and `next_commands`.
