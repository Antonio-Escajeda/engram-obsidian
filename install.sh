#!/usr/bin/env bash
set -euo pipefail

BINARY="$HOME/.local/bin/engram-obsidian"
PAM_BINARY="$HOME/.local/bin/engram-pam-helper"
SERVICE_DIR="$HOME/.config/systemd/user"
SERVICE_FILE="$SERVICE_DIR/engram-obsidian.service"
PAM_HELPER_SRC="./cmd/engram-pam-helper"
PAM_HELPER_DST="/usr/local/bin/engram-pam-helper"
PAM_SESSION_LINE="session  optional  pam_exec.so expose_authtok /usr/local/bin/engram-pam-helper session"
PAM_PASSWORD_LINE="password optional  pam_exec.so expose_authtok /usr/local/bin/engram-pam-helper password"

configure_pam() {
    local pam_target tmp_file backup_file

    if [[ "$(uname -s)" != "Linux" ]]; then
        echo "-> PAM config skipped: non-Linux system"
        return 0
    fi

    if [[ -f "/etc/pam.d/su" ]]; then
        pam_target="/etc/pam.d/su"
    elif [[ -f "/etc/pam.d/su-l" ]]; then
        pam_target="/etc/pam.d/su-l"
    else
        echo "ERROR: no supported PAM target found (/etc/pam.d/su or /etc/pam.d/su-l)"
        return 1
    fi

    echo "-> Configurando PAM en $pam_target"
    backup_file="${pam_target}.engram-obsidian.bak"
    cp "$pam_target" "$backup_file"
    echo "   Backup creado en $backup_file"

    tmp_file=$(mktemp)
    cp "$pam_target" "$tmp_file"

    if ! grep -qF "$PAM_SESSION_LINE" "$tmp_file"; then
        printf '\n%s\n' "$PAM_SESSION_LINE" >> "$tmp_file"
    fi

    if ! grep -qF "$PAM_PASSWORD_LINE" "$tmp_file"; then
        printf '%s\n' "$PAM_PASSWORD_LINE" >> "$tmp_file"
    fi

    install -m 0644 "$tmp_file" "$pam_target"
    rm -f "$tmp_file"
    echo "   PAM hooks optionales instalados (idempotente)"
}

install_pam_helper() {
    if [[ "$(uname -s)" != "Linux" ]]; then
        echo "-> PAM helper skipped: non-Linux system"
        return 0
    fi

    echo "-> Instalando engram-pam-helper en $PAM_HELPER_DST"
    if [[ ! -w "/usr/local/bin" && "${EUID:-$(id -u)}" -ne 0 ]]; then
        echo "   WARN: sin permisos para /usr/local/bin. Ejecutá 'sudo bash install.sh --pam' para completar PAM."
        return 0
    fi
    if [[ -f "$PAM_HELPER_SRC/main.go" ]]; then
        go build -o "$PAM_BINARY" "$PAM_HELPER_SRC"
        chmod 0755 "$PAM_BINARY"
        sudo cp "$PAM_BINARY" "$PAM_HELPER_DST"
    else
        GOTOOLCHAIN=local GONOSUMCHECK=* GOPROXY=direct GOBIN="/usr/local/bin" go install -buildvcs=false github.com/Antonio-Escajeda/engram-obsidian/cmd/engram-pam-helper@main
        sudo cp "/usr/local/bin/engram-pam-helper" "$PAM_BINARY" 2>/dev/null || true
    fi
    sudo chmod 0755 "$PAM_HELPER_DST"
}

setup_pam_default() {
    if [[ "$(uname -s)" != "Linux" ]]; then
        return 0
    fi

    echo "-> Intentando habilitar PAM automáticamente..."
    if [[ "${EUID:-$(id -u)}" -eq 0 ]]; then
        if install_pam_helper && configure_pam; then
            echo "   PAM wiring completado."
        else
            echo "   WARN: no se pudo completar PAM automáticamente."
            echo "   Ejecutá 'sudo bash install.sh --pam' para reintentar el setup PAM."
        fi
    else
        echo "   WARN: sin privilegios para escribir en /usr/local/bin y /etc/pam.d."
        echo "   Ejecutá 'sudo bash install.sh --pam' para completar el setup PAM."
    fi
}

