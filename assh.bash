#!/usr/bin/env bash
#
# assh — SSH для LLM-агентов
#
# Две ключевые фичи:
#   1. Token economy: exec возвращает метаданные, read — пагинированный контент
#   2. Persistent sessions: cwd/env живёт между командами (tmux)
#
# Использование:
#   assh exec    -H host -u root -i key -- "journalctl -p warning"
#     → {"ok":true,"output_id":"a1b2c3","stdout_lines":4327}
#
#   assh read --id a1b2c3 --limit 20 --offset 4300
#     → 20 строк из 4327 (агент не тащит всё в контекст)
#
#   assh session open -H host -u root -i key -n deploy
#     → {"ok":true,"session":"deploy","sid":"a1b2c3"}
#   assh session exec -s a1b2c3 -- "cd /var/log"
#     → {"ok":true,"rc":0,"seq":1,"cwd":"/var/log"}
#   assh session close -s a1b2c3

set -euo pipefail

AGENT_SSH_DIR="$HOME/.agent_ssh"
AUDIT_LOG="$AGENT_SSH_DIR/audit.jsonl"
OUTPUT_DIR="$AGENT_SSH_DIR/output"
SESSIONS_DIR="$AGENT_SSH_DIR/sessions"
SSH_CONTROL_DIR="$AGENT_SSH_DIR/ctrl"
RETRIES="${ASSH_RETRIES:-3}"
TIMEOUT="${ASSH_TIMEOUT:-10}"

mkdir -p "$AGENT_SSH_DIR" "$OUTPUT_DIR" "$SESSIONS_DIR" "$SSH_CONTROL_DIR"

# ═══ Утилиты ═══════════════════════════════════════════════════

generate_id() { openssl rand -hex 4 2>/dev/null || head -c 4 /dev/urandom | xxd -p; }

audit_log() {
    local action="$1"; shift
    echo "{\"ts\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"action\":\"$action\",$*}" >> "$AUDIT_LOG"
}

ssh_opts() {
    local host="$1" user="${2:-root}" port="${3:-22}" identity="${4:-}" timeout="${5:-10}"
    local opts=(
        -o "StrictHostKeyChecking=accept-new"
        -o "ConnectTimeout=$timeout"
        -o "ServerAliveInterval=30"
        -o "ServerAliveCountMax=3"
    )
    [[ -n "$port" && "$port" != "22" ]] && opts+=(-p "$port")
    [[ -n "$identity" ]] && opts+=(-i "$identity")

    local ctrl_key
    ctrl_key=$(echo -n "${user}@${host}:${port}" | shasum -a 256 2>/dev/null | cut -c1-12 || echo "${user}${host}${port}" | md5 2>/dev/null | cut -c1-12)
    opts+=(
        -o "ControlMaster=auto"
        -o "ControlPath=$SSH_CONTROL_DIR/ctrl-${ctrl_key}"
        -o "ControlPersist=600"
    )
    echo "${opts[*]}"
}

# ═══ exec — выполнение команды с token economy ══════════════════

