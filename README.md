# engram-obsidian

Daemon que sincroniza las memorias de [Engram](https://github.com/Gentleman-Programming/engram) hacia un vault de Obsidian de forma efímera y segura.

## Concepto

El vault se crea cuando Obsidian está abierto **y** hay una sesión root activa. Al cerrar cualquiera de las dos, los archivos se eliminan automáticamente. Las memorias viven en Engram — Obsidian es solo una vista temporal.

## Estructura del proyecto

```
engram-obsidian/
├── cmd/engram-obsidian/
│   └── main.go              flags, señales SIGINT/SIGTERM, arranca daemon
├── internal/
│   ├── store/
│   │   ├── types.go         Observation struct + helpers
│   │   └── reader.go        lectura SQLite read-only de engram.db
│   └── obsidian/
│       ├── slug.go          texto → filename seguro
│       ├── state.go         SyncState: qué archivos fueron escritos
│       ├── graph.go         genera .obsidian/graph.json efímero con colores por tipo
│       ├── markdown.go      Observation → frontmatter YAML + .md
│       ├── exporter.go      orquesta el sync, genera jerarquía de índices
│       ├── selection.go     selección persistente en JSON, Filter(obs) bool
│       ├── tui/
│       │   ├── model.go     ScreenConfig (vault/db path) | ScreenSelection (árbol)
│       │   ├── tree.go      BuildTree, Toggle, CheckState (None/Partial/Full)
│       │   ├── update.go    j/k navegar · space toggle · s confirmar · q volver
│       │   ├── view.go      lipgloss: config form + árbol con cursor full-width
│       │   └── browse.go    selector de carpetas Windows vía PowerShell
│       └── daemon/
│           ├── process.go   ObsidianRunning (tasklist.exe) · RootSessionActive (/proc)
│           └── daemon.go    loop: poll → sync → cleanup
└── .gitignore
```

## Requisitos

- WSL2 con Go 1.22+
- [Engram](https://github.com/Gentleman-Programming/engram) corriendo (`~/.engram/engram.db`)
- Obsidian instalado en Windows
- Vault en una ruta Windows nativa (`C:\...`), accesible desde WSL como `/mnt/c/...`

> **Importante:** Obsidian no soporta vaults en el filesystem de WSL (`\\wsl$\`). El vault debe estar en NTFS.

## Instalación

```bash
GOBIN=~/.local/bin go install github.com/Antonio-Escajeda/engram-obsidian/cmd/engram-obsidian@latest
```

Creá el service file de systemd:

```bash
cat > ~/.config/systemd/user/engram-obsidian.service << 'EOF'
[Unit]
Description=Engram → Obsidian Memory Sync
After=default.target

[Service]
ExecStart=%h/.local/bin/engram-obsidian --daemon --interval 10m
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=default.target
EOF
```

Habilitá y arrancá el servicio:

```bash
systemctl --user daemon-reload
systemctl --user enable engram-obsidian
systemctl --user start engram-obsidian
```

### Para actualizar

```bash
GOBIN=~/.local/bin go install github.com/Antonio-Escajeda/engram-obsidian/cmd/engram-obsidian@latest
systemctl --user restart engram-obsidian
```

## Uso

| Comando | Comportamiento |
|---|---|
| `engram-obsidian --select` | Abre TUI para cambiar selección y sincronizar (one-shot) |
| `engram-obsidian --daemon` | Sin TUI, usa selección guardada — modo systemd |
| `engram-obsidian --interval 10m` | Intervalo de re-sync periódico |
| `engram-obsidian --poll 2.5s` | Frecuencia de detección de Obsidian |

### Primera configuración

```bash
engram-obsidian --select
```

En la pantalla de configuración:
- **Vault path**: presioná `b` para abrir el selector de carpetas de Windows
- **DB path**: path a `~/.engram/engram.db`
- `Tab` para navegar entre campos · `Enter` para continuar a la selección

### Selección de proyectos

La TUI muestra un árbol `Proyecto → Mes → Nota`. Usá `Space` para activar/desactivar, `s` para confirmar y sincronizar.

La selección se guarda en `~/.engram/obsidian-selection.json` y el daemon la usa en cada ciclo.

## Estructura del vault

```
EngramVault/
└── _engram/
    ├── _index.md                    índice raíz con todos los proyectos
    ├── {proyecto}/
    │   ├── {proyecto}.md            índice del proyecto
    │   └── {año}/
    │       ├── {año}.md             índice del año
    │       └── {mes}/
    │           ├── {mes}.md         índice del mes con lista de notas
    │           └── {tipo}/
    │               └── nota.md      memoria individual
    └── .obsidian/
        └── graph.json               colores por tipo (efímero)
```

Cada nota linkea solo a su mes. El mes linkea al año, el año al proyecto — jerarquía estricta sin saltos de nivel.

### Colores en el graph view

| Tipo | Color |
|---|---|
| `architecture` | azul |
| `bugfix` | rojo |
| `decision` | verde |
| `pattern` | naranja |
| `discovery` | violeta |
| `config` | amarillo |
| `preference` | rosa |
| `session_summary` | teal |

## Logs

```bash
journalctl --user -u engram-obsidian.service -f
```
