#!/bin/zsh

set -euo pipefail

WORKSPACE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
NEW_RUNTIME_ROOT_DEFAULT="$HOME/Library/Application Support/KubeFeel"
LEGACY_RUNTIME_ROOT_DEFAULT="$HOME/Library/Application Support/KubeFleet"
RUNTIME_ROOT="${KUBEFEEL_RUNTIME_ROOT:-${KUBEFLEET_RUNTIME_ROOT:-$NEW_RUNTIME_ROOT_DEFAULT}}"
RUNTIME_BIN_DIR="$RUNTIME_ROOT/bin"
RUNTIME_FRONTEND_DIR="$RUNTIME_ROOT/frontend-dist"
RUNTIME_CHART_DIR="$RUNTIME_ROOT/charts"
RUNTIME_DB_PATH="$RUNTIME_ROOT/app.db"
RUNTIME_SECRET_DIR="$RUNTIME_ROOT/secrets"
JWT_SECRET_FILE="$RUNTIME_SECRET_DIR/jwt_secret"
ENCRYPTION_SECRET_FILE="$RUNTIME_SECRET_DIR/encryption_secret"
BOOTSTRAP_ADMIN_PASSWORD_FILE="$RUNTIME_SECRET_DIR/bootstrap_admin_password"
PLIST_PATH="$HOME/Library/LaunchAgents/io.kubefeel.server.plist"
LEGACY_PLIST_PATH="$HOME/Library/LaunchAgents/io.kubefleet.server.plist"
LEGACY_COMPANY_PLIST_PATH="$HOME/Library/LaunchAgents/com.bertreyking.kubefeel-server.plist"
LEGACY_COMPANY_FLEET_PLIST_PATH="$HOME/Library/LaunchAgents/com.bertreyking.kubefleet-server.plist"
LOG_DIR="$HOME/Library/Logs"
UID_VALUE="$(id -u)"
LABEL="io.kubefeel.server"
LEGACY_LABEL="io.kubefleet.server"
LEGACY_COMPANY_LABEL="com.bertreyking.kubefeel-server"
LEGACY_COMPANY_FLEET_LABEL="com.bertreyking.kubefleet-server"
BINARY_NAME="kubefeel-server"
LEGACY_DB_PATH="$LEGACY_RUNTIME_ROOT_DEFAULT/app.db"

ensure_secret_file() {
  local file_path="$1"
  local byte_length="$2"
  if [[ -s "$file_path" ]]; then
    return
  fi

  mkdir -p "$(dirname "$file_path")"
  python3 - "$file_path" "$byte_length" <<'PY'
import base64
import os
import secrets
import sys

target = sys.argv[1]
size = int(sys.argv[2])
value = base64.urlsafe_b64encode(secrets.token_bytes(size)).decode().rstrip("=")
with open(target, "w", encoding="utf-8") as fp:
    fp.write(value)
PY
  chmod 600 "$file_path"
}

read_plist_env() {
  local plist_path="$1"
  local key="$2"
  if [[ ! -f "$plist_path" ]]; then
    return
  fi

  /usr/libexec/PlistBuddy -c "Print :EnvironmentVariables:$key" "$plist_path" 2>/dev/null || true
}

migrate_secret_from_legacy_plists() {
  local target_file="$1"
  local env_key="$2"
  shift 2

  if [[ -s "$target_file" ]]; then
    return
  fi

  local value=""
  local plist_path
  for plist_path in "$@"; do
    value="$(read_plist_env "$plist_path" "$env_key")"
    if [[ -n "$value" ]]; then
      mkdir -p "$(dirname "$target_file")"
      printf '%s' "$value" >"$target_file"
      chmod 600 "$target_file"
      return
    fi
  done
}

mkdir -p "$RUNTIME_BIN_DIR" "$RUNTIME_FRONTEND_DIR" "$RUNTIME_CHART_DIR" "$RUNTIME_SECRET_DIR" "$(dirname "$PLIST_PATH")" "$LOG_DIR"

cd "$WORKSPACE_DIR/frontend"
npm run build

cd "$WORKSPACE_DIR"
go build -o "$RUNTIME_BIN_DIR/$BINARY_NAME" ./cmd/server
rsync -a --delete "$WORKSPACE_DIR/frontend/dist/" "$RUNTIME_FRONTEND_DIR/"
rsync -a --delete "$WORKSPACE_DIR/charts/" "$RUNTIME_CHART_DIR/"