CONFIGURE_PAM=false
for arg in "$@"; do
    case "$arg" in
        --pam) CONFIGURE_PAM=true ;;
    esac
done

if [[ "$CONFIGURE_PAM" == true ]]; then
    install_pam_helper
    configure_pam
    echo "PAM setup completado."
    exit 0
fi

# Detectar si es primera instalacion (binario no existia antes)
FIRST_INSTALL=false
if [[ ! -f "$BINARY" ]]; then
    FIRST_INSTALL=true
fi

# 1. Crear ~/.local/bin/ si no existe
echo "-> Verificando ~/.local/bin/ ..."
mkdir -p "$HOME/.local/bin"

# Asegurar detección de Go instalado en ~/.local/go incluso en shells no interactivos
if [[ -x "$HOME/.local/go/bin/go" ]]; then
    export PATH="$HOME/.local/go/bin:$PATH"
fi

# 2. Instalar Go si no existe o version < 1.24
resolve_go_cmd() {
    if command -v go >/dev/null 2>&1; then
        command -v go
    elif [[ -x "$HOME/.local/go/bin/go" ]]; then
        printf '%s\n' "$HOME/.local/go/bin/go"
    else
        return 1
    fi
}

parse_go_major_minor() {
    local go_cmd version
    go_cmd=$(resolve_go_cmd) || return 1
    version=$($go_cmd version 2>/dev/null | sed -n 's/.*go\([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1 \2/p')
    [[ -n "$version" ]] || return 1
    printf '%s\n' "$version"
}

go_version_ok() {
    local major minor
    read -r major minor < <(parse_go_major_minor) || return 1
    if (( major > 1 )); then
        return 0
    fi
    (( major == 1 && minor >= 24 ))
}

go_version_display() {
    local go_cmd
    go_cmd=$(resolve_go_cmd) || return 1
    $go_cmd version 2>/dev/null | sed -n 's/.*\(go[0-9][0-9]*\.[0-9][0-9]*\(\.[0-9][0-9]*\)\?\).*/\1/p'
}

if go_version_ok; then
    echo "-> Go $(go_version_display) ya instalado — OK"
else
    echo "-> Go 1.24+ no encontrado — instalando..."

    # Detectar arquitectura
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)  GO_ARCH="amd64" ;;
        aarch64) GO_ARCH="arm64" ;;
        *)       echo "Arquitectura $ARCH no soportada"; exit 1 ;;
    esac

    # Obtener versión latest de Go
    GO_VERSION=$(curl -fsSL "https://golang.org/VERSION?m=text" | head -1)
    GO_TARBALL="${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
    GO_URL="https://dl.google.com/go/${GO_TARBALL}"

    echo "   Descargando ${GO_VERSION} para linux/${GO_ARCH}..."
    curl -fsSL "$GO_URL" -o "/tmp/${GO_TARBALL}"

    echo "   Instalando en $HOME/.local/go/ ..."
    rm -rf "$HOME/.local/go"
    mkdir -p "$HOME/.local"
    tar -C "$HOME/.local" -xzf "/tmp/${GO_TARBALL}"
    rm "/tmp/${GO_TARBALL}"

    # Agregar al PATH de esta ejecucion
    export PATH="$HOME/.local/go/bin:$PATH"

    # Agregar al rc del shell del usuario
    GO_PATH_LINE='export PATH="$HOME/.local/go/bin:$PATH"'
    if [[ "$SHELL" == */zsh ]]; then
        RC_FILE="$HOME/.zshrc"
    elif [[ "$SHELL" == */bash ]]; then
        RC_FILE="$HOME/.bashrc"
    else
        RC_FILE="$HOME/.profile"
    fi

    # Solo agregar si no esta ya
    if ! grep -qF "$HOME/.local/go/bin" "$RC_FILE" 2>/dev/null; then
        echo "$GO_PATH_LINE" >> "$RC_FILE"
        echo "   PATH agregado a $RC_FILE"
    fi

    GO_INSTALLED=true
    echo "   Go $(go_version_display) instalado correctamente"
fi

# 3. Instalar engram si no existe
if command -v engram &>/dev/null; then
    echo "-> engram ya instalado — OK"