cmd_exec() {
    local host="" user="root" port="22" identity="" env_var=""
    local cmd_args=()
    local past_separator=false

    while [[ $# -gt 0 ]]; do
        if [[ "$past_separator" == true ]]; then cmd_args+=("$1"); shift; continue; fi
        case "$1" in
            -H|--host)      host="$2"; shift 2 ;;
            -u|--user)      user="$2"; shift 2 ;;
            -p|--port)      port="$2"; shift 2 ;;
            -i|--identity)  identity="$2"; shift 2 ;;
            -E|--password-env) env_var="$2"; shift 2 ;;
            -t|--timeout)   TIMEOUT="$2"; shift 2 ;;
            -r|--retries)   RETRIES="$2"; shift 2 ;;
            --)             past_separator=true; shift ;;
            *)              cmd_args+=("$1"); shift ;;
        esac
    done

    [[ -z "$host" ]] && { echo '{"ok":false,"error":"--host required"}'; return 1; }
    [[ ${#cmd_args[@]} -eq 0 ]] && { echo '{"ok":false,"error":"command required"}'; return 1; }

    local output_id
    output_id=$(generate_id)
    local output_file="$OUTPUT_DIR/${output_id}"
    local stderr_file="$OUTPUT_DIR/${output_id}.err"
    local ssh_args
    ssh_args=$(ssh_opts "$host" "$user" "$port" "$identity" "$TIMEOUT")
    local target="${user}@${host}"
    local cmd_str="${cmd_args[*]}"

    local use_password=false password=""
    if [[ -n "$identity" ]] && [[ -f "$identity" ]]; then
        : # key auth
    elif [[ -n "$env_var" ]]; then
        password="${!env_var:-}"
        [[ -z "$password" ]] && { echo "{\"ok\":false,\"error\":\"Env var $env_var is empty\"}"; return 1; }
        use_password=true
    else
        echo '{"ok":false,"error":"No auth method. Use -i <key> or -E <env_var>"}'; return 1
    fi

    local attempt=1 rc=0
    while [[ $attempt -le $RETRIES ]]; do
        if [[ "$use_password" == true ]]; then
            local askpass_script
            askpass_script=$(mktemp /tmp/askpass_XXXXXX.sh)
            printf '#!/bin/sh\necho %s\n' "$password" > "$askpass_script"
            chmod 700 "$askpass_script"
            trap "rm -f '$askpass_script'" RETURN
            SSH_ASKPASS="$askpass_script" SSH_ASKPASS_REQUIRE=force DISPLAY="${DISPLAY:-:0}" \
                timeout 300 ssh $ssh_args "$target" "$cmd_str" \
                > "$output_file" 2> "$stderr_file" && rc=0 || rc=$?
            rm -f "$askpass_script"; trap - RETURN
        else
            timeout 300 ssh $ssh_args "$target" "$cmd_str" \
                > "$output_file" 2> "$stderr_file" && rc=0 || rc=$?
        fi

        local stderr_content
        stderr_content=$(cat "$stderr_file" 2>/dev/null || echo "")

        if [[ $rc -eq 0 ]]; then
            local stdout_lines stderr_lines cwd
            stdout_lines=$(wc -l < "$output_file" 2>/dev/null || echo 0)
            stderr_lines=$(wc -l < "$stderr_file" 2>/dev/null || echo 0)
            cwd=$(ssh $ssh_args "$target" "pwd 2>/dev/null" 2>/dev/null || echo "")
            find "$OUTPUT_DIR" -type f -mmin +60 -delete 2>/dev/null || true
            audit_log "exec" "\"host\":\"$host\",\"output_id\":\"$output_id\",\"exit_code\":0,\"stdout_lines\":$stdout_lines"
            printf '{"ok":true,"exit_code":0,"output_id":"%s","stdout_lines":%d,"stderr_lines":%d,"attempt":%d,"cwd":"%s"}\n' \
                "$output_id" "$stdout_lines" "$stderr_lines" "$attempt" "$cwd"
            return 0
        fi

        case "$stderr_content" in
            *"Permission denied"*|*"Authentication failed"*)
                rm -f "$output_file" "$stderr_file"
                echo '{"ok":false,"error":"auth_failed"}'; return 1 ;;
            *"Host key verification failed"*)
                rm -f "$output_file" "$stderr_file"
                echo '{"ok":false,"error":"host_key_failed"}'; return 1 ;;
        esac

        [[ $attempt -lt $RETRIES ]] && sleep $((2 ** attempt))
        attempt=$((attempt + 1))
    done

    rm -f "$output_file" "$stderr_file"
    printf '{"ok":false,"error":"all_retries_failed","attempts":%d}\n' "$RETRIES"
    return 1
}

# ═══ read — пагинированное чтение вывода (token economy) ══════════

