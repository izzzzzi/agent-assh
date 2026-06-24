# assh Fleet — Multi-Host Operations

Execute the same command across multiple hosts in parallel.

## Basic usage

```bash
assh fleet exec -H host1 -H host2 -H host3 -u root -- "uptime"
assh fleet exec -H web01 -H web02 -u deploy -i ~/.ssh/id_ed25519 -- "df -h"
```

## JSON output

Each host result is returned as a JSON array.
