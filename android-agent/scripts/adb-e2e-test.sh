#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ADB="${ADB:-adb}"
APK="${APK:-$ROOT/android-agent/app/build/outputs/apk/debug/app-debug.apk}"
SERVER_BIN="${SERVER_BIN:-$ROOT/dist/vohive}"
REPORT="${REPORT:-$ROOT/android-agent/build/zte-e2e-test.txt}"
PACKAGE="com.vohive.agent"
DEVICE_ID="zte-e2e"
PORT="${VOHIVE_E2E_PORT:-$((18000 + RANDOM % 1000))}"
PROXY_PORT="$((PORT + 1))"
SMS_TO="${VOHIVE_E2E_SMS_TO:-}"
ESIM_SWITCH="${VOHIVE_E2E_ESIM_SWITCH:-false}"
AGENT_HTTP_PORT="${VOHIVE_AGENT_HTTP_PORT:-8765}"
AGENT_WEB_USERNAME="admin"
AGENT_WEB_PASSWORD="vohive-agent-e2e"
TARGET_SERIAL="${SERIAL:-}"

mkdir -p "$(dirname "$REPORT")"
exec > >(tee "$REPORT") 2>&1

ADB="$ADB" APK="$APK" SERIAL="$TARGET_SERIAL" REPORT="${REPORT%.txt}-local.txt" \
  "$ROOT/android-agent/scripts/adb-smoke-test.sh"

if [[ -n "$TARGET_SERIAL" ]]; then
  SERIAL="$TARGET_SERIAL"