if [[ ! -f "$RUNTIME_DB_PATH" && -f "$LEGACY_DB_PATH" ]]; then
  sqlite3 "$LEGACY_DB_PATH" ".backup '$RUNTIME_DB_PATH'"
elif [[ ! -f "$RUNTIME_DB_PATH" && -f "$WORKSPACE_DIR/app.db" ]]; then
  sqlite3 "$WORKSPACE_DIR/app.db" ".backup '$RUNTIME_DB_PATH'"
fi

migrate_secret_from_legacy_plists \
  "$JWT_SECRET_FILE" \
  "APP_JWT_SECRET" \
  "$LEGACY_COMPANY_PLIST_PATH" \
  "$LEGACY_COMPANY_FLEET_PLIST_PATH" \
  "$LEGACY_PLIST_PATH"
migrate_secret_from_legacy_plists \
  "$ENCRYPTION_SECRET_FILE" \
  "APP_ENCRYPTION_SECRET" \
  "$LEGACY_COMPANY_PLIST_PATH" \
  "$LEGACY_COMPANY_FLEET_PLIST_PATH" \
  "$LEGACY_PLIST_PATH"
migrate_secret_from_legacy_plists \
  "$BOOTSTRAP_ADMIN_PASSWORD_FILE" \
  "APP_BOOTSTRAP_ADMIN_PASSWORD" \
  "$LEGACY_COMPANY_PLIST_PATH" \
  "$LEGACY_COMPANY_FLEET_PLIST_PATH" \
  "$LEGACY_PLIST_PATH"

ensure_secret_file "$JWT_SECRET_FILE" 32
ensure_secret_file "$ENCRYPTION_SECRET_FILE" 32
ensure_secret_file "$BOOTSTRAP_ADMIN_PASSWORD_FILE" 24

JWT_SECRET_VALUE="$(cat "$JWT_SECRET_FILE")"
ENCRYPTION_SECRET_VALUE="$(cat "$ENCRYPTION_SECRET_FILE")"
BOOTSTRAP_ADMIN_PASSWORD_VALUE="$(cat "$BOOTSTRAP_ADMIN_PASSWORD_FILE")"

cat >"$PLIST_PATH" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>$RUNTIME_BIN_DIR/$BINARY_NAME</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$RUNTIME_ROOT</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>APP_ADDR</key>
    <string>:18081</string>
    <key>APP_DB_PATH</key>
    <string>$RUNTIME_DB_PATH</string>
    <key>APP_FRONTEND_DIR</key>
    <string>$RUNTIME_FRONTEND_DIR</string>
    <key>APP_JWT_SECRET</key>
    <string>$JWT_SECRET_VALUE</string>
    <key>APP_ENCRYPTION_SECRET</key>
    <string>$ENCRYPTION_SECRET_VALUE</string>
    <key>APP_BOOTSTRAP_ADMIN_PASSWORD</key>
    <string>$BOOTSTRAP_ADMIN_PASSWORD_VALUE</string>
  </dict>
  <key>StandardOutPath</key>
  <string>$LOG_DIR/kubefeel-server.log</string>
  <key>StandardErrorPath</key>
  <string>$LOG_DIR/kubefeel-server.err.log</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
</dict>
</plist>
EOF

launchctl bootout "gui/$UID_VALUE/$LEGACY_LABEL" >/dev/null 2>&1 || true
launchctl bootout "gui/$UID_VALUE/$LEGACY_COMPANY_LABEL" >/dev/null 2>&1 || true
launchctl bootout "gui/$UID_VALUE/$LEGACY_COMPANY_FLEET_LABEL" >/dev/null 2>&1 || true
launchctl bootout "gui/$UID_VALUE/$LABEL" >/dev/null 2>&1 || true
rm -f "$LEGACY_PLIST_PATH"
rm -f "$LEGACY_COMPANY_PLIST_PATH"
rm -f "$LEGACY_COMPANY_FLEET_PLIST_PATH"
launchctl bootstrap "gui/$UID_VALUE" "$PLIST_PATH"
launchctl kickstart -k "gui/$UID_VALUE/$LABEL"

echo "KubeFeel local service installed."
echo "Bootstrap admin password file: $BOOTSTRAP_ADMIN_PASSWORD_FILE"
