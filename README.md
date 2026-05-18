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

Cloná el repo y corré el script de instalación:

```bash
git clone https://github.com/Antonio-Escajeda/engram-obsidian.git
cd engram-obsidian
./install.sh
```

El script:
- Crea `~/.local/bin/` si no existe
- Compila el binario desde el repo local y lo instala en `~/.local/bin/engram-obsidian`
- Escribe el service file de systemd en `~/.config/systemd/user/`
- Habilita e inicia el servicio automáticamente
- Al terminar muestra el estado del servicio

Si es la primera vez, el script te recuerda correr `engram-obsidian --select` para configurar el vault.

### Para actualizar

Desde el directorio del repo:

```bash
git pull
./install.sh
```

El script detecta que el servicio ya está activo y lo reinicia automáticamente.

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