else
  mapfile -t DEVICES < <("$ADB" devices | awk 'NR > 1 && $2 == "device" {print $1}')
  if (( ${#DEVICES[@]} != 1 )); then
    "$ADB" devices -l
    echo "Expected exactly one authorized Android device; found ${#DEVICES[@]}." >&2
    exit 2
  fi
  SERIAL="${DEVICES[0]}"
fi
ORIGINAL_PREFS="$(mktemp)"
"$ADB" -s "$SERIAL" shell run-as "$PACKAGE" cat \
  shared_prefs/vohive-agent.xml >"$ORIGINAL_PREFS"
agent_id="$(sed -n 's/.*<string name="agent_id">\([^<]*\)<\/string>.*/\1/p' "$ORIGINAL_PREFS" | head -1)"
if [[ -z "$agent_id" ]]; then
  echo "Agent ID is missing from SharedPreferences." >&2
  exit 5
fi

phone_ip="$("$ADB" -s "$SERIAL" shell ip -o -4 addr show scope global |
  awk '$2 ~ /^wlan/ {print $4; exit}' | cut -d/ -f1 | tr -d '\r')"
if [[ -z "$phone_ip" ]]; then
  echo "The Android device has no LAN IPv4 address." >&2
  exit 6
fi
host_ip="$(ip route get "$phone_ip" | awk '{for (i=1;i<=NF;i++) if ($i=="src") {print $(i+1); exit}}')"
if [[ -z "$host_ip" ]]; then
  echo "No host source address is available for $phone_ip." >&2
  exit 7
fi

RUN_DIR="$(mktemp -d)"
SERVER_PID=""
MOBILE_DATA_KEY=""
ORIGINAL_MOBILE_DATA=""
restore() {
  "$ADB" -s "$SERIAL" shell am stopservice -n "$PACKAGE/.AgentService" >/dev/null 2>&1 || true
  "$ADB" -s "$SERIAL" shell am force-stop "$PACKAGE" >/dev/null 2>&1 || true
  if [[ -s "$ORIGINAL_PREFS" ]]; then
    "$ADB" -s "$SERIAL" shell \
      "run-as '$PACKAGE' sh -c 'mkdir -p shared_prefs && cat > shared_prefs/vohive-agent.xml'" \
      <"$ORIGINAL_PREFS" || true
    "$ADB" -s "$SERIAL" shell am start -W \
      -n "$PACKAGE/.HeadlessBootstrapActivity" >/dev/null 2>&1 || true
  fi
  if [[ -n "$SERVER_PID" ]]; then
    kill -TERM "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "$MOBILE_DATA_KEY" ]]; then
    if [[ "$ORIGINAL_MOBILE_DATA" == "null" || -z "$ORIGINAL_MOBILE_DATA" ]]; then
      "$ADB" -s "$SERIAL" shell settings delete global "$MOBILE_DATA_KEY" >/dev/null 2>&1 || true
    else
      "$ADB" -s "$SERIAL" shell settings put global "$MOBILE_DATA_KEY" \
        "$ORIGINAL_MOBILE_DATA" >/dev/null 2>&1 || true
    fi
  fi
  rm -rf "$RUN_DIR"
}
trap restore EXIT

cp "$SERVER_BIN" "$RUN_DIR/vohive"
cat >"$RUN_DIR/config.yaml" <<YAML
server:
  port: ":$PORT"
  debug: true
web:
  username: admin
  password: zte-e2e-secret
devices:
  - id: $DEVICE_ID
    name: ZTE E2E
    device_kind: android
    device_backend: android
    android:
      agent_id: $agent_id
YAML

(
  cd "$RUN_DIR"
  exec ./vohive -backend-only -c "$RUN_DIR/config.yaml"
) >"$RUN_DIR/server.log" 2>&1 &
SERVER_PID=$!
base_url="http://127.0.0.1:$PORT"
for _ in $(seq 1 80); do
  if curl -fsS "$base_url/ping" >/dev/null 2>&1; then break; fi
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    cat "$RUN_DIR/server.log"
    exit 8
  fi
  sleep 0.25
done

session_token="$(curl -fsS -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"zte-e2e-secret"}' \
  "$base_url/api/auth/login" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')"
pair_token="$(curl -fsS -X POST -H "Authorization: Bearer $session_token" \
  "$base_url/api/devices/$DEVICE_ID/android-agent/pairing-token" | \
  python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')"

"$ADB" -s "$SERIAL" shell am force-stop "$PACKAGE"
provision_output="$("$ADB" -s "$SERIAL" shell am broadcast --receiver-foreground \
  -a com.vohive.agent.PROVISION \
  -n "$PACKAGE/.HeadlessCommandReceiver" \
  --es server_url "http://$host_ip:$PORT" \
  --es device_id "$DEVICE_ID" \
  --es agent_id "$agent_id" \
  --es pair_token "$pair_token" \
  --es web_username "$AGENT_WEB_USERNAME" \
  --es web_password "$AGENT_WEB_PASSWORD" \
  --ei http_port "$AGENT_HTTP_PORT" \
  --ez auto_start true \
  --ez agent_enabled true 2>&1)"
printf '%s\n' "$provision_output"
if grep -Eq 'SecurityException|Permission Denial' <<<"$provision_output"; then
  echo "Headless provisioning broadcast was rejected." >&2
  exit 17
fi
service_output="$("$ADB" -s "$SERIAL" shell am start -W \
  -n "$PACKAGE/.HeadlessBootstrapActivity" 2>&1)"
printf '%s\n' "$service_output"
if grep -Eq 'SecurityException|Permission Denial|Error type|Error:' <<<"$service_output"; then
  echo "Headless bootstrap start was rejected." >&2
  exit 19
fi

echo "== authenticated headless web console =="
agent_base_url="http://$phone_ip:$AGENT_HTTP_PORT"
for _ in $(seq 1 60); do
  if curl -fsS --connect-timeout 1 "$agent_base_url/" >/dev/null 2>&1; then break; fi
  sleep 0.25
done
agent_unauth="$(curl -sS -o /dev/null -w '%{http_code}' "$agent_base_url/api/status")"
[[ "$agent_unauth" == "401" ]] || { echo "Agent API did not enforce authentication." >&2; exit 18; }
agent_cookie="$RUN_DIR/agent-cookie.txt"
agent_login="$(curl -fsS -c "$agent_cookie" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$AGENT_WEB_USERNAME\",\"password\":\"$AGENT_WEB_PASSWORD\"}" \
  "$agent_base_url/api/auth/login")"
agent_csrf="$(AGENT_LOGIN="$agent_login" python3 -c 'import json,os; print(json.loads(os.environ["AGENT_LOGIN"])["csrf_token"])')"
agent_status="$(curl -fsS -b "$agent_cookie" "$agent_base_url/api/status")"
AGENT_STATUS="$agent_status" python3 - <<'PY'
import json, os
p = json.loads(os.environ['AGENT_STATUS'])
assert p['service']['running']
assert p['web']['bind'] == '0.0.0.0'
assert p['upstream']['enabled']
print('agent_web_auth=PASS')
PY
echo "agent_csrf_token_length=${#agent_csrf}"

