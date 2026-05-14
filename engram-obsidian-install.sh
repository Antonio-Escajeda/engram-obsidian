#!/usr/bin/env bash
# engram-obsidian-install.sh
# Installs the engram-obsidian daemon with auto-detected Windows user path.
# Instala el daemon engram-obsidian con detección automática del path del usuario Windows.
# Idempotent: safe to re-run; updates an existing installation.
# Idempotente: se puede volver a correr sin problemas; actualiza una instalación existente.

set -euo pipefail

# ── Colors ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

info()    { echo -e "${CYAN}[info]${NC}  $*"; }
ok()      { echo -e "${GREEN}[ok]${NC}    $*"; }
warn()    { echo -e "${YELLOW}[warn]${NC}  $*"; }
die()     { echo -e "${RED}[error]${NC} $*" >&2; exit 1; }
header()  { echo -e "\n${BOLD}$*${NC}"; }

# ── Dependencies ──────────────────────────────────────────────────────────────
header "==> Checking WSL dependencies"

# 1. systemd enabled in WSL
# 1. systemd habilitado en WSL
_wsl_conf="/etc/wsl.conf"
_systemd_enabled=false
if [[ -f "$_wsl_conf" ]]; then
    if grep -qP '^\s*systemd\s*=\s*true' "$_wsl_conf" 2>/dev/null; then
        _systemd_enabled=true
    fi
fi
if [[ "$_systemd_enabled" == false ]]; then
    echo -e "${YELLOW}⚠ systemd no está habilitado en WSL.${NC}"
    echo -e "${YELLOW}  Agregando systemd=true a ${_wsl_conf} (requiere sudo)...${NC}"
    if ! grep -qP '^\s*\[boot\]' "$_wsl_conf" 2>/dev/null; then
        echo -e "\n[boot]\nsystemd=true" | sudo tee -a "$_wsl_conf" >/dev/null
    else
        # [boot] section exists — append systemd=true after it
        # La sección [boot] ya existe — agregar systemd=true después de ella
        sudo sed -i '/^\s*\[boot\]/a systemd=true' "$_wsl_conf"
    fi
    echo ""
    echo -e "${YELLOW}⚠ systemd habilitado — necesitás reiniciar WSL:${NC}"
    echo -e "  Ejecutá desde PowerShell: ${BOLD}wsl --shutdown${NC}"
    echo -e "  Luego volvé a abrir WSL y corré este script de nuevo."
    echo ""
    exit 1
fi
echo "[deps] systemd OK"

# 2. Python 3
if ! command -v python3 &>/dev/null; then
    echo "[deps] instalando python3..."
    sudo apt-get update -qq && sudo apt-get install -y python3 \
        || die "Falló la instalación de python3"
fi
echo "[deps] python3 OK"

# 3. Homebrew
if ! command -v brew &>/dev/null; then
    if [[ -x "/home/linuxbrew/.linuxbrew/bin/brew" ]]; then
        eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"
    fi
fi
if ! command -v brew &>/dev/null; then
    echo "[deps] instalando Homebrew..."
    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" \
        || die "Falló la instalación de Homebrew"
    eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"
else
    # Already installed — ensure it's in PATH for this session
    # Ya instalado — asegurarse de que esté en el PATH para esta sesión
    if [[ -x "/home/linuxbrew/.linuxbrew/bin/brew" ]]; then
        eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"
    fi
fi
command -v brew &>/dev/null || die "brew no está disponible después de la instalación"
echo "[deps] Homebrew OK"

# 4. Engram
if ! command -v engram &>/dev/null; then
    echo "[deps] instalando engram via brew..."
    brew tap gentleman-programming/tap || die "Falló brew tap gentleman-programming/tap"
    brew install engram || die "Falló brew install engram"
else
    _engram_ver=$(engram --version 2>/dev/null || engram version 2>/dev/null || echo "desconocida")
    echo "[deps] engram OK (${_engram_ver})"
fi
command -v engram &>/dev/null || die "engram no está disponible después de la instalación"
echo "[deps] engram instalado"

