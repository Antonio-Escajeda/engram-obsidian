#!/usr/bin/env bash
set -euo pipefail

SERVICE="engram-obsidian.service"
SERVICE_FILE="${HOME}/.config/systemd/user/${SERVICE}"
SELECTION_FILE="${HOME}/.engram/obsidian-selection.json"
DEFAULT_VAULT_WSL=""
DEFAULT_VAULT_UNIX="${HOME}/Documents/EngramVault"

if [[ -n "${WSL_DISTRO_NAME:-}" ]]; then
  DEFAULT_VAULT_WSL="/mnt/c/Users/${USER}/Documents/EngramVault"
fi

ok() {
  printf "[OK] %s\n" "$1"
}

warn() {
  printf "[WARN] %s\n" "$1"
}

info() {
  printf "[INFO] %s\n" "$1"
}

fail() {
  printf "[FAIL] %s\n" "$1"
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "Comando requerido no encontrado: $1"
}

require_cmd systemctl
require_cmd journalctl
require_cmd python3

info "Validando estado del servicio ${SERVICE}"
if systemctl --user is-enabled "$SERVICE" >/dev/null 2>&1; then
  ok "Servicio habilitado"
else
  warn "Servicio no habilitado"
fi

if systemctl --user is-active "$SERVICE" >/dev/null 2>&1; then
  ok "Servicio activo"
else
  fail "Servicio inactivo"
fi

if [[ -f "$SERVICE_FILE" ]]; then
  ok "Existe service file: $SERVICE_FILE"
  if grep -q "^Environment=ENGRAM_DATA_DIR=%h/.engram$" "$SERVICE_FILE"; then
    ok "Service define ENGRAM_DATA_DIR portable (%h/.engram)"
  else
    warn "Service no define ENGRAM_DATA_DIR=%h/.engram (se usará fallback del daemon)"
  fi
else
  fail "No existe service file: $SERVICE_FILE"
fi

info "Revisando logs recientes"
LOGS="$(journalctl --user -u "$SERVICE" -n 200 --no-pager || true)"
if [[ -z "$LOGS" ]]; then
  warn "Sin logs recientes"
else
  ok "Logs recuperados"
fi

if [[ -f "$SELECTION_FILE" ]]; then
  ok "Existe archivo de selección: $SELECTION_FILE"
else
  fail "No existe archivo de selección: $SELECTION_FILE"
fi

info "Validando campos de selección (config y confirmed)"
python3 - <<'PY'
import json
import os
import sys

selection_file = os.path.expanduser("~/.engram/obsidian-selection.json")
with open(selection_file, "r", encoding="utf-8") as f:
    data = json.load(f)

config = data.get("config", {})
vault = config.get("vault_path", "")
db_path = config.get("db_path", "")
confirmed = data.get("confirmed")

if not vault:
    print("[FAIL] vault_path vacío")
    sys.exit(1)
if not db_path:
    print("[FAIL] db_path vacío")
    sys.exit(1)

print(f"[OK] vault_path={vault}")
print(f"[OK] db_path={db_path}")

if db_path.startswith("/"):
    print("[OK] db_path absoluto (compatibilidad preservada)")
elif db_path.startswith("~/") or db_path == "~":
    print("[OK] db_path con ~ (portable)")
else:
    print("[OK] db_path relativo (se resuelve contra ENGRAM_DATA_DIR)")

if confirmed is True:
    print("[OK] confirmed=true")
elif confirmed is False:
    print("[WARN] confirmed=false (primera instalación sin --select)")
else:
    print("[INFO] confirmed ausente (archivo legacy, compatible)")
PY

VAULT_PATH="$(python3 - <<'PY'
import json
import os
selection_file = os.path.expanduser("~/.engram/obsidian-selection.json")
with open(selection_file, "r", encoding="utf-8") as f:
    data = json.load(f)
print(data.get("config", {}).get("vault_path", ""))
PY
)"

if [[ -z "$VAULT_PATH" ]]; then
  fail "No se pudo obtener vault_path"
fi

if [[ -d "$VAULT_PATH" ]]; then
  ok "El directorio del vault existe: $VAULT_PATH"
else
  fail "El directorio del vault no existe: $VAULT_PATH"
fi

if [[ -n "$DEFAULT_VAULT_WSL" ]]; then
  if [[ "$VAULT_PATH" == /mnt/* ]]; then
    ok "En WSL el vault apunta a ruta Windows/NTFS"
  else
    warn "En WSL el vault no apunta a /mnt/... (actual: $VAULT_PATH)"
    info "Ruta sugerida: $DEFAULT_VAULT_WSL"
  fi
else
  if [[ "$VAULT_PATH" == "$DEFAULT_VAULT_UNIX" ]]; then
    ok "Vault path coincide con default esperado"
  else
    info "Vault path custom detectado: $VAULT_PATH"
  fi
fi

if grep -q "Selection not confirmed — run --select first; skipping sync" <<<"$LOGS"; then
  info "Se detectó estado pre-selección en logs"
fi

if grep -q "Conditions MET" <<<"$LOGS" && grep -q "Sync complete" <<<"$LOGS"; then
  ok "Se detectó al menos una sincronización completa"
else
  warn "No se detectó sync completo en las últimas 200 líneas"
fi

if grep -q "Conditions not met — cleaning _engram vault content" <<<"$LOGS"; then
  ok "Se detectó limpieza cuando condiciones no se cumplen"
else
  info "No se detectó evento de cleanup en las últimas 200 líneas"
fi

info "Validación finalizada"