status_json=""
for _ in $(seq 1 120); do
  status_json="$(curl -fsS -H "Authorization: Bearer $session_token" \
    "$base_url/api/devices/$DEVICE_ID/android-agent/status")"
  if STATUS_JSON="$status_json" python3 - <<'PY'
import json, os, sys
p = json.loads(os.environ['STATUS_JSON'])
s = p.get('snapshot') or {}
sys.exit(0 if p.get('online') and s.get('firmware') and 'battery_pct' in s else 1)
PY
  then break; fi
  sleep 0.5
done

echo "== VoHive status snapshot =="
STATUS_JSON="$status_json" python3 - <<'PY'
import json, os
p = json.loads(os.environ['STATUS_JSON'])
s = p.get('snapshot') or {}
keys = [
  'imei','imsi','iccid','msisdn','signal_dbm','signal_rsrp','signal_rsrq',
  'signal_sinr','battery_pct','firmware','baseband','reg_status_text',
  'network_mode','selected_subscription_id','esim_supported','esim_enabled','eid'
]
print(json.dumps({k:s.get(k) for k in keys}, ensure_ascii=False, indent=2))
print('registration_details=', json.dumps(s.get('registration_details', []), ensure_ascii=False))
print('subscriptions=', json.dumps(s.get('subscriptions', []), ensure_ascii=False))
print('access=', json.dumps(s.get('access', {}), ensure_ascii=False))
if not p.get('online') or not s.get('firmware') or 'battery_pct' not in s:
    raise SystemExit('Agent status did not become ready')
PY

echo "== subscription RPC =="
subscriptions_json="$(curl -fsS -H "Authorization: Bearer $session_token" \
  "$base_url/api/devices/$DEVICE_ID/android-agent/subscriptions")"
printf '%s' "$subscriptions_json"
echo
selected_subscription="$(SUBSCRIPTIONS_JSON="$subscriptions_json" python3 - <<'PY'
import json, os
items = json.loads(os.environ['SUBSCRIPTIONS_JSON']).get('subscriptions') or []
selected = next((x for x in items if x.get('active') and x.get('selected')), None)
selected = selected or next((x for x in items if x.get('active')), None)
print('' if selected is None else selected.get('subscription_id', ''))
PY
)"
if [[ -n "$selected_subscription" ]]; then
  MOBILE_DATA_KEY="mobile_data$selected_subscription"
  ORIGINAL_MOBILE_DATA="$("$ADB" -s "$SERIAL" shell settings get global \
    "$MOBILE_DATA_KEY" | tr -d '\r')"
  "$ADB" -s "$SERIAL" shell settings put global "$MOBILE_DATA_KEY" 1
  echo "== non-destructive active subscription re-selection =="
  curl -fsS -X POST -H "Authorization: Bearer $session_token" \
    -H 'Content-Type: application/json' \
    -d "{\"subscription_id\":$selected_subscription}" \
    "$base_url/api/devices/$DEVICE_ID/android-agent/subscriptions/select"
  echo
  for _ in $(seq 1 60); do
    if curl -fsS -H "Authorization: Bearer $session_token" \
      "$base_url/api/devices/$DEVICE_ID/android-agent/status" | \
      python3 -c 'import json,sys; raise SystemExit(0 if json.load(sys.stdin).get("snapshot", {}).get("data_connected") else 1)'
    then break; fi
    sleep 0.5
  done
fi