cmd_read() {
    local id="" offset=0 limit=50 stream="stdout" raw=false

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --id)      id="$2"; shift 2 ;;
            --offset)  offset="$2"; shift 2 ;;
            --limit)   limit="$2"; shift 2 ;;
            --stream)  stream="$2"; shift 2 ;;
            --raw)     raw=true; shift ;;
            *)         shift ;;
        esac
    done

    [[ -z "$id" ]] && { echo '{"ok":false,"error":"--id required"}'; return 1; }

    local file="$OUTPUT_DIR/${id}"
    [[ "$stream" == "stderr" ]] && file="${file}.err"

    if [[ ! -f "$file" ]]; then
        printf '{"ok":false,"error":"output %s not found (expired or wrong id)"}\n' "$id"
        return 1
    fi

    local total_lines
    total_lines=$(wc -l < "$file" 2>/dev/null || echo 0)

    if [[ "$raw" == true ]]; then
        tail -n +$((offset + 1)) "$file" | head -n "$limit"
        return 0
    fi

    local content
    if [[ $total_lines -le $limit && $offset -eq 0 ]]; then
        content=$(python3 -c 'import sys,json; print(json.dumps(sys.stdin.read()))' < "$file" 2>/dev/null || echo '""')
    else
        content=$(tail -n +$((offset + 1)) "$file" | head -n "$limit" | python3 -c 'import sys,json; print(json.dumps(sys.stdin.read()))' 2>/dev/null || echo '""')
    fi

    local has_more="false"
    [[ $((offset + limit)) -lt $total_lines ]] && has_more="true"

    printf '{"ok":true,"output_id":"%s","stream":"%s","offset":%d,"limit":%d,"total_lines":%d,"has_more":%s,"content":%s}\n' \
        "$id" "$stream" "$offset" "$limit" "$total_lines" "$has_more" "$content"
}

# ═══ session — persistent tmux sessions ═══════════════════════════

cmd_session() {
    local subcmd="${1:-help}"; shift || true
    case "$subcmd" in
        open)   session_open "$@" ;;
        exec)   session_exec "$@" ;;
        read)   session_read "$@" ;;
        close)  session_close "$@" ;;
        list)   session_list "$@" ;;
        help|*) session_help ;;
    esac
}

_session_ssh() {
    local sid="$1"; shift
    local session_file="$SESSIONS_DIR/${sid}.json"
    [[ ! -f "$session_file" ]] && { echo '{"ok":false,"error":"session not found"}'; return 1; }
    local host user port identity
    host=$(python3 -c "import json; print(json.load(open('$session_file'))['host'])" 2>/dev/null)
    user=$(python3 -c "import json; print(json.load(open('$session_file'))['user'])" 2>/dev/null)
    port=$(python3 -c "import json; print(json.load(open('$session_file')).get('port','22'))" 2>/dev/null)
    identity=$(python3 -c "import json; print(json.load(open('$session_file')).get('identity',''))" 2>/dev/null)
    local ssh_args
    ssh_args=$(ssh_opts "$host" "$user" "$port" "$identity" "$TIMEOUT")
    ssh $ssh_args "${user}@${host}" "$@"
}