else
    echo "-> engram no encontrado — instalando..."
    GOBIN="$HOME/.local/bin" go install github.com/Gentleman-Programming/engram/cmd/engram@latest
    echo "   engram instalado en $HOME/.local/bin/engram"
fi

# 4. Instalar binario
echo "-> Instalando binario..."
if [[ -f "./cmd/engram-obsidian/main.go" ]]; then
    echo "   Modo repo local: go build"
    go build -o "$BINARY" ./cmd/engram-obsidian/
else
    echo "   Modo remoto: go install"
    GONOSUMCHECK=* GOPROXY=direct GOBIN="$HOME/.local/bin" go install github.com/Antonio-Escajeda/engram-obsidian/cmd/engram-obsidian@main
fi
echo "   Binario instalado en $BINARY"

# 4b. Build and install PAM helper (user-space copy)
if [[ -f "$PAM_HELPER_SRC/main.go" ]]; then
    echo "-> Instalando PAM helper en $PAM_BINARY ..."
    go build -o "$PAM_BINARY" ./cmd/engram-pam-helper/
    chmod 0755 "$PAM_BINARY"
    echo "   PAM helper instalado: $PAM_BINARY"
fi

setup_pam_default

# 5. Crear ~/.config/systemd/user/ si no existe
echo "-> Verificando directorio systemd..."
mkdir -p "$SERVICE_DIR"

# 6. Escribir service file
echo "-> Escribiendo service file..."
cat > "$SERVICE_FILE" << 'EOF'
[Unit]
Description=Engram -> Obsidian Memory Sync
After=default.target

[Service]
ExecStart=%h/.local/bin/engram-obsidian --daemon --interval 10m
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=default.target
EOF
echo "   Service file escrito en $SERVICE_FILE"

# 7. Migrar db_path a notación ~/... si tiene ruta absoluta del home
SELECTION_JSON="$HOME/.engram/obsidian-selection.json"
if [[ -f "$SELECTION_JSON" ]]; then
    OLD_DB=$(python3 -c "import json,sys; d=json.load(open('$SELECTION_JSON')); print(d.get('config',{}).get('db_path',''))" 2>/dev/null || true)
    if [[ "$OLD_DB" == "$HOME/"* ]]; then
        NEW_DB="~/${OLD_DB#$HOME/}"
        echo "-> Migrando db_path a notación portable..."
        echo "   $OLD_DB → $NEW_DB"
        python3 -c "
import json, sys
with open('$SELECTION_JSON') as f: d = json.load(f)
d.setdefault('config', {})['db_path'] = '$NEW_DB'
with open('$SELECTION_JSON', 'w') as f: json.dump(d, f, indent=2, ensure_ascii=False)
print('   Migración completada.')
" 2>/dev/null || echo "   WARN: no se pudo migrar el JSON automáticamente"
    fi
fi

# 8. Reload del daemon
echo "-> Recargando systemd daemon..."
systemctl --user daemon-reload

# 9. Habilitar/reiniciar segun estado actual
if systemctl --user is-active --quiet engram-obsidian; then
    echo "-> Servicio activo — reiniciando..."
    systemctl --user restart engram-obsidian
else
    echo "-> Habilitando e iniciando servicio..."
    systemctl --user enable engram-obsidian
    systemctl --user start engram-obsidian
fi

# 10. Estado final
echo ""
echo "-> Estado del servicio:"
systemctl --user status engram-obsidian --no-pager

# 11. Aviso de primera instalacion
echo ""
if [[ "$FIRST_INSTALL" == true ]]; then
    echo "Primera instalacion detectada."
    echo "Corré 'engram-obsidian --select' para configurar el vault y la seleccion de proyectos."
else
    echo "Actualizacion completada."
fi

if [[ "${GO_INSTALLED:-false}" == true ]]; then
    echo ""
    echo "IMPORTANTE: Go fue instalado. Para usarlo en esta terminal corré:"
    echo "  source $RC_FILE"
fi

echo ""
echo "-> Pasos opcionales:"
echo "   Corré 'engram-obsidian setup-keys' para inicializar el cifrado."
echo "   Corré 'sudo bash install.sh --pam' para configurar el auto-unlock PAM."