alternate_subscription="$(SUBSCRIPTIONS_JSON="$subscriptions_json" SELECTED_SUBSCRIPTION="$selected_subscription" python3 - <<'PY'
import json, os
items = json.loads(os.environ['SUBSCRIPTIONS_JSON']).get('subscriptions') or []
selected = int(os.environ['SELECTED_SUBSCRIPTION']) if os.environ['SELECTED_SUBSCRIPTION'] else -1
alternates = [x for x in items if x.get('active') and x.get('subscription_id') != selected]
alternates.sort(key=lambda x: (not x.get('embedded'), x.get('subscription_id', 0)))
print('' if not alternates else alternates[0].get('subscription_id', ''))
PY
)"
if [[ -n "$selected_subscription" && -n "$alternate_subscription" ]]; then
  echo "== alternate SIM/eSIM selection round-trip =="
  curl -fsS -X POST -H "Authorization: Bearer $session_token" \
    -H 'Content-Type: application/json' \
    -d "{\"subscription_id\":$alternate_subscription}" \
    "$base_url/api/devices/$DEVICE_ID/android-agent/subscriptions/select" >/dev/null
  alternate_ready=false
  for _ in $(seq 1 60); do
    if curl -fsS -H "Authorization: Bearer $session_token" \
      "$base_url/api/devices/$DEVICE_ID/android-agent/status" | \
      ALT_SUBSCRIPTION="$alternate_subscription" python3 -c 'import json,os,sys; s=json.load(sys.stdin).get("snapshot", {}); target=int(os.environ["ALT_SUBSCRIPTION"]); items=s.get("subscriptions") or []; raise SystemExit(0 if s.get("selected_subscription_id")==target and any(x.get("subscription_id")==target and x.get("selected") for x in items) else 1)'
    then alternate_ready=true; break; fi
    sleep 0.5
  done
  if [[ "$alternate_ready" != "true" ]]; then
    echo "alternate subscription selection did not converge" >&2
    exit 13
  fi
  echo "alternate_subscription_selected=$alternate_subscription"

  curl -fsS -X POST -H "Authorization: Bearer $session_token" \
    -H 'Content-Type: application/json' \
    -d "{\"subscription_id\":$selected_subscription}" \
    "$base_url/api/devices/$DEVICE_ID/android-agent/subscriptions/select" >/dev/null
  original_ready=false
  for _ in $(seq 1 60); do
    if curl -fsS -H "Authorization: Bearer $session_token" \
      "$base_url/api/devices/$DEVICE_ID/android-agent/status" | \
      ORIGINAL_SUBSCRIPTION="$selected_subscription" python3 -c 'import json,os,sys; s=json.load(sys.stdin).get("snapshot", {}); raise SystemExit(0 if s.get("selected_subscription_id")==int(os.environ["ORIGINAL_SUBSCRIPTION"]) else 1)'
    then original_ready=true; break; fi
    sleep 0.5
  done
  if [[ "$original_ready" != "true" ]]; then
    echo "original subscription selection did not restore" >&2
    exit 14
  fi
  echo "original_subscription_restored=$selected_subscription"
fi

embedded_subscription="$(SUBSCRIPTIONS_JSON="$subscriptions_json" python3 - <<'PY'
import json, os
items = json.loads(os.environ['SUBSCRIPTIONS_JSON']).get('subscriptions') or []
embedded = next((x for x in items if x.get('active') and x.get('embedded')), None)
print('' if embedded is None else embedded.get('subscription_id', ''))
PY
)"
if [[ "$ESIM_SWITCH" == "true" && -n "$embedded_subscription" ]]; then
  echo "== already-active eSIM profile switch RPC =="
  switch_json="$(curl -fsS -X POST -H "Authorization: Bearer $session_token" \
    -H 'Content-Type: application/json' \
    -d "{\"subscription_id\":$embedded_subscription,\"port_index\":0}" \
    "$base_url/api/devices/$DEVICE_ID/android-agent/esim/switch")"
  printf '%s\n' "$switch_json"
  esim_state=""
  for _ in $(seq 1 120); do
    esim_state="$(curl -fsS -H "Authorization: Bearer $session_token" \
      "$base_url/api/devices/$DEVICE_ID/android-agent/status" | \
      python3 -c 'import json,sys; print(((json.load(sys.stdin).get("snapshot") or {}).get("esim_operation") or {}).get("state", ""))')"
    if [[ "$esim_state" == "completed" || "$esim_state" == "failed" || "$esim_state" == "user_resolution_required" || "$esim_state" == "resolution_failed" ]]; then
      break
    fi
    sleep 0.5
  done
  echo "esim_switch_state=$esim_state"
  if [[ "$esim_state" != "completed" ]]; then
    echo "already-active eSIM switch did not complete" >&2
    exit 15
  fi
  # Keep subsequent tests on the original physical subscription while the
  # script-level preference restoration remains an additional rollback.
  curl -fsS -X POST -H "Authorization: Bearer $session_token" \
    -H 'Content-Type: application/json' \
    -d "{\"subscription_id\":$selected_subscription}" \
    "$base_url/api/devices/$DEVICE_ID/android-agent/subscriptions/select" >/dev/null
  data_restored=false
  for _ in $(seq 1 120); do
    if curl -fsS -H "Authorization: Bearer $session_token" \
      "$base_url/api/devices/$DEVICE_ID/android-agent/status" | \
      ORIGINAL_SUBSCRIPTION="$selected_subscription" python3 -c 'import json,os,sys; s=json.load(sys.stdin).get("snapshot", {}); raise SystemExit(0 if s.get("selected_subscription_id")==int(os.environ["ORIGINAL_SUBSCRIPTION"]) and s.get("data_connected") else 1)'
    then data_restored=true; break; fi
    sleep 0.5
  done
  if [[ "$data_restored" != "true" ]]; then
    echo "default-data subscription did not reconnect after eSIM switch" >&2
    exit 16
  fi