session_open() {
    local host="" user="root" port="22" identity="" env_var="" name=""

    while [[ $# -gt 0 ]]; do
        case "$1" in
            -H|--host)      host="$2"; shift 2 ;;
            -u|--user)      user="$2"; shift 2 ;;
            -p|--port)      port="$2"; shift 2 ;;
            -i|--identity)  identity="$2"; shift 2 ;;
            -E|--password-env) env_var="$2"; shift 2 ;;
            -n|--name)      name="$2"; shift 2 ;;
            *)              shift ;;
        esac
    done

    [[ -z "$host" ]] && { echo '{"ok":false,"error":"--host required"}'; return 1; }
    [[ -z "$name" ]] && name="a-$(generate_id)"
    local sid
    sid=$(generate_id)

    local ssh_args
    ssh_args=$(ssh_opts "$host" "$user" "$port" "$identity" "$TIMEOUT")
    local target="${user}@${host}"

    local use_password=false password=""
    if [[ -n "$env_var" ]]; then
        password="${!env_var:-}"
        [[ -z "$password" ]] && { echo '{"ok":false,"error":"env var empty"}'; return 1; }
        use_password=true
    fi

    local setup_cmd="command -v tmux >/dev/null 2>&1 || echo NOTMUX; tmux new-session -d -s $name 2>/dev/null; mkdir -p /tmp/assh_sessions/$name; echo SESSION_OK"

    local rc=0 result
    if [[ "$use_password" == true ]]; then
        local askpass_script
        askpass_script=$(mktemp /tmp/askpass_XXXXXX.sh)
        printf '#!/bin/sh\necho %s\n' "$password" > "$askpass_script"
        chmod 700 "$askpass_script"
        result=$(SSH_ASKPASS="$askpass_script" SSH_ASKPASS_REQUIRE=force DISPLAY="${DISPLAY:-:0}" \
            timeout 30 ssh $ssh_args "$target" "$setup_cmd" 2>&1) || rc=$?
        rm -f "$askpass_script"
    else
        result=$(timeout 30 ssh $ssh_args "$target" "$setup_cmd" 2>&1) || rc=$?
    fi

    if echo "$result" | grep -q "NOTMUX"; then
        echo '{"ok":false,"error":"tmux not installed on remote. Use assh exec for stateless commands."}'
        return 1
    fi

    if echo "$result" | grep -q "SESSION_OK"; then
        printf '{"sid":"%s","name":"%s","host":"%s","user":"%s","port":"%s","identity":"%s","created":"%s"}\n' \
            "$sid" "$name" "$host" "$user" "$port" "$identity" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
            > "$SESSIONS_DIR/${sid}.json"

        audit_log "session_open" "\"sid\":\"$sid\",\"name\":\"$name\",\"host\":\"$host\""
        printf '{"ok":true,"session":"%s","sid":"%s","host":"%s","user":"%s"}\n' "$name" "$sid" "$host" "$user"
    else
        printf '{"ok":false,"error":"Failed to create session"}\n'
        return 1
    fi
}

