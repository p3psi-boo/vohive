#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ADB="${ADB:-adb}"
APK="${APK:-$ROOT/app/build/outputs/apk/debug/app-debug.apk}"
PACKAGE="com.vohive.agent"
SERIAL="${1:-${SERIAL:-}}"
HTTP_PORT="${HTTP_PORT:-8765}"
WEB_USERNAME="${WEB_USERNAME:-admin}"
WEB_PASSWORD="${WEB_PASSWORD:-}"
SERVER_URL="${SERVER_URL:-}"
DEVICE_ID="${DEVICE_ID:-}"
AGENT_ID="${AGENT_ID:-}"
PAIR_TOKEN="${PAIR_TOKEN:-}"
AUTO_START="${AUTO_START:-true}"
INSTALL_APK="${INSTALL_APK:-true}"

if [[ -z "$SERIAL" ]]; then
  mapfile -t devices < <("$ADB" devices | awk 'NR > 1 && $2 == "device" {print $1}')
  if (( ${#devices[@]} != 1 )); then
    "$ADB" devices -l
    echo "usage: $0 SERIAL" >&2
    exit 2
  fi
  SERIAL="${devices[0]}"
fi
if [[ -z "$WEB_PASSWORD" ]]; then
  WEB_PASSWORD="vh-$(python3 -c 'import secrets; print(secrets.token_urlsafe(15))')"
fi
if (( ${#WEB_PASSWORD} < 12 )); then
  echo "WEB_PASSWORD must contain at least 12 characters." >&2
  exit 3
fi
if (( HTTP_PORT < 1024 || HTTP_PORT > 65535 )); then
  echo "HTTP_PORT must be between 1024 and 65535." >&2
  exit 4
fi
if [[ "$INSTALL_APK" == "true" ]]; then
  [[ -f "$APK" ]] || { echo "APK not found: $APK" >&2; exit 5; }
  "$ADB" -s "$SERIAL" install --no-streaming -r "$APK"
fi

permissions=(
  android.permission.READ_PHONE_STATE
  android.permission.READ_PHONE_NUMBERS
  android.permission.ACCESS_COARSE_LOCATION
  android.permission.ACCESS_FINE_LOCATION
  android.permission.SEND_SMS
  android.permission.RECEIVE_SMS
  android.permission.READ_SMS
)
sdk="$("$ADB" -s "$SERIAL" shell getprop ro.build.version.sdk | tr -d '\r')"
if (( sdk >= 33 )); then permissions+=(android.permission.POST_NOTIFICATIONS); fi
for permission in "${permissions[@]}"; do
  "$ADB" -s "$SERIAL" shell pm grant "$PACKAGE" "$permission" >/dev/null || true
done
"$ADB" -s "$SERIAL" shell cmd role add-role-holder android.app.role.SMS "$PACKAGE" 0 >/dev/null || true

agent_enabled=false
if [[ -n "$SERVER_URL" && -n "$PAIR_TOKEN" ]]; then agent_enabled=true; fi
args=(
  -a com.vohive.agent.PROVISION
  -n "$PACKAGE/.HeadlessCommandReceiver"
  --es web_username "$WEB_USERNAME"
  --es web_password "$WEB_PASSWORD"
  --ei http_port "$HTTP_PORT"
  --ez auto_start "$AUTO_START"
  --ez agent_enabled "$agent_enabled"
)
[[ -n "$SERVER_URL" ]] && args+=(--es server_url "$SERVER_URL")
[[ -n "$DEVICE_ID" ]] && args+=(--es device_id "$DEVICE_ID")
[[ -n "$AGENT_ID" ]] && args+=(--es agent_id "$AGENT_ID")
[[ -n "$PAIR_TOKEN" ]] && args+=(--es pair_token "$PAIR_TOKEN")

broadcast_output="$("$ADB" -s "$SERIAL" shell am broadcast --receiver-foreground \
  "${args[@]}" 2>&1)"
printf '%s\n' "$broadcast_output"
if grep -Eq 'SecurityException|Permission Denial' <<<"$broadcast_output"; then
  echo "Headless provisioning broadcast was rejected." >&2
  exit 6
fi
service_output="$("$ADB" -s "$SERIAL" shell am start -W \
  -n "$PACKAGE/.HeadlessBootstrapActivity" 2>&1)"
printf '%s\n' "$service_output"
if grep -Eq 'SecurityException|Permission Denial|Error type|Error:' <<<"$service_output"; then
  echo "Headless bootstrap start was rejected." >&2
  exit 7
fi

phone_ip="$("$ADB" -s "$SERIAL" shell ip -o -4 addr show scope global |
  awk '$2 ~ /^(wlan|eth)/ {print $4; exit}' | cut -d/ -f1 | tr -d '\r')"
actual_agent_id="$("$ADB" -s "$SERIAL" shell run-as "$PACKAGE" cat \
  shared_prefs/vohive-agent.xml 2>/dev/null | \
  sed -n 's/.*<string name="agent_id">\([^<]*\)<\/string>.*/\1/p' | head -1 | tr -d '\r')"
echo "serial=$SERIAL"
echo "agent_id=$actual_agent_id"
echo "username=$WEB_USERNAME"
echo "password=$WEB_PASSWORD"
if [[ -n "$phone_ip" ]]; then
  echo "url=http://$phone_ip:$HTTP_PORT/"
else
  echo "url=http://DEVICE_LAN_IP:$HTTP_PORT/"
fi