fi

echo "== non-destructive SMS query RPC =="
sms_json="$(curl -fsS -H "Authorization: Bearer $session_token" \
  "$base_url/api/devices/$DEVICE_ID/android-agent/sms?limit=5")"
SMS_JSON="$sms_json" python3 - <<'PY'
import json, os
items = json.loads(os.environ['SMS_JSON']).get('messages') or []
print(json.dumps({
    'status': 'ok',
    'count': len(items),
    'messages': [
        {
            'index': x.get('index'),
            'type': x.get('type'),
            'tag': x.get('tag'),
            'subscription_id': x.get('subscription_id'),
            'content_bytes': len((x.get('content') or '').encode()),
        }
        for x in items
    ],
}, ensure_ascii=False))
PY
first_sms_id="$(SMS_JSON="$sms_json" python3 - <<'PY'
import json, os
items = json.loads(os.environ['SMS_JSON']).get('messages') or []
print('' if not items else items[0].get('index', ''))
PY
)"
if [[ -n "$first_sms_id" ]]; then
  echo "== non-destructive single SMS read RPC =="
  single_sms_json="$(curl -fsS -H "Authorization: Bearer $session_token" \
    "$base_url/api/devices/$DEVICE_ID/android-agent/sms/$first_sms_id")"
  SINGLE_SMS_JSON="$single_sms_json" python3 - <<'PY'
import json, os
m = json.loads(os.environ['SINGLE_SMS_JSON']).get('message') or {}
print(json.dumps({
    'status': 'ok',
    'message': {
        'index': m.get('index'),
        'type': m.get('type'),
        'tag': m.get('tag'),
        'subscription_id': m.get('subscription_id'),
        'content_bytes': len((m.get('content') or '').encode()),
    },
}, ensure_ascii=False))
PY
fi

if [[ -n "$SMS_TO" && -n "$selected_subscription" ]]; then
  echo "== destructive SMS send/receive/delete RPC =="
  sms_body="VOHIVE-E2E-$(date +%s)-$RANDOM"
  send_json="$(curl -fsS -X POST -H "Authorization: Bearer $session_token" \
    -H 'Content-Type: application/json' \
    -d "{\"device_id\":\"$DEVICE_ID\",\"phone\":\"$SMS_TO\",\"message\":\"$sms_body\",\"subscription_id\":$selected_subscription}" \
    "$base_url/api/sms/send")"
  message_id="$(SEND_JSON="$send_json" python3 -c 'import json,os; print(json.loads(os.environ["SEND_JSON"]).get("message_id", ""))')"
  if [[ -z "$message_id" ]]; then
    echo "SMS send did not return message_id" >&2
    exit 10
  fi
  echo "sms_send_message_id=$message_id"

  matched_json="[]"
  for _ in $(seq 1 180); do
    current_sms="$(curl -fsS -H "Authorization: Bearer $session_token" \
      "$base_url/api/devices/$DEVICE_ID/android-agent/sms?limit=80")"
    matched_json="$(SMS_JSON="$current_sms" SMS_BODY="$sms_body" python3 - <<'PY'
