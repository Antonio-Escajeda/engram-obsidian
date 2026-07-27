# engram-obsidian

Daemon que sincroniza las memorias de [Engram](https://github.com/Gentleman-Programming/engram) hacia un vault de Obsidian de forma efímera y segura.

## Concepto

El daemon puede preparar/crear el vault desde el bootstrap, pero **solo lo puebla** cuando Obsidian está abierto **y** hay una sesión root activa.

En una instalación nueva, el vault se crea vacío y no se sincroniza nada hasta que el usuario ejecute `engram-obsidian --select` y confirme la selección.

Al cerrar cualquiera de las dos condiciones (Obsidian o sesión root), los archivos se eliminan automáticamente. Las memorias viven en Engram — Obsidian es solo una vista temporal.

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

### Sin clonar el repo (recomendado para usuarios nuevos)

```bash
curl -fsSL https://raw.githubusercontent.com/Antonio-Escajeda/engram-obsidian/main/install.sh | bash
```

> Este comando instala/actualiza el daemon en modo usuario sin requerir `sudo`.
> Para completar la integración PAM del sistema, ejecuta luego:
>
> ```bash
> sudo bash install.sh --pam
> ```

### Desde el repo local

```bash
git clone https://github.com/Antonio-Escajeda/engram-obsidian.git
cd engram-obsidian
./install.sh
```

El script es idempotente — funciona tanto para instalación nueva como para actualización:
- Crea `~/.local/bin/` si no existe
- Si está en el repo local, compila con `go build`; si no, instala con `go install` remoto
- En Linux/WSL intenta habilitar PAM automáticamente durante la instalación
- Escribe el service file de systemd en `~/.config/systemd/user/`
- Fija `ENGRAM_DATA_DIR=%h/.engram` en el servicio para resolución portable de `db_path`
- Habilita e inicia el servicio (o lo reinicia si ya estaba activo)
- Al terminar muestra el estado del servicio

Si es la primera vez, el script te recuerda correr `engram-obsidian --select` para configurar y confirmar selección.

> **Primera instalación:** el directorio del vault se crea vacío para que puedas abrirlo/seleccionarlo en Obsidian inmediatamente. La primera sincronización ocurre recién después de `--select`.

### Para actualizar

Desde el directorio del repo:

```bash
git pull
./install.sh
```

El script detecta que el servicio ya está activo y lo reinicia automáticamente.

### Configuración PAM (automática por default en Linux/WSL)

`install.sh` ahora intenta completar el wiring PAM en el flujo normal:
- Si corrés con privilegios (`root`/`sudo`), instala `engram-pam-helper` y configura `/etc/pam.d/*` en el momento.
- Si corrés sin privilegios, la instalación principal sigue sin fallar y el script te indica correr `sudo bash install.sh --pam` para terminar PAM.

Para habilitar desbloqueo automático del keyring al usar `su`/`sudo`, corré:

```bash
sudo bash install.sh --pam
```

El modo `--pam` se mantiene para forzar/reintentar la configuración:
- Instala `engram-pam-helper` en `/usr/local/bin/engram-pam-helper`
- Detecta el archivo PAM del sistema (`/etc/pam.d/su` o `/etc/pam.d/su-l`)
- Inserta hooks `pam_exec` como `optional` de forma idempotente (sin duplicar líneas)

> Si corrés `install.sh` sin privilegios, el script continúa la instalación normal y te indica ejecutar `sudo bash install.sh --pam` para completar el wiring PAM.

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
- **Vault path**: se pre-rellena automáticamente con la carpeta `Documents` del usuario Windows actual (detección robusta en WSL usando `wslvar`/`wslpath`, `cmd.exe`, `USERPROFILE` y fallback por `/mnt/c/Users`). Podés confirmarlo o cambiarlo; también podés presionar `b` para abrir el selector de carpetas de Windows
- **DB path**: default `~/.engram/engram.db`. Se aceptan rutas absolutas (se mantienen tal cual) y también rutas relativas/`./...` que se resuelven dentro de `ENGRAM_DATA_DIR` (por default `~/.engram`)
- **Graph mode**: `● Star` / `○ Full Mesh` — navegá con `← →` o `Space` para cambiar
- `Tab` para navegar entre campos · `Enter` para continuar a la selección

> **Nota:** la detección automática del vault path solo ocurre en el primer uso. Si ya existe config guardada en `~/.engram/obsidian-selection.json`, se respeta sin sobreescribir.

### Selección de proyectos

La TUI muestra un árbol `Proyecto → Mes → Nota`. Usá `Space` para activar/desactivar, `s` para confirmar y sincronizar.

La selección se guarda en `~/.engram/obsidian-selection.json` y el daemon la usa en cada ciclo.

Compatibilidad de `db_path`:
- Si `db_path` está en absoluto (`/mnt/c/...`, `/home/...`) no se modifica.
- Si `db_path` viene en legacy absoluto bajo `$HOME`, `install.sh` lo migra a notación `~/...` para que sea portable entre usuarios/máquinas.
- Si `db_path` es relativo, el daemon lo resuelve contra `ENGRAM_DATA_DIR` (o `~/.engram` si no está definido).

### Graph Mode

Controla cómo se generan los links entre notas para el Obsidian graph view.

| Modo | Comportamiento |
|---|---|
| **Star** (default) | Cada tipo tiene un archivo hub (`📋 bugfix.md`, `📋 architecture.md`, `📋 database.md`, etc.). Todas las notas del mismo tipo apuntan al hub → topología estrella por color. |
| **Full Mesh** | Además de los hubs, cada nota linkea directamente a todas las demás del mismo tipo en el mismo proyecto → clique completo por color. |

Se configura en la pantalla de configuración (`--select`) con el campo **Graph mode**.

### Encrypt DB

Cifra `~/.engram/engram.db` en reposo usando AES-256-GCM. La clave se gestiona automáticamente via Linux Keyring (session-bound) con fallback a key file cifrada.

| Estado | Comportamiento |
|---|---|
| **Sesión activa** | `engram.db` existe en plaintext — engram MCP funciona normalmente |
| **Sesión terminada** | Solo existe `engram.db.enc` — inaccesible sin el daemon |

Se configura en la pantalla de configuración (`--select`) con el campo **Encrypt DB** (`space` para activar/desactivar). Desactivado por default.

> **Nota**: si el daemon se detiene con `engram.db.enc` en disco, la DB queda cifrada hasta que el daemon vuelva a correr con sesión activa.

### Vault Lock

Protege el contenido generado en `_engram` contra modificaciones/borrados accidentales después de cada sync.

> **Estado actual:** la opción quedó temporalmente deshabilitada en la pantalla `--select` mientras se corrige el comportamiento de lock en Windows. Por ahora el flujo usa `disabled`.

| Modo | Comportamiento |
|---|---|
| **Disabled** (default) | No aplica lock extra al vault generado. |
| **Strict** | Antes de cleanup/sync intenta unlock; después de sync aplica readonly y luego intenta lock avanzado en Windows. |

Detalles de implementación (Windows-first):
- Base siempre disponible: permisos readonly en archivos/directorios de `_engram`.
- Intentos avanzados (best effort): `attrib`/`icacls` vía `cmd.exe` desde WSL interop cuando la ruta está en `/mnt/<drive>/...`.
- Si los pasos avanzados fallan o no están disponibles, el daemon loguea warning claro y sigue con readonly best effort (no rompe el loop).

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
| `database` | cyan |
| `preference` | rosa |
| `session_summary` | teal |

### Regla de uso recomendada para cambios de base de datos

- Usá `type: database` para migraciones, cambios de esquema, índices, tuning SQL o decisiones de modelado de datos.
- Para mantener consistencia de lectura en Obsidian, titulá notas con el formato: `YYYY-MM-DD [database] - descripción corta`.

## Logs

```bash
journalctl --user -u engram-obsidian.service -f
```

## Validación rápida post-instalación

Podés ejecutar una validación semiautomática del estado del servicio, selección, ruta de vault y eventos clave de sync/cleanup:

```bash
./scripts/validate-install.sh
```
