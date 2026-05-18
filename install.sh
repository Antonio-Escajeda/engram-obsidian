#!/usr/bin/env bash
set -euo pipefail

BINARY="$HOME/.local/bin/engram-obsidian"
SERVICE_DIR="$HOME/.config/systemd/user"
SERVICE_FILE="$SERVICE_DIR/engram-obsidian.service"

# Detectar si es primera instalacion (binario no existia antes)
FIRST_INSTALL=false
if [[ ! -f "$BINARY" ]]; then
    FIRST_INSTALL=true
fi

# 1. Crear ~/.local/bin/ si no existe
echo "-> Verificando ~/.local/bin/ ..."
mkdir -p "$HOME/.local/bin"

# 2. Instalar binario
echo "-> Instalando binario..."
if [[ -f "./cmd/engram-obsidian/main.go" ]]; then
    echo "   Modo repo local: go build"
    go build -o "$BINARY" ./cmd/engram-obsidian/
else
    echo "   Modo remoto: go install"
    GOBIN="$HOME/.local/bin" go install github.com/Antonio-Escajeda/engram-obsidian/cmd/engram-obsidian@latest
fi
echo "   Binario instalado en $BINARY"

# 3. Crear ~/.config/systemd/user/ si no existe
echo "-> Verificando directorio systemd..."
mkdir -p "$SERVICE_DIR"

# 4. Escribir service file
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

# 5. Reload del daemon
echo "-> Recargando systemd daemon..."
systemctl --user daemon-reload

# 6. Habilitar/reiniciar segun estado actual
if systemctl --user is-active --quiet engram-obsidian; then
    echo "-> Servicio activo — reiniciando..."
    systemctl --user restart engram-obsidian
else
    echo "-> Habilitando e iniciando servicio..."
    systemctl --user enable engram-obsidian
    systemctl --user start engram-obsidian
fi

# 7. Estado final
echo ""
echo "-> Estado del servicio:"
systemctl --user status engram-obsidian --no-pager

# 8. Aviso de primera instalacion
echo ""
if [[ "$FIRST_INSTALL" == true ]]; then
    echo "Primera instalacion detectada."
    echo "Corré 'engram-obsidian --select' para configurar el vault y la seleccion de proyectos."
else
    echo "Actualizacion completada."
fi