import json, os
items = json.loads(os.environ['SMS_JSON']).get('messages') or []
matches = [
    {
        'index': x.get('index'),
        'type': x.get('type'),
        'subscription_id': x.get('subscription_id'),
    }
    for x in items if x.get('content') == os.environ['SMS_BODY']
]
print(json.dumps(matches, separators=(',', ':')))
PY
)"
    if MATCHED_JSON="$matched_json" python3 - <<'PY'
import json, os
items = json.loads(os.environ['MATCHED_JSON'])
types = {x.get('type') for x in items}
raise SystemExit(0 if 1 in types and 2 in types else 1)
PY
    then break; fi
    sleep 1
  done
  echo "sms_provider_matches=$matched_json"
  if ! MATCHED_JSON="$matched_json" python3 - <<'PY'
import json, os
types = {x.get('type') for x in json.loads(os.environ['MATCHED_JSON'])}
raise SystemExit(0 if 1 in types and 2 in types else 1)
PY
  then
    echo "SMS self-test did not observe both sent and received provider rows" >&2
    exit 11
  fi

  mapfile -t test_sms_ids < <(MATCHED_JSON="$matched_json" python3 - <<'PY'
import json, os
for item in json.loads(os.environ['MATCHED_JSON']):
    print(item['index'])
PY
  )
  for sms_id in "${test_sms_ids[@]}"; do
    curl -fsS -X DELETE -H "Authorization: Bearer $session_token" \
      "$base_url/api/devices/$DEVICE_ID/android-agent/sms/$sms_id" >/dev/null
  done
  after_delete="$(curl -fsS -H "Authorization: Bearer $session_token" \
    "$base_url/api/devices/$DEVICE_ID/android-agent/sms?limit=80")"
  if SMS_JSON="$after_delete" SMS_BODY="$sms_body" python3 - <<'PY'
import json, os
items = json.loads(os.environ['SMS_JSON']).get('messages') or []
raise SystemExit(0 if any(x.get('content') == os.environ['SMS_BODY'] for x in items) else 1)
PY
  then
    echo "SMS test rows remain after deletion" >&2
    exit 12
  fi
  echo "sms_delete_verified=${#test_sms_ids[@]}"
fi

echo "== Android cellular HTTP proxy =="
cellular_ready=false
for _ in $(seq 1 90); do
  if curl -fsS -H "Authorization: Bearer $session_token" \
    "$base_url/api/devices/$DEVICE_ID/android-agent/status" | \
    python3 -c 'import json,sys; raise SystemExit(0 if json.load(sys.stdin).get("snapshot", {}).get("data_connected") else 1)'
  then cellular_ready=true; break; fi
  sleep 0.5
done
if [[ "$cellular_ready" != "true" ]]; then
  echo "selected cellular network did not become available" >&2
  exit 20
fi
proxy_config="$(cat <<JSON
{"instances":[{"id":"zte-http-e2e","name":"ZTE HTTP E2E","device_id":"$DEVICE_ID","enabled":true,"mode":"http","listen_addr":"127.0.0.1","listen_port":$PROXY_PORT,"auth_enabled":false}]}
JSON
)"
curl -fsS -X PUT -H "Authorization: Bearer $session_token" \
  -H 'Content-Type: application/json' -d "$proxy_config" \
  "$base_url/api/proxy-instances/config"
echo
proxy_status="000"
proxy_body=""
for _ in $(seq 1 12); do
  proxy_result="$(curl -sS --max-time 15 --proxy "http://127.0.0.1:$PROXY_PORT" \
    -w $'\n%{http_code}' http://www.baidu.com/ || true)"
  proxy_status="${proxy_result##*$'\n'}"
  proxy_body="${proxy_result%$'\n'*}"
  if [[ "$proxy_status" == "200" ]]; then break; fi
  sleep 1
done
if [[ "$proxy_status" != "200" ]]; then
  echo "cellular proxy returned HTTP $proxy_status: $proxy_body" >&2
  echo "== server tail ==" >&2
  tail -n 80 "$RUN_DIR/server.log" >&2
  exit 9
fi
echo "cellular_proxy_status=$proxy_status"
echo "cellular_proxy_response_bytes=${#proxy_body}"

echo "== server tail =="
tail -n 30 "$RUN_DIR/server.log"
echo "result=PASS"
