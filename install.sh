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
    GOBIN="$HOME/.local/bin" go install github.com/Antonio-Escajeda/engram-obsidian/cmd/engram-obsidian@main
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

# 5. Migrar db_path a notación ~/... si tiene ruta absoluta del home
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

# 6. Reload del daemon
echo "-> Recargando systemd daemon..."
systemctl --user daemon-reload

# 7. Habilitar/reiniciar segun estado actual
if systemctl --user is-active --quiet engram-obsidian; then
    echo "-> Servicio activo — reiniciando..."
    systemctl --user restart engram-obsidian
else
    echo "-> Habilitando e iniciando servicio..."
    systemctl --user enable engram-obsidian
    systemctl --user start engram-obsidian
fi

# 8. Estado final
echo ""
echo "-> Estado del servicio:"
systemctl --user status engram-obsidian --no-pager

# 9. Aviso de primera instalacion
echo ""
if [[ "$FIRST_INSTALL" == true ]]; then
    echo "Primera instalacion detectada."
    echo "Corré 'engram-obsidian --select' para configurar el vault y la seleccion de proyectos."
else
    echo "Actualizacion completada."
fi