# 5. engram serve running
# 5. engram serve en ejecución
# Read port early (before auto-detection block sets ENGRAM_PORT) — use env var or default 7437
# Leer el puerto anticipadamente (antes del bloque de auto-detección) — usar variable de entorno o el valor por defecto 7437
_pre_port="${ENGRAM_PORT:-7437}"
if ! curl -sf "http://localhost:${_pre_port}/health" --max-time 2 &>/dev/null; then
    echo "[deps] iniciando engram serve en background..."
    engram serve &>/dev/null &
    sleep 2
    if ! curl -sf "http://localhost:${_pre_port}/health" --max-time 2 &>/dev/null; then
        warn "engram serve arrancó pero el health check falló — continuando igual"
    else
        echo "[deps] engram serve OK"
    fi
else
    echo "[deps] engram serve OK (ya estaba corriendo)"
fi

# 6. ~/.local/bin in PATH
# 6. ~/.local/bin en el PATH
mkdir -p "$HOME/.local/bin"
if [[ ":$PATH:" != *":$HOME/.local/bin:"* ]]; then
    echo "[deps] agregando ~/.local/bin al PATH en ~/.bashrc y ~/.zshrc..."
    for _rc in "$HOME/.bashrc" "$HOME/.zshrc"; do
        if [[ -f "$_rc" ]]; then
            echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$_rc"
            echo "[deps] → actualizado $_rc"
        fi
    done
    export PATH="$HOME/.local/bin:$PATH"
fi
echo "[deps] ~/.local/bin OK"

echo ""

# ── Auto-detection ────────────────────────────────────────────────────────────
header "==> Auto-detecting environment"

# 1. Windows mount point — find where Windows/system32/tasklist.exe lives under /mnt/
# 1. Punto de montaje de Windows — encontrar dónde vive Windows/system32/tasklist.exe bajo /mnt/
WIN_MOUNT=""
for _mnt in /mnt/c /mnt/d /mnt/e /mnt/f; do
    if [[ -f "${_mnt}/Windows/system32/tasklist.exe" ]]; then
        WIN_MOUNT="$_mnt"
        break
    fi
done
if [[ -z "$WIN_MOUNT" ]]; then
    # Broader scan under /mnt/ (one level deep)
    # Búsqueda más amplia bajo /mnt/ (un nivel de profundidad)
    while IFS= read -r -d '' _entry; do
        if [[ -f "${_entry}/Windows/system32/tasklist.exe" ]]; then
            WIN_MOUNT="$_entry"
            break
        fi
    done < <(find /mnt -mindepth 1 -maxdepth 1 -type d -print0 2>/dev/null)
fi
WIN_MOUNT="${WIN_MOUNT:-/mnt/c}"
ok "Windows mount point: ${BOLD}${WIN_MOUNT}${NC}"

# 2. Vault name and base folder — configurable via env, defaults to EngramVault/Documents
# 2. Nombre y carpeta base del vault — configurable via variable de entorno, por defecto EngramVault/Documents
OBSIDIAN_VAULT_NAME="${OBSIDIAN_VAULT_NAME:-EngramVault}"
OBSIDIAN_VAULT_BASE="${OBSIDIAN_VAULT_BASE:-Documents}"
ok "Vault name: ${BOLD}${OBSIDIAN_VAULT_NAME}${NC}  (override: OBSIDIAN_VAULT_NAME)"
ok "Vault base: ${BOLD}${OBSIDIAN_VAULT_BASE}${NC}  (override: OBSIDIAN_VAULT_BASE)"

# 3. Engram port — read from Engram config if available, else env var, else 7437
# 3. Puerto de Engram — leer del config de Engram si existe, sino variable de entorno, sino 7437
_detected_port=""
for _cfg in \
    "$HOME/.engram/config.json" \
    "$HOME/.config/engram/config.json" \
    "$HOME/.engram/engram.json"; do
    if [[ -f "$_cfg" ]]; then
        # Extract "port": NNNN from JSON (no jq dependency)
        # Extraer "port": NNNN del JSON (sin dependencia de jq)
        _p=$(grep -oP '"port"\s*:\s*\K[0-9]+' "$_cfg" 2>/dev/null | head -1 || true)
        if [[ -n "$_p" ]]; then
            _detected_port="$_p"
            ok "Engram port read from ${_cfg}: ${BOLD}${_detected_port}${NC}"
            break
        fi
    fi
