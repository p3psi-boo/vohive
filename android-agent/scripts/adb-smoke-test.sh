#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APK="${APK:-$ROOT/app/build/outputs/apk/debug/app-debug.apk}"
ADB="${ADB:-adb}"
REPORT="${REPORT:-$ROOT/build/zte-smoke-test.txt}"
PACKAGE="com.vohive.agent"
HTTP_PORT="${HTTP_PORT:-8765}"
WEB_USERNAME="${WEB_USERNAME:-admin}"
WEB_PASSWORD="${WEB_PASSWORD:-vohive-smoke-8765}"
INSTALL_APK="${INSTALL_APK:-true}"
SERIAL="${SERIAL:-}"

mkdir -p "$(dirname "$REPORT")"
exec > >(tee "$REPORT") 2>&1

if [[ -z "$SERIAL" ]]; then
  mapfile -t DEVICES < <("$ADB" devices | awk 'NR > 1 && $2 == "device" {print $1}')
  if (( ${#DEVICES[@]} != 1 )); then
    "$ADB" devices -l
    echo "Expected exactly one authorized Android device; found ${#DEVICES[@]}." >&2
    exit 2
  fi
  SERIAL="${DEVICES[0]}"
fi
COOKIE_JAR="$(mktemp)"
trap 'rm -f "$COOKIE_JAR"' EXIT

echo "serial=$SERIAL"
echo "apk=$APK"
sha256sum "$APK"
"$ADB" -s "$SERIAL" shell getprop ro.product.manufacturer
"$ADB" -s "$SERIAL" shell getprop ro.product.model
"$ADB" -s "$SERIAL" shell getprop ro.build.version.release
"$ADB" -s "$SERIAL" shell getprop ro.build.version.sdk

if [[ "$INSTALL_APK" == "true" ]]; then "$ADB" -s "$SERIAL" install --no-streaming -r "$APK"; fi
"$ADB" -s "$SERIAL" shell am force-stop "$PACKAGE"
"$ADB" -s "$SERIAL" logcat -c
ADB="$ADB" APK="$APK" INSTALL_APK=false WEB_USERNAME="$WEB_USERNAME" \
  WEB_PASSWORD="$WEB_PASSWORD" HTTP_PORT="$HTTP_PORT" \
  "$ROOT/scripts/provision-headless.sh" "$SERIAL"

phone_ip="$("$ADB" -s "$SERIAL" shell ip -o -4 addr show scope global |
  awk '$2 ~ /^(wlan|eth)/ {print $4; exit}' | cut -d/ -f1 | tr -d '\r')"
if [[ -z "$phone_ip" ]]; then
  echo "The Android device has no LAN IPv4 address." >&2
  exit 3
fi
base_url="http://$phone_ip:$HTTP_PORT"
for _ in $(seq 1 60); do
  if curl -fsS --connect-timeout 1 -o /dev/null "$base_url/"; then break; fi
  sleep 0.25
done

echo
echo "== web binding and authentication =="
"$ADB" -s "$SERIAL" shell ss -ltn 2>/dev/null | grep -E "(^|:)${HTTP_PORT}[[:space:]]" || true
root_title="$(curl -fsS "$base_url/" | grep -o '<title>[^<]*' | head -1)"
echo "root=$root_title"
unauth_code="$(curl -sS -o /dev/null -w '%{http_code}' "$base_url/api/status")"
echo "unauthenticated_status=$unauth_code"
[[ "$unauth_code" == "401" ]] || { echo "Unauthenticated API was not rejected." >&2; exit 4; }
login_json="$(curl -fsS -c "$COOKIE_JAR" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$WEB_USERNAME\",\"password\":\"$WEB_PASSWORD\"}" \
  "$base_url/api/auth/login")"
csrf="$(LOGIN_JSON="$login_json" python3 -c 'import json,os; print(json.loads(os.environ["LOGIN_JSON"])["csrf_token"])')"
status_json="$(curl -fsS -b "$COOKIE_JAR" "$base_url/api/status")"
STATUS_JSON="$status_json" python3 - <<'PY'
import json, os
p = json.loads(os.environ['STATUS_JSON'])
assert p['service']['running'] is True
assert p['web']['bind'] == '0.0.0.0'
assert p['web']['authentication'] == 'session'
assert 'telephony' in p
print(json.dumps({
    'service': p['service'],
    'web': p['web'],
    'upstream': p['upstream'],
    'default_sms_app': p['default_sms_app'],
}, ensure_ascii=False, indent=2))
PY
curl -fsS -b "$COOKIE_JAR" "$base_url/api/diagnostics" >/dev/null
echo "csrf_token_length=${#csrf}"

echo
echo "== no Android launcher UI =="
launcher="$("$ADB" -s "$SERIAL" shell cmd package resolve-activity --brief \
  -a android.intent.action.MAIN -c android.intent.category.LAUNCHER "$PACKAGE" 2>/dev/null || true)"
printf '%s\n' "$launcher"
if grep -q "$PACKAGE/" <<<"$launcher"; then
  echo "Agent unexpectedly exposes a launcher activity." >&2
  exit 5
fi

echo
echo "== package and foreground service =="
"$ADB" -s "$SERIAL" shell dumpsys package "$PACKAGE" | sed -n \
  '/versionCode=/p;/versionName=/p;/runtime permissions:/,/Queries:/p'
"$ADB" -s "$SERIAL" shell dumpsys activity services "$PACKAGE" | \
  grep -E 'AgentService|isForeground=true|foregroundId=' || true

echo
echo "== fatal logs =="
fatal_logs="$("$ADB" -s "$SERIAL" logcat -d -v brief 'AndroidRuntime:E' '*:S' || true)"
printf '%s\n' "$fatal_logs"
if grep -q 'FATAL EXCEPTION' <<<"$fatal_logs"; then
  echo "AndroidRuntime reported a fatal exception." >&2
  exit 6
fi

echo
echo "result=PASS"