session_exec() {
    local sid="" cmd_str=""
    local past_separator=false

    while [[ $# -gt 0 ]]; do
        if [[ "$past_separator" == true ]]; then cmd_str="$cmd_str $1"; shift; continue; fi
        case "$1" in
            -s|--sid)   sid="$2"; shift 2 ;;
            --)         past_separator=true; shift ;;
            *)          cmd_str="$cmd_str $1"; shift ;;
        esac
    done
    cmd_str=$(echo "$cmd_str" | sed 's/^ *//')

    [[ -z "$sid" ]] && { echo '{"ok":false,"error":"--sid required"}'; return 1; }
    [[ -z "$cmd_str" ]] && { echo '{"ok":false,"error":"command required"}'; return 1; }

    local session_file="$SESSIONS_DIR/${sid}.json"
    [[ ! -f "$session_file" ]] && { echo "{\"ok\":false,\"error\":\"session $sid not found\"}"; return 1; }

    local name
    name=$(python3 -c "import json; print(json.load(open('$session_file'))['name'])" 2>/dev/null)

    # Step 1: get next seq
    local seq
    seq=$(_session_ssh "$sid" "
        SEQ_FILE=/tmp/assh_sessions/${name}.seq
        OUT_DIR=/tmp/assh_sessions/${name}
        mkdir -p \$OUT_DIR
        if [ -f \$SEQ_FILE ]; then
            SEQ=\$(( \$(cat \$SEQ_FILE) + 1 ))
        else
            SEQ=1
        fi
        echo \$SEQ > \$SEQ_FILE
        echo \$SEQ
    " 2>/dev/null)
    seq=$(echo "$seq" | grep -oE '[0-9]+' | head -1)
    [[ -z "$seq" ]] && seq=1

    # Step 2: send command to tmux
    _session_ssh "$sid" "
        SEQ=$seq
        OUT_DIR=/tmp/assh_sessions/${name}
        tmux send-keys -t ${name} \"${cmd_str} > \$OUT_DIR/\${SEQ}.out 2> \$OUT_DIR/\${SEQ}.err; echo \\\$? > \$OUT_DIR/\${SEQ}.rc\" Enter
    " 2>/dev/null || true

    # Step 3: wait for completion (poll for rc file)
    local waited=0
    while [[ $waited -lt 120 ]]; do
        local rc_exists
        rc_exists=$(_session_ssh "$sid" "
            if [ -f /tmp/assh_sessions/${name}/${seq}.rc ]; then echo YES; else echo NO; fi
        " 2>/dev/null || echo "NO")
        rc_exists=$(echo "$rc_exists" | tr -d '[:space:]')
        [[ "$rc_exists" == "YES" ]] && break
        sleep 1; waited=$((waited + 1))
    done

    # Step 4: get results
    local result
    result=$(_session_ssh "$sid" "
        RC=\$(cat /tmp/assh_sessions/${name}/${seq}.rc 2>/dev/null || echo -1)
        STDOUT_LINES=\$(wc -l < /tmp/assh_sessions/${name}/${seq}.out 2>/dev/null || echo 0)
        STDERR_LINES=\$(wc -l < /tmp/assh_sessions/${name}/${seq}.err 2>/dev/null || echo 0)
        CWD=\$(tmux display-message -p -t ${name} '#{pane_current_path}' 2>/dev/null || echo '')
        echo \"RC:\$RC STDOUT:\$STDOUT_LINES STDERR:\$STDERR_LINES CWD:\$CWD\"
    " 2>/dev/null || echo "RC:-1")

    local rc stdout_lines stderr_lines cwd
    rc=$(echo "$result" | grep -oP 'RC:\K[-0-9]+' | head -1)
    stdout_lines=$(echo "$result" | grep -oP 'STDOUT:\K[0-9]+' | head -1)
    stderr_lines=$(echo "$result" | grep -oP 'STDERR:\K[0-9]+' | head -1)
    cwd=$(echo "$result" | grep -oP 'CWD:\K[^ ]+' | head -1)

    [[ -z "$rc" ]] && rc="-1"
    [[ -z "$stdout_lines" ]] && stdout_lines=0
    [[ -z "$stderr_lines" ]] && stderr_lines=0

    local ok="false"
    [[ "$rc" == "0" ]] && ok="true"

    audit_log "session_exec" "\"sid\":\"$sid\",\"seq\":$seq,\"rc\":$rc"

    printf '{"ok":%s,"rc":%s,"seq":%s,"stdout_lines":%s,"stderr_lines":%s,"cwd":"%s","session":"%s"}\n' \
        "$ok" "$rc" "$seq" "$stdout_lines" "$stderr_lines" "$cwd" "$name"
}

session_read() {
    local sid="" seq="" offset=0 limit=50 stream="stdout"

    while [[ $# -gt 0 ]]; do
        case "$1" in
            -s|--sid)       sid="$2"; shift 2 ;;
            --seq)          seq="$2"; shift 2 ;;
            --offset)       offset="$2"; shift 2 ;;
            --limit)        limit="$2"; shift 2 ;;
            --stream)       stream="$2"; shift 2 ;;
            *)              shift ;;
        esac
    done

    [[ -z "$sid" ]] && { echo '{"ok":false,"error":"--sid required"}'; return 1; }
    [[ -z "$seq" ]] && { echo '{"ok":false,"error":"--seq required"}'; return 1; }

    local session_file="$SESSIONS_DIR/${sid}.json"
    [[ ! -f "$session_file" ]] && { echo '{"ok":false,"error":"session not found"}'; return 1; }

    local name
    name=$(python3 -c "import json; print(json.load(open('$session_file'))['name'])" 2>/dev/null)

    local ext="out"
    [[ "$stream" == "stderr" ]] && ext="err"

    local result
    result=$(_session_ssh "$sid" "
        FILE=/tmp/assh_sessions/${name}/${seq}.${ext}
        if [ ! -f \$FILE ]; then echo 'NOT_FOUND'; exit 0; fi
        TOTAL_LINES=\$(wc -l < \$FILE)
        if [ \$TOTAL_LINES -le $limit ]; then
            cat \$FILE
        else
            tail -n +$((offset + 1)) \$FILE | head -n $limit
        fi
        echo ''
        echo \"TOTAL_LINES:\$TOTAL_LINES\"
    " 2>/dev/null)

    if echo "$result" | grep -q "NOT_FOUND"; then
        printf '{"ok":false,"error":"output seq %s not found on remote"}\n' "$seq"
        return 1
    fi

    local total_lines
    total_lines=$(echo "$result" | grep "TOTAL_LINES:" | sed 's/TOTAL_LINES://' | tr -d '[:space:]')
    result=$(echo "$result" | grep -v "TOTAL_LINES:")

    local content
    content=$(echo "$result" | python3 -c 'import sys,json; print(json.dumps(sys.stdin.read()))' 2>/dev/null || echo '""')

    local has_more="false"
    [[ -n "$total_lines" ]] && [[ $((offset + limit)) -lt ${total_lines:-0} ]] && has_more="true"

    printf '{"ok":true,"sid":"%s","seq":%s,"stream":"%s","offset":%d,"limit":%d,"total_lines":%s,"has_more":%s,"content":%s}\n' \
        "$sid" "$seq" "$stream" "$offset" "$limit" "${total_lines:-0}" "$has_more" "$content"
}