done
ENGRAM_PORT="${ENGRAM_PORT:-${_detected_port:-7437}}"
ok "Engram port: ${BOLD}${ENGRAM_PORT}${NC}  (override: ENGRAM_PORT)"

# 4. Windows codepage + locale — read ACP and LocaleName from registry via reg.exe
# 4. Codepage + locale de Windows — leer ACP y LocaleName del registro via reg.exe
# Both codepage (for tasklist.exe decoding) and locale (for month names) are auto-detected dynamically.
_detected_cp=""
_reg_exe="${WIN_MOUNT}/Windows/system32/reg.exe"
if [[ -f "$_reg_exe" ]]; then
    _cp_raw=$("$_reg_exe" query \
        "HKLM\\SYSTEM\\CurrentControlSet\\Control\\Nls\\CodePage" \
        /v ACP 2>/dev/null | grep -oP 'REG_SZ\s+\K\S+' || true)
    if [[ -n "$_cp_raw" ]]; then
        _detected_cp="cp${_cp_raw}"
        ok "Windows codepage from registry: ${BOLD}${_detected_cp}${NC}"
    fi
fi
WIN_CODEPAGE="${_detected_cp:-cp1252}"
ok "Windows codepage: ${BOLD}${WIN_CODEPAGE}${NC}  (fallback: cp1252)"

# ── 1. Auto-detect Windows user ───────────────────────────────────────────────
header "==> Detecting Windows user"

USERS_BASE="${WIN_MOUNT}/Users"
EXCLUDE=("Public" "Default" "Default User" "All Users" "defaultuser0")

[[ -d "$USERS_BASE" ]] || die "${USERS_BASE} not found. Are you running under WSL?"

candidates=()
while IFS= read -r -d '' entry; do
    name=$(basename "$entry")
    skip=false
    for ex in "${EXCLUDE[@]}"; do
        [[ "$name" == "$ex" ]] && skip=true && break
    done
    $skip || candidates+=("$name")
done < <(find "$USERS_BASE" -mindepth 1 -maxdepth 1 -type d -print0 2>/dev/null)

[[ ${#candidates[@]} -eq 0 ]] && die "No Windows user directories found in $USERS_BASE"

if [[ ${#candidates[@]} -eq 1 ]]; then
    WINDOWS_USER="${candidates[0]}"
    info "Found single Windows user: ${BOLD}$WINDOWS_USER${NC}"
else
    info "Multiple Windows users found:"
    for i in "${!candidates[@]}"; do
        echo "  [$((i+1))] ${candidates[$i]}"
    done
    printf "\nWhich user should own the vault? [1]: "
    read -r choice
    choice="${choice:-1}"
    [[ "$choice" =~ ^[0-9]+$ && "$choice" -ge 1 && "$choice" -le "${#candidates[@]}" ]] \
        || die "Invalid selection: $choice"
    WINDOWS_USER="${candidates[$((choice-1))]}"
    ok "Selected: $WINDOWS_USER"
fi

VAULT_PATH="${WIN_MOUNT}/Users/${WINDOWS_USER}/${OBSIDIAN_VAULT_BASE}/${OBSIDIAN_VAULT_NAME}"
# Build Windows-style path: derive drive letter from mount point (e.g. /mnt/c -> C:)
# Construir el path estilo Windows: derivar la letra de unidad del punto de montaje (ej: /mnt/c -> C:)
_drive_letter=$(basename "$WIN_MOUNT" | tr '[:lower:]' '[:upper:]')
WIN_VAULT_PATH="${_drive_letter}:\\Users\\${WINDOWS_USER}\\${OBSIDIAN_VAULT_BASE}\\${OBSIDIAN_VAULT_NAME}"
info "Vault path: $VAULT_PATH"

# ── 2. Create vault directory ─────────────────────────────────────────────────
header "==> Preparing vault directory"

mkdir -p "$VAULT_PATH"
ok "Vault directory ready: $VAULT_PATH"

# ── 3. Copy or create .obsidian config ────────────────────────────────────────
header "==> Setting up Obsidian config"

OBSIDIAN_SRC="$HOME/.engram/ENGRAM/.obsidian"
OBSIDIAN_DEST="$VAULT_PATH/.obsidian"

if [[ -d "$OBSIDIAN_SRC" ]]; then
    cp -r "$OBSIDIAN_SRC" "$OBSIDIAN_DEST"
    ok "Copied .obsidian/ from $OBSIDIAN_SRC"
else
    warn "No .obsidian/ found at $OBSIDIAN_SRC — creating minimal config"
    mkdir -p "$OBSIDIAN_DEST"
    cat > "$OBSIDIAN_DEST/app.json" <<'APPJSON'
{
  "legacyEditor": false,
  "livePreview": true,
  "showLineNumber": false,
  "strictLineBreaks": false,
  "foldHeading": true,
  "foldIndent": true,
  "defaultViewMode": "preview",
  "tabSize": 2
}
APPJSON
    ok "Created minimal .obsidian/app.json"
fi

# ── 4. Install the daemon ─────────────────────────────────────────────────────
header "==> Installing engram-obsidian daemon"

DAEMON_DEST="$HOME/.local/bin/engram-obsidian"
mkdir -p "$(dirname "$DAEMON_DEST")"

cat > "$DAEMON_DEST" <<PYEOF
#!/usr/bin/env python3
"""
engram-obsidian — Syncs Engram memories to an Obsidian vault.

Behavior:
  - Polls tasklist.exe every 5 seconds to detect Obsidian.exe
  - Obsidian opens  (OFF->ON): fetches /export, writes _engram/ into the vault
  - Obsidian closes (ON->OFF): deletes ONLY ~/.engram/ENGRAM/_engram/ recursively
  - On startup: if Obsidian is already running, syncs immediately
  - API errors are logged and swallowed — never crash the daemon

File structure: _engram/{project}/{type}/{date} {title}.md
"""

import json
import os
import shutil
import subprocess
import time
import urllib.request
import urllib.error
from datetime import datetime
from pathlib import Path

# ── Configuration ────────────────────────────────────────────────────────────
ENGRAM_URL   = "http://localhost:${ENGRAM_PORT}/export"
VAULT_DIR    = Path("${VAULT_PATH}")
ENGRAM_DIR   = VAULT_DIR / "_engram"
POLL_INTERVAL  = 5  # seconds
TASKLIST_EXE   = "${WIN_MOUNT}/Windows/system32/tasklist.exe"
INVALID_CHARS  = str.maketrans('<>:"/\\\\|?*', '---------')
MAX_FILENAME  = 100

# ── Helpers ───────────────────────────────────────────────────────────────────

def log(msg: str) -> None:
    print(f"[{datetime.now():%H:%M:%S}] {msg}", flush=True)


def sanitize(name: str) -> str:
    """Replace filesystem-invalid chars and truncate to MAX_FILENAME."""
    return name.translate(INVALID_CHARS)[:MAX_FILENAME].strip()


def obsidian_running() -> bool:
    """Return True if any Obsidian.exe process is found via tasklist.exe."""
    try:
        result = subprocess.run(
            [TASKLIST_EXE],
            capture_output=True,
            timeout=10,
        )
        # tasklist.exe output encoding detected from Windows registry ACP key
        # Codificación de salida de tasklist.exe detectada desde la clave ACP del registro de Windows
        output = result.stdout.decode("${WIN_CODEPAGE}", errors="replace")
        return "Obsidian.exe" in output
    except (subprocess.TimeoutExpired, FileNotFoundError, OSError, UnicodeDecodeError) as exc:
        log(f"WARN tasklist.exe failed: {exc}")
        return False


def root_session_active() -> bool:
    """Return True if root has an interactive shell on a pts device.

    Detects 'wsl -u root', 'su -', and 'sudo -i' sessions.
    Reads /proc directly — no subprocess, no utmp dependency.
    """
    INTERACTIVE_SHELLS = {"bash", "zsh", "sh", "fish", "dash"}
    try:
        for pid in os.listdir("/proc"):
            if not pid.isdigit():
                continue
            try:
                with open(f"/proc/{pid}/status") as f:
                    for line in f:
                        if line.startswith("Uid:"):
                            if int(line.split()[1]) != 0:
                                break
                            with open(f"/proc/{pid}/comm") as fc:
                                if fc.read().strip() not in INTERACTIVE_SHELLS:
                                    break
                            with open(f"/proc/{pid}/stat") as fs:
                                stat = fs.read()
                            tty_nr = int(stat[stat.rfind(")") + 2:].split()[4])
                            if tty_nr != 0 and ((tty_nr >> 8) & 0xff) == 136:
                                return True
                            break
            except (PermissionError, FileNotFoundError, ValueError):
                continue
    except OSError as exc:
        log(f"WARN root_session_active scan failed: {exc}")
    return False


def should_sync() -> bool:
    """Combined gate: Obsidian must be running AND root must have an active session."""
    return obsidian_running() and root_session_active()


def fetch_engram() -> dict | None:
    """Fetch /export from Engram. Returns parsed JSON or None on error."""
    try:
        with urllib.request.urlopen(ENGRAM_URL, timeout=10) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.URLError as exc:
        log(f"WARN Engram API unreachable: {exc.reason}")
        return None
    except (json.JSONDecodeError, Exception) as exc:
        log(f"WARN Engram API error: {exc}")
        return None


# ── Sync logic ────────────────────────────────────────────────────────────────

MONTH_NAMES_BY_LANG = {
    "es": ["enero","febrero","marzo","abril","mayo","junio",
           "julio","agosto","septiembre","octubre","noviembre","diciembre"],
    "en": ["january","february","march","april","may","june",
           "july","august","september","october","november","december"],
    "fr": ["janvier","février","mars","avril","mai","juin",
           "juillet","août","septembre","octobre","novembre","décembre"],
    "pt": ["janeiro","fevereiro","março","abril","maio","junho",
           "julho","agosto","setembro","outubro","novembro","dezembro"],
    "de": ["januar","februar","märz","april","mai","juni",
           "juli","august","september","oktober","november","dezember"],
    "it": ["gennaio","febbraio","marzo","aprile","maggio","giugno",
           "luglio","agosto","settembre","ottobre","novembre","dicembre"],
}


def detect_windows_locale() -> str:
    """Detect Windows locale language code via registry. Returns 2-char lang code, default 'en'."""
    try:
        reg_exe = TASKLIST_EXE.replace("tasklist.exe", "reg.exe")
        result = subprocess.run(
            [reg_exe, "query", r"HKCU\Control Panel\International", "/v", "LocaleName"],
            capture_output=True, timeout=5
        )
        output = result.stdout.decode("cp1252", errors="replace")
        for line in output.splitlines():
            if "LocaleName" in line:
                locale = line.split()[-1].strip()  # "es-MX"
                return locale[:2].lower()           # "es"
    except Exception:
        pass
    return "en"


MONTH_NAMES = MONTH_NAMES_BY_LANG.get(detect_windows_locale(), MONTH_NAMES_BY_LANG["en"])


def write_observation(obs: dict, base_dir: Path) -> None:
    """Write a single observation as a Markdown file."""
    project   = sanitize(obs.get("project") or "unknown")
    obs_type  = sanitize(obs.get("type")    or "unknown")
    title     = obs.get("title", "untitled")
    safe_title = sanitize(title)
    created   = (obs.get("created_at") or "")[:10]

    filename  = f"{created} {safe_title}.md" if created else f"{safe_title}.md"

    if created:
        year = created[:4]
        month_num = int(created[5:7])
        month_dir = f"{created[5:7]}-{MONTH_NAMES[month_num - 1]}"
    else:
        year = "sin-fecha"
        month_dir = "sin-fecha"

    dest_dir = base_dir / project / year / month_dir / obs_type
    dest_dir.mkdir(parents=True, exist_ok=True)

    scope     = obs.get("scope", "")
    topic_key = obs.get("topic_key", "")
    updated   = (obs.get("updated_at") or "")[:10]
    obs_id    = obs.get("id", "")
    content   = obs.get("content", "")

    tags = ["engram", obs_type, project]

    frontmatter = (
        "---\n"
        f"id: {obs_id}\n"
        f"type: {obs_type}\n"
        f"project: {project}\n"
        f"scope: {scope}\n"
        f"topic_key: {topic_key}\n"
        f"created: {created}\n"
        f"updated: {updated}\n"
        f"tags: [{', '.join(tags)}]\n"
        "---\n"
    )

    md = f"{frontmatter}\n> [[{project}/{project}|{project}]]\n\n# {title}\n\n{content}\n"
    (dest_dir / filename).write_text(md, encoding="utf-8")


def write_year_index(project: str, year: str, months_data: dict, base_dir: Path) -> None:
    """Write _index.md for a year node listing all months."""
    lines = [
        "---",
        f"tags: [engram, index, {project}, {year}]",
        "---",
        "",
        f"# {project} / {year}",
        "",
    ]
    for month_dir in sorted(months_data.keys(), reverse=True):
        count = sum(len(v) for v in months_data[month_dir].values())
        lines.append(f"- [[{project}/{year}/{month_dir}/{month_dir}|{month_dir}]] ({count} memorias)")
    lines.append("")
    year_path = base_dir / project / year
    year_path.mkdir(parents=True, exist_ok=True)
    (year_path / f"{year}.md").write_text("\n".join(lines), encoding="utf-8")


def write_month_index(project: str, year: str, month_dir: str, types_data: dict, base_dir: Path) -> None:
    """Write _index.md for a month node listing observations by type."""
    lines = [
        "---",
        f"tags: [engram, index, {project}, {year}, {month_dir}]",
        "---",
        "",
        f"# {project} / {year} / {month_dir}",
        "",
    ]
    for obs_type in sorted(types_data.keys()):
        entries = types_data[obs_type]
        lines.append(f"## {obs_type} ({len(entries)})")
        for link in entries:
            lines.append(f"- {link}")
        lines.append("")
    month_path = base_dir / project / year / month_dir
    month_path.mkdir(parents=True, exist_ok=True)
    (month_path / f"{month_dir}.md").write_text("\n".join(lines), encoding="utf-8")


def write_project_index(project: str, obs_list: list, base_dir: Path) -> None:
    """Write _index.md for a single project grouped by year → month → type."""
    from collections import defaultdict

    # Structure: by_year[year][month_dir][obs_type] = [wikilink, ...]
    by_year: dict[str, dict] = defaultdict(lambda: defaultdict(lambda: defaultdict(list)))
    for o in obs_list:
        obs_type  = sanitize(o.get("type") or "unknown")
        title     = o.get("title", "untitled")
        safe_title = sanitize(title)
        created   = (o.get("created_at") or "")[:10]
        filename  = f"{created} {safe_title}" if created else safe_title
        if created:
            year = created[:4]
            month_num = int(created[5:7])
            month_dir = f"{created[5:7]}-{MONTH_NAMES[month_num - 1]}"
        else:
            year = "sin-fecha"
            month_dir = "sin-fecha"
        wikilink = f"[[{project}/{year}/{month_dir}/{obs_type}/{filename}|{title}]]"
        by_year[year][month_dir][obs_type].append(wikilink)

    total_proj = sum(
        len(links)
        for months in by_year.values()
        for types in months.values()
        for links in types.values()
    )

    lines = [
        "---",
        f"tags: [engram, index, {project}]",
        "---",
        "",
        f"# {project}",
        "",
        f"{total_proj} memorias",
        "",
    ]

    for year in sorted(by_year.keys(), reverse=True):
        lines.append(f"## {year}")
        for month_dir in sorted(by_year[year].keys(), reverse=True):
            lines.append(f"### {month_dir}")
            for obs_type in sorted(by_year[year][month_dir].keys()):
                entries = by_year[year][month_dir][obs_type]
                lines.append(f"#### {obs_type} ({len(entries)})")
                for link in entries:
                    lines.append(f"- {link}")
            lines.append("")

    proj_dir = base_dir / project
    proj_dir.mkdir(parents=True, exist_ok=True)
    (proj_dir / f"{project}.md").write_text("\n".join(lines), encoding="utf-8")


def write_index(data: dict, base_dir: Path, total: int) -> None:
    """Write root _index.md with stats, timestamp, and per-project wikilinks."""
    from collections import defaultdict

    obs_list = data.get("observations", [])
    by_project: dict[str, list] = defaultdict(list)
    for o in obs_list:
        p = sanitize(o.get("project") or "unknown")
        by_project[p].append(o)

    now_str = datetime.now().strftime("%Y-%m-%d %H:%M:%S")

    lines = [
        "---",
        "tags: [engram, index]",
        f"synced: {now_str}",
        "---",
        "",
        "# Engram Memory Index",
        "",
        f"> Synced: **{now_str}** — **{total}** observations across **{len(by_project)}** projects",
        "",
        "## Projects",
        "",
    ]

    for proj in sorted(by_project.keys()):
        proj_obs = by_project[proj]
        total_proj = len(proj_obs)
        lines.append(f"- [[{proj}/{proj}|{proj}]] ({total_proj} memorias)")

    lines.append("")

    (base_dir / "_index.md").write_text("\n".join(lines), encoding="utf-8")

    # Write per-project _index.md files and year/month indexes
    # Escribir los archivos _index.md por proyecto y los índices de año/mes
    from collections import defaultdict
    for proj, proj_obs in by_project.items():
        try:
            write_project_index(proj, proj_obs, base_dir)
        except Exception as exc:
            log(f"WARN could not write {proj}/{proj}.md: {exc}")

        # Build year→month→type structure for sub-indexes
        by_year: dict = defaultdict(lambda: defaultdict(lambda: defaultdict(list)))
        for o in proj_obs:
            obs_type  = sanitize(o.get("type") or "unknown")
            title     = o.get("title", "untitled")
            safe_title = sanitize(title)
            created   = (o.get("created_at") or "")[:10]
            filename  = f"{created} {safe_title}" if created else safe_title
            if created:
                year = created[:4]
                month_num = int(created[5:7])
                month_dir = f"{created[5:7]}-{MONTH_NAMES[month_num - 1]}"
            else:
                year = "sin-fecha"
                month_dir = "sin-fecha"
            wikilink = f"[[{proj}/{year}/{month_dir}/{obs_type}/{filename}|{title}]]"
            by_year[year][month_dir][obs_type].append(wikilink)

        for year, months_data in by_year.items():
            try:
                write_year_index(proj, year, months_data, base_dir)
            except Exception as exc:
                log(f"WARN could not write {proj}/{year}/{year}.md: {exc}")
            for month_dir, types_data in months_data.items():
                try:
                    write_month_index(proj, year, month_dir, types_data, base_dir)
                except Exception as exc:
                    log(f"WARN could not write {proj}/{year}/{month_dir}/{month_dir}.md: {exc}")


def sync_to_vault() -> None:
    """Fetch Engram data and write all files under ENGRAM_DIR."""
    log("Syncing Engram — Obsidian...")
    data = fetch_engram()
    if data is None:
        log("Sync aborted: could not reach Engram API")
        return

    observations = data.get("observations", [])
    total = len(observations)
    log(f"Fetched {total} observations")

    # Wipe and recreate _engram/ for a clean sync
    # Borrar y recrear _engram/ para una sincronización limpia
    if ENGRAM_DIR.exists():
        shutil.rmtree(ENGRAM_DIR)
    ENGRAM_DIR.mkdir(parents=True, exist_ok=True)

    for obs in observations:
        try:
            write_observation(obs, ENGRAM_DIR)
        except Exception as exc:
            log(f"WARN skipping obs id={obs.get('id')}: {exc}")

    try:
        write_index(data, ENGRAM_DIR, total)
    except Exception as exc:
        log(f"WARN could not write _index.md: {exc}")

    log(f"Sync complete — {total} notes written to {ENGRAM_DIR}")


def cleanup_vault() -> None:
    """Remove ONLY the _engram/ directory from the vault."""
    if ENGRAM_DIR.exists():
        shutil.rmtree(ENGRAM_DIR)
        log(f"Cleaned up {ENGRAM_DIR}")
    else:
        log("Nothing to clean up (_engram/ not present)")


# ── Main loop ─────────────────────────────────────────────────────────────────

def main() -> None:
    log("engram-obsidian daemon starting")
    log(f"Vault: {VAULT_DIR}")
    log(f"Poll interval: {POLL_INTERVAL}s")

    # Bootstrap: detect current state without triggering a transition
    # Bootstrap: detectar el estado actual sin disparar una transición
    was_running = should_sync()
    if was_running:
        log("Obsidian running + root session active — initial sync")
        sync_to_vault()
    else:
        log("Conditions not met — standby")

    while True:
        time.sleep(POLL_INTERVAL)
        is_running = should_sync()

        if not was_running and is_running:
            log("Conditions MET (Obsidian + root) — syncing")
            sync_to_vault()

        elif was_running and not is_running:
            log("Conditions no longer met — cleaning up")
            cleanup_vault()

        was_running = is_running


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        log("Interrupted — exiting")
PYEOF

chmod +x "$DAEMON_DEST"
ok "Daemon installed: $DAEMON_DEST"

# ── 5. Install systemd user service ──────────────────────────────────────────
header "==> Installing systemd user service"

SYSTEMD_DIR="$HOME/.config/systemd/user"
SERVICE_FILE="$SYSTEMD_DIR/engram-obsidian.service"
mkdir -p "$SYSTEMD_DIR"

PYTHON_BIN=$(command -v python3 || echo "/usr/bin/python3")

cat > "$SERVICE_FILE" <<SVCEOF
[Unit]
Description=Engram → Obsidian sync daemon
After=network.target

[Service]
Type=simple
ExecStart=${DAEMON_DEST}
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=default.target
SVCEOF

ok "Service file written: $SERVICE_FILE"

# ── 6. Enable and start the service ───────────────────────────────────────────
header "==> Enabling and starting service"

if systemctl --user is-active --quiet engram-obsidian.service 2>/dev/null; then
    info "Service already running — restarting to pick up changes"
    systemctl --user restart engram-obsidian.service
    ok "Service restarted"
else
    systemctl --user daemon-reload
    systemctl --user enable engram-obsidian.service
    systemctl --user start engram-obsidian.service
    ok "Service enabled and started"
fi

# ── 7. Final summary ──────────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}${GREEN}Installation complete!${NC}"
echo ""
echo -e "  Daemon:   ${CYAN}$DAEMON_DEST${NC}"
echo -e "  Service:  ${CYAN}$SERVICE_FILE${NC}"
echo -e "  Vault:    ${CYAN}$VAULT_PATH${NC}"
echo ""
echo -e "${BOLD}${YELLOW}Abri este vault en Obsidian:${NC}"
echo -e "  ${BOLD}${WIN_VAULT_PATH}${NC}"
echo ""
echo -e "Para ver logs del daemon:"
echo -e "  journalctl --user -u engram-obsidian.service -f"
echo ""
echo -e "${YELLOW}Prerequisito manual:${NC}"
echo -e "  Obsidian instalado en Windows (el script no puede verificarlo)."
echo -e "  Descargalo desde: ${CYAN}https://obsidian.md/download${NC}"
echo ""