session_close() {
    local sid=""
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -s|--sid) sid="$2"; shift 2 ;;
            *) shift ;;
        esac
    done

    [[ -z "$sid" ]] && { echo '{"ok":false,"error":"--sid required"}'; return 1; }
    local session_file="$SESSIONS_DIR/${sid}.json"
    [[ ! -f "$session_file" ]] && { echo '{"ok":false,"error":"session not found"}'; return 1; }

    local name
    name=$(python3 -c "import json; print(json.load(open('$session_file'))['name'])" 2>/dev/null)

    _session_ssh "$sid" "tmux kill-session -t '${name}' 2>/dev/null; rm -rf /tmp/assh_sessions/'${name}'" 2>/dev/null || true
    rm -f "$session_file"

    audit_log "session_close" "\"sid\":\"$sid\",\"name\":\"$name\""
    printf '{"ok":true,"sid":"%s","session":"%s"}\n' "$sid" "$name"
}

session_list() {
    echo "["
    local first=true
    for f in "$SESSIONS_DIR"/*.json; do
        [[ ! -f "$f" ]] && continue
        [[ "$first" == true ]] && first=false || echo ","
        cat "$f"
    done
    echo "]"
}

session_help() {
    cat <<'SESSION_HELP'
Persistent sessions (tmux, state preserved between commands):

  assh session open   -H host [-u user] [-i key] [-n name]
     -> {"ok":true,"session":"name","sid":"xxxx"}

  assh session exec   -s SID -- "command"
     -> {"ok":true,"rc":0,"seq":2,"stdout_lines":15,"cwd":"/path"}

  assh session read   -s SID --seq N [--limit 10] [--stream stdout|stderr]
     -> {"ok":true,"content":"...","total_lines":15}

  assh session close  -s SID
  assh session list

Requires tmux on the remote host.
SESSION_HELP
}

# ═══ scan — сбор информации о сервере ══════════════════════════════

cmd_scan() {
    local host="" user="root" port="22" identity="" env_var=""
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -H|--host)      host="$2"; shift 2 ;;
            -u|--user)      user="$2"; shift 2 ;;
            -p|--port)      port="$2"; shift 2 ;;
            -i|--identity)  identity="$2"; shift 2 ;;
            -E|--password-env) env_var="$2"; shift 2 ;;
            *)              shift ;;
        esac
    done

    [[ -z "$host" ]] && { echo '{"ok":false,"error":"--host required"}'; return 1; }

    local ssh_args
    ssh_args=$(ssh_opts "$host" "$user" "$port" "$identity" "$TIMEOUT")
    local target="${user}@${host}"

    local use_password=false password=""
    if [[ -n "$env_var" ]]; then
        password="${!env_var:-}"; use_password=true
    fi

    local scan_cmd='printf "{\"hostname\":\"%s\",\"os\":\"%s\",\"kernel\":\"%s\",\"arch\":\"%s\",\"cpu_cores\":\"%s\",\"mem_total_mb\":\"%s\",\"mem_used_mb\":\"%s\",\"disk_root_pct\":\"%s\",\"docker\":\"%s\",\"ip\":\"%s\"}" "$(hostname)" "$(cat /etc/os-release 2>/dev/null | grep "^PRETTY_NAME=" | cut -d= -f2 | tr -d \\" 2>/dev/null || uname -s)" "$(uname -r)" "$(uname -m)" "$(nproc 2>/dev/null || echo N/A)" "$(free -m 2>/dev/null | grep "^Mem:" | awk "{print \\$2}" || echo N/A)" "$(free -m 2>/dev/null | grep "^Mem:" | awk "{print \\$3}" || echo N/A)" "$(df / 2>/dev/null | tail -1 | awk "{print \\$5}" | tr -d % || echo N/A)" "$(docker --version 2>/dev/null | cut -d, -f1 | awk "{print \\$3}" | tr -d , || echo no)" "$(hostname -I 2>/dev/null | cut -d\" \" -f1 || echo N/A)"'

    local rc=0 result
    if [[ "$use_password" == true ]]; then
        local askpass_script
        askpass_script=$(mktemp /tmp/askpass_XXXXXX.sh)
        printf '#!/bin/sh\necho %s\n' "$password" > "$askpass_script"
        chmod 700 "$askpass_script"
        result=$(SSH_ASKPASS="$askpass_script" SSH_ASKPASS_REQUIRE=force DISPLAY="${DISPLAY:-:0}" \
            timeout 30 ssh $ssh_args "$target" "$scan_cmd" 2>/dev/null) || rc=$?
        rm -f "$askpass_script"
    else
        result=$(timeout 30 ssh $ssh_args "$target" "$scan_cmd" 2>/dev/null) || rc=$?
    fi

    echo "$result"
    audit_log "scan" "\"host\":\"$host\",\"user\":\"$user\""
}

# ═══ key-deploy ═══════════════════════════════════════════════════

cmd_key_deploy() {
    local host="" user="root" port="22" identity="$HOME/.ssh/id_agent_ed25519" env_var=""
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -H|--host)      host="$2"; shift 2 ;;
            -u|--user)      user="$2"; shift 2 ;;
            -p|--port)      port="$2"; shift 2 ;;
            -E|--password-env) env_var="$2"; shift 2 ;;
            -i|--identity)  identity="$2"; shift 2 ;;
            *)              shift ;;
        esac
    done

    [[ -z "$host" ]] && { echo '{"ok":false,"error":"--host required"}'; return 1; }

    [[ ! -f "$identity" ]] && { ssh-keygen -t ed25519 -f "$identity" -N "" 2>/dev/null || true; }
    [[ -z "$env_var" ]] && { echo '{"ok":false,"error":"--password-env required for key-deploy"}'; return 1; }

    local password="${!env_var:-}"
    [[ -z "$password" ]] && { echo '{"ok":false,"error":"env var empty"}'; return 1; }

    local pub_key
    pub_key=$(cat "${identity}.pub")
    local ssh_args
    ssh_args=$(ssh_opts "$host" "$user" "$port" "" "$TIMEOUT")
    local target="${user}@${host}"

    local askpass_script
    askpass_script=$(mktemp /tmp/askpass_XXXXXX.sh)
    printf '#!/bin/sh\necho %s\n' "$password" > "$askpass_script"
    chmod 700 "$askpass_script"

    SSH_ASKPASS="$askpass_script" SSH_ASKPASS_REQUIRE=force DISPLAY="${DISPLAY:-:0}" \
        ssh $ssh_args "$target" "mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo '$pub_key' >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && echo KEY_DEPLOYED_OK" 2>&1
    local rc=$?
    rm -f "$askpass_script"

    if [[ $rc -eq 0 ]]; then
        audit_log "key_deploy" "\"host\":\"$host\",\"ok\":true"
        printf '{"ok":true,"message":"Key deployed. Use: assh exec -H %s -u %s -i %s -- <command>"}\n' "$host" "$user" "$identity"
    else
        audit_log "key_deploy" "\"host\":\"$host\",\"ok\":false"
        echo '{"ok":false,"error":"key deploy failed"}'; return 1
    fi
}

# ═══ audit ════════════════════════════════════════════════════════

cmd_audit() {
    local failed=false host="" action="" last=20

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --failed)    failed=true; shift ;;
            --host)      host="$2"; shift 2 ;;
            --action)    action="$2"; shift 2 ;;
            --last)      last="$2"; shift 2 ;;
            *)           shift ;;
        esac
    done

    [[ ! -f "$AUDIT_LOG" ]] && { echo "[]"; return 0; }

    local cmd="tail -n $last '$AUDIT_LOG'"
    [[ "$failed" == true ]] && cmd="grep '\"ok\":false' '$AUDIT_LOG' | tail -n $last"
    [[ -n "$host" ]] && cmd="grep '\"host\":\"$host\"' '$AUDIT_LOG' | tail -n $last"
    [[ -n "$action" ]] && cmd="grep '\"action\":\"$action\"' '$AUDIT_LOG' | tail -n $last"

    eval "$cmd"
}

# ═══ connections ═══════════════════════════════════════════════════

cmd_connections() {
    echo "["
    local first=true
    for ctrl in "$SSH_CONTROL_DIR"/ctrl-*; do
        [[ ! -e "$ctrl" ]] && continue
        local alive="false"
        ssh -o "ControlPath=$ctrl" -O check "x" 2>/dev/null && alive="true" || true
        [[ "$first" == true ]] && first=false || echo ","
        printf '  {"path":"%s","alive":%s}' "$ctrl" "$alive"
    done
    echo "]"
}

# ═══ help ═════════════════════════════════════════════════════════

cmd_help() {
    cat <<'HELP'
assh — SSH for LLM agents

Two key features:
  1. TOKEN ECONOMY: exec returns metadata, read returns paginated content
  2. PERSISTENT SESSIONS: cwd/env preserved across commands (tmux)

Commands:
  exec          Execute command (metadata-only output)
  read          Read output with pagination
  scan          Collect server info (OS, CPU, RAM, disk)
  session open  Open persistent tmux session
  session exec  Execute command in session (cwd preserved!)
  session read  Read session output with pagination
  session close Close session
  session list  List open sessions
  key-deploy    Generate and deploy SSH key
  audit         Audit log with filters
  connections   Active SSH connections

=== TOKEN ECONOMY (why it matters) ===

  # Regular SSH dumps 4327 lines (1 MB) into agent context:
  ssh host "journalctl -p warning"

  # assh returns ONLY metadata — agent decides what to read:
  assh exec -H host -u root -i key -- "journalctl -p warning"
    -> {"ok":true, "output_id":"a1b2c3", "stdout_lines":4327}

  # Read only what you need:
  assh read --id a1b2c3 --limit 20 --offset 4307
  assh read --id a1b2c3 --stream stderr --limit 10
  assh read --id a1b2c3 --raw  (for piping)

=== PERSISTENT SESSIONS ===

  assh session open -H host -u root -i key -n deploy
    -> {"ok":true, "session":"deploy", "sid":"a1b2c3"}

  assh session exec -s a1b2c3 -- "cd /var/log"
    -> {"ok":true, "rc":0, "seq":1, "cwd":"/var/log"}

  assh session exec -s a1b2c3 -- "ls *.log | wc -l"
    -> {"ok":true, "rc":0, "seq":2, "cwd":"/var/log"}  # STILL in /var/log!

  assh session read -s a1b2c3 --seq 2 --limit 10
  assh session close -s a1b2c3

=== KEY DEPLOY (one time, then password-free) ===

  export MY_PASS="secret"
  assh key-deploy -H host -u root -E MY_PASS && unset MY_PASS
  assh exec -H host -u root -i ~/.ssh/id_agent_ed25519 -- "hostname"

Env vars:
  ASSH_RETRIES   Number of retries (default: 3)
  ASSH_TIMEOUT   Connection timeout in seconds (default: 10)
HELP
}

# ═══ Entry point ══════════════════════════════════════════════════

case "${1:-help}" in
    exec)        shift; cmd_exec "$@" ;;
    read)        shift; cmd_read "$@" ;;
    scan)        shift; cmd_scan "$@" ;;
    session)     shift; cmd_session "$@" ;;
    key-deploy)  shift; cmd_key_deploy "$@" ;;
    audit)       shift; cmd_audit "$@" ;;
    connections) shift; cmd_connections "$@" ;;
    help|--help|-h) cmd_help ;;
    *)           echo "Unknown command: $1. Run assh help."; exit 1 ;;
esac