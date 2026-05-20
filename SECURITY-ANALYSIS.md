# Security Analysis: Encrypt-at-Rest para engram.db

**Proyecto:** engram-obsidian  
**Fecha:** 2026-05-20  
**Estado:** Propuesta de diseño — pre-implementación

---

> **Estado**: ✅ Implementado — 2026-05-20
> **Known gaps v1**: `encryptDB()` no se llama en `kill -TERM` mid-session (deferred v2). Ver sección 6.

## 1. Threat Model

### Qué se protege

`~/.engram/engram.db` — base SQLite con FTS5 que contiene **todas las memorias del usuario**: decisiones de arquitectura, bugs encontrados, convenciones de código, contexto de proyectos. Datos sensibles por naturaleza (puede contener tokens, paths, lógica de negocio propietaria).

### Contra qué atacante

| Vector | Descripción | Cubierto |
|--------|-------------|----------|
| Acceso físico al disco cuando el usuario no está logueado | Volcado de disco, cold boot, extravío de laptop | ✅ |
| Proceso malicioso leyendo archivos mientras el usuario no está activo | Malware, cron jobs, otro usuario en sistema multi-user | ✅ |
| Snapshot/backup de disco sin permiso del usuario | Backup automático de cloud que incluye `~/.engram/` | ✅ |
| Atacante con sesión activa del mismo usuario | Root o proceso con mismo UID mientras hay sesión | ❌ (fuera de scope — si tienen tu sesión, ya ganaron) |
| Análisis forense post-acceso | Recuperar datos de disco tras brecha física | ✅ |

### Precondiciones del modelo

- La **clave de cifrado existe únicamente en memoria kernel** (Linux Keyring) mientras hay sesión activa
- El daemon `engram-obsidian` es el único gestor del ciclo encrypt/decrypt
- El binario `engram` (externo) escribe en plaintext durante sesión activa — eso es correcto y esperado
- WSL2 es un entorno de primera clase en este proyecto (confirmado por commits previos)

---

## 2. Arquitectura propuesta: Encrypt-at-Rest con sesión bound

### Ciclo de vida completo

```
ROOT SESSION STARTS
        │
        ▼
daemon detecta: ShouldSync() → true
        │
        ▼
decryptDB():
  1. Leer ~/.engram/engram.db.enc
  2. Obtener clave de Linux Keyring (o generar + guardar si primera vez)
  3. AES-256-GCM decrypt → archivo temporal ~/.engram/.engram.db.tmp
  4. atomic rename: .tmp → engram.db
  5. Borrar engram.db.enc
        │
        ▼
[sesión activa — engram MCP escribe libremente en engram.db (plaintext)]
[daemon sincroniza a Obsidian periódicamente]
        │
ROOT SESSION ENDS
        ▼
daemon detecta: ShouldSync() → false, cleanupCountdown >= cleanupConfirmPolls
        │
        ▼
encryptDB():
  1. PRAGMA wal_checkpoint(TRUNCATE) — fuerza commit WAL
  2. Cerrar reader de store si está abierto
  3. Leer engram.db completo en memoria (o streaming)
  4. AES-256-GCM encrypt → ~/.engram/.engram.db.enc.tmp
  5. atomic rename: .tmp → engram.db.enc
  6. Borrar engram.db, engram.db-wal, engram.db-shm
        │
        ▼
[solo existe engram.db.enc — ilegible sin clave]
```

### Estado en repo: en qué momento existe cada archivo

| Estado del sistema | Archivos en `~/.engram/` |
|--------------------|--------------------------|
| Sin sesión / daemon no corriendo | `engram.db.enc` únicamente |
| Sesión activa, daemon corriendo | `engram.db` (+ posible `engram.db-wal`, `engram.db-shm`) |
| Durante decrypt (transitorio, <1s) | `engram.db.enc` + `.engram.db.tmp` |
| Durante encrypt (transitorio, <1s) | `engram.db` + `.engram.db.enc.tmp` |

### Qué pasa cuando engram MCP escribe durante sesión activa

Correcto y esperado. `engram.db` existe como plaintext mientras hay sesión. El MCP server escribe normalmente. Al cierre de sesión, el daemon hace checkpoint + cifra todo, incluyendo las memorias nuevas. **No hay cambios en el binario engram**.

### Qué pasa si el daemon muere a mitad del cifrado

```
encryptDB() usa atomic write:
  1. Cifra a .engram.db.enc.tmp (archivo temporal)
  2. Solo si el write completa sin error: rename → engram.db.enc
  3. Solo si el rename OK: borrar engram.db

Si el daemon muere en paso 1 o 2:
  → engram.db sigue intacto (plaintext accesible)
  → .engram.db.enc.tmp es basura, se borra en siguiente startup

Si muere en paso 3:
  → Existen tanto engram.db como engram.db.enc
  → Startup del daemon: detectar condición, priorizar engram.db.enc, borrar plaintext
```

---

## 3. Gestión de clave: Linux Keyring

### Generación

```go
// Primera vez que no existe clave en keyring
func generateMasterKey() ([]byte, error) {
    key := make([]byte, 32) // 256 bits
    if _, err := rand.Read(key); err != nil {
        return nil, fmt.Errorf("generate key: %w", err)
    }
    return key, nil
}
```

### Almacenamiento en Linux Kernel Keyring

```go
// golang.org/x/sys/unix — ya en go.mod como dependencia transitiva de golang.org/x/sys v0.30.0
import "golang.org/x/sys/unix"

const keyringDesc = "engram-obsidian-master-key"

func storeKey(key []byte) error {
    // USER keyring (@u) — persiste mientras el usuario tenga sesión
    // No session keyring — sobreviviría a cierre de Keyring explícito
    keyID, err := unix.AddKey("user", keyringDesc, key, unix.KEY_SPEC_USER_KEYRING)
    if err != nil {
        return fmt.Errorf("keyring store: %w", err)
    }
    // Restringir lectura solo al UID actual
    _ = keyID
    return nil
}

func loadKey() ([]byte, error) {
    keyID, err := unix.KeyctlSearch(unix.KEY_SPEC_USER_KEYRING, "user", keyringDesc, 0)
    if err != nil {
        return nil, fmt.Errorf("keyring search: key not found — %w", err)
    }
    buf := make([]byte, 32)
    n, err := unix.KeyctlBuffer(unix.KEYCTL_READ, keyID, buf, 32)
    if err != nil {
        return nil, fmt.Errorf("keyring read: %w", err)
    }
    return buf[:n], nil
}
```

### Key Derivation

La master key del keyring no se usa directamente para cifrar. Se deriva con HKDF para separar instancias por usuario y máquina:

```go
import (
    "crypto/sha256"
    "golang.org/x/crypto/hkdf"
)

func deriveEncryptionKey(masterKey []byte) ([]byte, error) {
    machineID, err := os.ReadFile("/etc/machine-id")
    if err != nil {
        return nil, fmt.Errorf("derive key: read machine-id: %w", err)
    }
    uid := fmt.Sprintf("%d", os.Getuid())
    salt := append(bytes.TrimSpace(machineID), []byte(uid)...)

    reader := hkdf.New(sha256.New, masterKey, salt, []byte("engram-obsidian-v1"))
    derived := make([]byte, 32)
    if _, err := io.ReadFull(reader, derived); err != nil {
        return nil, fmt.Errorf("derive key: hkdf: %w", err)
    }
    return derived, nil
}
```

**Nota sobre WSL2:** `/etc/machine-id` existe en WSL2 (generado en primer boot de la distro). `golang.org/x/crypto/hkdf` requiere agregar `golang.org/x/crypto` a `go.mod` — **única dependencia nueva**.

### WSL2 compatibility

El Linux Kernel Keyring funciona en WSL2 (kernel 5.15+ incluido en WSL2 por defecto). Verificado: `keyctl show @u` funciona en WSL2 con Ubuntu/Debian. El keyring de usuario persiste mientras haya al menos una sesión del usuario abierta.

---

## 3b. Portabilidad y fallbacks de key storage

### Detección en runtime de Linux Keyring

No todos los sistemas Linux tienen keyring disponible (kernels viejos, distros minimalistas, containers sin namespaces de usuario configurados). La detección debe ser en runtime, no en build time:

```go
func keyringAvailable() bool {
    // Intentar operación de prueba en el keyring del usuario
    // Si falla → keyring no disponible en este sistema/kernel
    _, err := unix.KeyctlSearch(unix.KEY_SPEC_USER_KEYRING, "user", "__engram_probe__", 0)
    // ENOKEY = keyring existe pero clave no encontrada → keyring funciona
    // Otros errores → keyring no disponible
    return err == nil || errors.Is(err, unix.ENOKEY)
}
```

### Estrategia de fallback (en orden de preferencia)

1. **Linux Keyring** (session-bound, en memoria kernel) — óptimo
2. **Key file cifrada con machine-derived key** (`~/.engram/engram-key.enc`, permisos `0400`) — si keyring no disponible
3. Si ni siquiera existe `/etc/machine-id` → usar `hostname + UID + username` como salt

### Implementación del fallback

```go
func getOrCreateKey(dbDir string) ([]byte, error) {
    // Intento 1: keyring
    if keyringAvailable() {
        key, err := loadKeyFromKeyring()
        if err == nil {
            return key, nil
        }
        // No existía → generar y guardar en keyring
        key, err = generateMasterKey()
        if err != nil {
            return nil, err
        }
        _ = storeKeyInKeyring(key)       // best-effort
        _ = saveKeyToFile(key, dbDir)    // backup siempre
        return key, nil
    }

    // Intento 2: key file cifrada con machine-derived key
    return getOrCreateKeyFromFile(dbDir)
}
```

### Fallback de machine-id

```go
func getMachineID() string {
    if data, err := os.ReadFile("/etc/machine-id"); err == nil {
        return strings.TrimSpace(string(data))
    }
    // Fallback: hostname + UID + username
    hostname, _ := os.Hostname()
    u, _ := user.Current()
    return fmt.Sprintf("%s-%s-%s", hostname, u.Uid, u.Username)
}
```

### Build tags — OBLIGATORIO

- `internal/crypto/keyring.go` debe tener `//go:build linux` al inicio — el código usa `unix.KeyctlSearch` que no existe en macOS/Windows
- Crear `internal/crypto/keyring_other.go` con `//go:build !linux` que retorna un error descriptivo: `"Linux Keyring not available on this platform — using file-based key storage"`
- Esto garantiza que el proyecto compile en cualquier plataforma aunque el keyring solo funcione en Linux

---

## 4. Algoritmo de cifrado: AES-256-GCM

### Por qué AES-GCM sobre CBC/CFB

| Propiedad | AES-CBC | AES-CFB | AES-256-GCM |
|-----------|---------|---------|-------------|
| Autenticación integrada | ❌ | ❌ | ✅ |
| Detecta tampering | ❌ | ❌ | ✅ |
| Padding oracle attack | Vulnerable | Vulnerable | Inmune |
| Performance | OK | OK | Excelente (AES-NI) |
| Disponible en stdlib Go | ✅ | ✅ | ✅ |

AES-GCM provee **AEAD** (Authenticated Encryption with Associated Data). Si alguien modifica `engram.db.enc` en disco, el decrypt falla con error de autenticación antes de escribir datos corruptos.

### Formato del archivo `.enc`

```
[4 bytes magic: 0x454E474D ("ENGM")] 
[1 byte version: 0x01]
[12 bytes nonce (random, por operación)]
[N bytes ciphertext]
[16 bytes GCM authentication tag]
```

Total overhead: 33 bytes sobre el tamaño de `engram.db`.

### Implementación

```go
// internal/crypto/crypto.go

const magic = "ENGM"
const magicVersion = byte(0x01)

func Encrypt(key, plaintext []byte) ([]byte, error) {
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    nonce := make([]byte, gcm.NonceSize()) // 12 bytes
    if _, err := rand.Read(nonce); err != nil {
        return nil, err
    }
    ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

    // Formato: magic(4) + version(1) + nonce(12) + ciphertext+tag
    out := make([]byte, 0, 5+len(nonce)+len(ciphertext))
    out = append(out, []byte(magic)...)
    out = append(out, magicVersion)
    out = append(out, nonce...)
    out = append(out, ciphertext...)
    return out, nil
}

func Decrypt(key, data []byte) ([]byte, error) {
    if len(data) < 5+12+16 {
        return nil, fmt.Errorf("invalid .enc file: too short")
    }
    if string(data[:4]) != magic {
        return nil, fmt.Errorf("invalid .enc file: bad magic")
    }
    if data[4] != magicVersion {
        return nil, fmt.Errorf("unsupported .enc version: %d", data[4])
    }
    nonce := data[5:17]
    ciphertext := data[17:]

    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, err
    }
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    return gcm.Open(nil, nonce, ciphertext, nil)
}
```

**Consideración de memoria:** `engram.db` típico es <10MB. Cargar en memoria para cifrar/descifrar es aceptable. Si crece a >100MB, considerar streaming con GCM chunked (fuera de scope v1).

---

## 5. Manejo del WAL de SQLite

### Por qué el WAL es crítico

SQLite en modo WAL escribe primero en `engram.db-wal`. Los datos commiteados pero no checkpointed **solo existen en el WAL**, no en `engram.db`. Si se cifra `engram.db` sin hacer checkpoint, se pierden esos datos.

### Secuencia correcta antes de cifrar

```go
func (d *Daemon) encryptDB(dbPath string, key []byte) error {
    // 1. Abrir conexión temporal solo para checkpoint
    db, err := sql.Open("sqlite", fmt.Sprintf("file:%s", dbPath))
    if err != nil {
        return fmt.Errorf("encrypt: open for checkpoint: %w", err)
    }
    
    // 2. Checkpoint TRUNCATE: commitea WAL + trunca el archivo WAL a 0 bytes
    // Esto fuerza que todos los datos queden en engram.db principal
    if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
        db.Close()
        return fmt.Errorf("encrypt: wal_checkpoint: %w", err)
    }
    db.Close()

    // 3. Leer engram.db completo
    plaintext, err := os.ReadFile(dbPath)
    if err != nil {
        return fmt.Errorf("encrypt: read db: %w", err)
    }

    // 4. Cifrar
    ciphertext, err := crypto.Encrypt(key, plaintext)
    if err != nil {
        return fmt.Errorf("encrypt: aes-gcm: %w", err)
    }

    // 5. Atomic write
    tmpPath := dbPath + ".enc.tmp"
    if err := os.WriteFile(tmpPath, ciphertext, 0600); err != nil {
        return fmt.Errorf("encrypt: write tmp: %w", err)
    }
    encPath := dbPath + ".enc" // → ~/.engram/engram.db.enc
    if err := os.Rename(tmpPath, encPath); err != nil {
        os.Remove(tmpPath)
        return fmt.Errorf("encrypt: rename: %w", err)
    }

    // 6. Borrar plaintext (incluyendo WAL y SHM residuales)
    os.Remove(dbPath)
    os.Remove(dbPath + "-wal")
    os.Remove(dbPath + "-shm")

    return nil
}
```

### `PRAGMA wal_checkpoint(TRUNCATE)` vs `FULL` vs `PASSIVE`

- `PASSIVE`: no espera — puede fallar si hay readers activos
- `FULL`: espera readers pero no trunca
- `TRUNCATE`: espera readers + trunca WAL → **archivo WAL queda en 0 bytes** → confirma que todo fue commiteado

El daemon cierra el `store.Reader` **antes** de llamar a `encryptDB()` para no ser él mismo un blocker del checkpoint.

---

## 6. Casos edge y mitigaciones

### Daemon muere mid-encryption

```
Secuencia encryptDB() con atomic write:

Paso 1: PRAGMA wal_checkpoint(TRUNCATE)     ← Si muere: engram.db intacto ✅
Paso 2: os.ReadFile(engram.db)              ← Si muere: engram.db intacto ✅
Paso 3: Encrypt(key, plaintext)             ← Si muere: solo en memoria, nada en disco ✅
Paso 4: WriteFile(.enc.tmp)                 ← Si muere: .enc.tmp incompleto (basura), engram.db intacto ✅
Paso 5: Rename(.enc.tmp → .enc)             ← Atómico en Linux. Si muere antes: .enc.tmp existe, engram.db intacto ✅
                                               Si muere después: .enc completo, engram.db intacto ✅
Paso 6: Remove(engram.db, -wal, -shm)       ← Si muere: existen tanto .enc como .db → startup detecta y borra .db ✅
```

**Regla de startup del daemon:**
```go
func (d *Daemon) resolveDBState(dbPath string) error {
    encPath := dbPath + ".enc"
    _, errDB  := os.Stat(dbPath)
    _, errEnc := os.Stat(encPath)

    switch {
    case errDB == nil && errEnc == nil:
        // Ambos existen — crash en paso 6. Priorizar .enc (más reciente).
        d.cfg.Logf("WARN: both engram.db and engram.db.enc found — removing plaintext (priority: encrypted)")
        os.Remove(dbPath)
        os.Remove(dbPath + "-wal")
        os.Remove(dbPath + "-shm")
    case errDB != nil && errEnc == nil:
        // Estado normal sin sesión — OK
    case errDB == nil && errEnc != nil:
        // Estado normal con sesión — OK
    case errDB != nil && errEnc != nil:
        // Primera instalación — bootstrap
        d.cfg.Logf("No DB found — first run, will create on first sync")
    }
    
    // Limpiar .enc.tmp residual
    os.Remove(dbPath + ".enc.tmp")
    return nil
}
```

### Key perdida (crash severo del sistema)

El Linux Keyring vive en memoria kernel. Si el sistema se apaga abruptamente (no un cierre limpio de sesión), la clave desaparece del keyring pero `engram.db.enc` puede quedar cifrada.

**Recovery mechanism (backup key):**

```go
// Al generar la master key por primera vez, también guardar backup
func saveKeyBackup(key []byte, dbDir string) error {
    // Derivar backup key desde machine-id solamente (sin user keyring)
    // Permite recovery si el keyring falla pero la máquina es la misma
    machineID, _ := os.ReadFile("/etc/machine-id")
    backupSalt := append(bytes.TrimSpace(machineID), []byte("backup-v1")...)
    reader := hkdf.New(sha256.New, key, backupSalt, []byte("engram-backup"))
    backupKey := make([]byte, 32)
    io.ReadFull(reader, backupKey)

    // Cifrar master key con backup key y guardar en disco
    encrypted, _ := crypto.Encrypt(backupKey, key)
    return os.WriteFile(filepath.Join(dbDir, "engram-key.bak"), encrypted, 0400)
}
```

**Limitación:** Si el atacante tiene acceso físico al disco Y conoce el machine-id, puede recuperar la clave. Esto es un tradeoff consciente: backup key protege contra crash accidental, no contra adversario sofisticado con acceso físico.

**Recovery flow:** al startup, si `loadKey()` falla y existe `engram-key.bak`, intenta recuperar con machine-id derived key y re-registra en keyring.

### engram MCP intenta abrir DB encriptada

Sin `engram.db` en disco, `engram` falla con:
```
open ~/.engram/engram.db: no such file or directory
```
Esto es correcto y esperado. El usuario no tiene sesión activa = no hay acceso a memorias. No hay cambios necesarios en `engram`.

**Consideración:** si el usuario inicia `engram` antes de que el daemon haya descifrado (race condition de startup), verá el mismo error. El daemon debería descifrar lo más rápido posible en el bootstrap. La ventana típica es <500ms.

### Primera instalación sin `.enc` (bootstrap)

```go
func (d *Daemon) decryptDB(dbPath string) error {
    encPath := dbPath + ".enc"
    
    if _, err := os.Stat(encPath); os.IsNotExist(err) {
        // Primera vez: engram.db puede existir ya (instalación sin cifrado previo)
        if _, err := os.Stat(dbPath); err == nil {
            d.cfg.Logf("First run with encryption: encrypting existing DB")
            key, err := d.getOrCreateKey(dbPath)
            if err != nil {
                return err
            }
            return d.encryptDB(dbPath, key) // cifra el DB existente y lo deja como .enc
        }
        // No hay DB ni .enc — primera instalación desde cero, no hay nada que descifrar
        d.cfg.Logf("No DB found — will be created by engram on first use")
        return nil
    }

    // Caso normal: descifrar .enc → engram.db
    key, err := d.getOrCreateKey(dbPath)
    if err != nil {
        return err
    }
    // ... decrypt flow
}
```

### Múltiples instancias del daemon

Actualmente el daemon no tiene lock file. Con cifrado, dos instancias simultáneas podrían:
1. Ambas leer `engram.db.enc` y descifrar OK (idempotente, no destructivo)
2. Ambas intentar `encryptDB()` simultáneamente → una escritura gana, la otra puede fallar en `os.Remove`

**Mitigación v1:** `flock` en `~/.engram/engram-obsidian.lock` al inicio del proceso. Implementación simple:
```go
lockFile, _ := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
    return fmt.Errorf("another instance is running")
}
defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
```

---

## 7. Impacto en el proyecto engram (binario externo)

**Cambios necesarios en engram: CERO.**

El binario `engram` sigue la misma lógica que hoy:
- Abre `~/.engram/engram.db` si existe → escribe normalmente
- Si no existe → crea uno nuevo (comportamiento actual sin cambios)

El daemon `engram-obsidian` gestiona todo externamente. `engram` no sabe nada de cifrado.

### Race condition: engram escribe mientras daemon está cifrando

**Escenario:** sesión cerrándose, daemon llama `encryptDB()`, pero el proceso `engram` MCP server sigue vivo unos segundos más con la DB abierta.

```
Timeline:
T=0  ShouldSync() → false (cleanupCountdown=1)
T=2  ShouldSync() → false (cleanupCountdown=2 → trigger encryptDB)
T=2  encryptDB(): PRAGMA wal_checkpoint(TRUNCATE) — puede bloquearse si engram tiene writers activos
     → PRAGMA usa busy_timeout implícito. Si después de timeout sigue bloqueado → log WARN, reintentar en siguiente tick
T=4  engram MCP server termina (proceso muere con la sesión)
T=6  encryptDB() reintento → checkpoint OK → cifrado completo
```

**Mitigación:** `encryptDB()` no es un paso único — si el checkpoint falla por busy, retorna error y el daemon lo reintenta en el siguiente tick (2.5s). El vault de Obsidian **no se limpia** hasta que `encryptDB()` tenga éxito.

```go
// En el loop del daemon, antes de cleanup():
if wasSynced && !conditionsMet {
    cleanupCountdown++
    if cleanupCountdown >= cleanupConfirmPolls {
        if err := d.encryptDB(dbPath, key); err != nil {
            d.cfg.Logf("WARN encryptDB: %v — retrying next tick", err)
            // No resetear cleanupCountdown — reintentar inmediatamente
        } else {
            if d.cleanup() {
                wasSynced = false
                cleanupCountdown = 0
            }
        }
    }
}
```

---

## 8. Scope de cambios en engram-obsidian

### Archivos a crear

**`internal/crypto/crypto.go`**  
AES-256-GCM encrypt/decrypt. Manejo del formato `.enc` (magic + version + nonce + ciphertext).  
Deps: `crypto/aes`, `crypto/cipher`, `crypto/rand` — todos stdlib.

**`internal/crypto/keyring.go`**  
Linux Kernel Keyring integration: `storeKey`, `loadKey`, `getOrCreateKey`, `saveKeyBackup`, `recoverKeyFromBackup`.  
Deps: `golang.org/x/sys/unix` (ya indirecta en go.mod como `golang.org/x/sys v0.30.0` — solo hacer direct), `golang.org/x/crypto` (nueva — solo para HKDF).  
Requiere `//go:build linux` al inicio del archivo.

**`internal/crypto/keyring_other.go`**  
Stub para plataformas no-Linux. Implementa las mismas funciones con `//go:build !linux` retornando `errors.New("Linux Keyring not available on this platform — using file-based key storage")`. Garantiza que el proyecto compile en macOS/Windows sin cambios en el build pipeline.

### Archivos a modificar

**`internal/obsidian/daemon/daemon.go`**  
Agregar en el lifecycle:
- `resolveDBState()` en bootstrap de `Run()` — antes del primer `ShouldSync()`
- `decryptDB()` cuando `conditionsMet` pasa a `true` (antes de `runCycle()` / `syncOnly()`)  
- `encryptDB()` cuando `!conditionsMet && cleanupCountdown >= cleanupConfirmPolls` (antes de `cleanup()`)
- `flock` para instancia única

**`internal/obsidian/selection.go`**  
Agregar `EncryptDB bool` al Config struct:
```go
type Config struct {
    VaultPath string `json:"vault_path"`
    DBPath    string `json:"db_path"`
    GraphMode string `json:"graph_mode"`
    EncryptDB bool   `json:"encrypt_db"` // default: false (opt-in)
}
```

**`internal/obsidian/tui/view.go` y `update.go`**  
Agregar toggle "Encrypt DB: [ ] Enabled" en el Config screen, entre GraphMode y Continuar. Mismo patrón que el radio button de GraphMode pero checkbox booleano. Default `false` — el usuario debe opt-in explícitamente.

**`internal/store/reader.go`**  
Agregar chequeo previo en `Open()`:
```go
func Open(dbPath string) (*Reader, error) {
    // ... expand home path ...
    
    // Si .enc existe pero .db no → error descriptivo
    if _, err := os.Stat(dbPath); os.IsNotExist(err) {
        encPath := dbPath + ".enc"
        if _, encErr := os.Stat(encPath); encErr == nil {
            return nil, fmt.Errorf("DB is encrypted (%s exists) — engram-obsidian daemon must be running", encPath)
        }
        return nil, fmt.Errorf("DB not found at %s", dbPath)
    }
    // ... resto igual ...
}
```

### Nueva dependencia en go.mod

```
golang.org/x/crypto v0.x.x  ← para HKDF (hkdf package)
```

`golang.org/x/sys` ya está como indirecta — solo moverla a directa.

### Resumen de cambios

| Archivo | Tipo | Líneas estimadas |
|---------|------|-----------------|
| `internal/crypto/crypto.go` | Nuevo | ~60 |
| `internal/crypto/keyring.go` | Nuevo | ~100 |
| `internal/crypto/keyring_other.go` | Nuevo | ~15 |
| `internal/obsidian/daemon/daemon.go` | Modificar | +80 |
| `internal/obsidian/selection.go` | Modificar | +5 |
| `internal/obsidian/tui/view.go` | Modificar | +10 |
| `internal/obsidian/tui/update.go` | Modificar | +8 |
| `internal/store/reader.go` | Modificar | +10 |
| `go.mod` / `go.sum` | Modificar | +2 |
| **Total** | | **~290 líneas** |

> Todo el código en `internal/crypto/keyring.go` debe tener `//go:build linux`. El stub `keyring_other.go` con `//go:build !linux` garantiza que el proyecto compile en macOS/Windows aunque no use el keyring.

---

## 9. Análisis de riesgo

| Riesgo | Probabilidad | Impacto | Mitigación |
|--------|-------------|---------|------------|
| Key perdida por crash severo → datos inaccesibles | Baja | Crítico | Backup key con machine-id derivation |
| WAL no checkpointed → pérdida de memorias recientes al cifrar | Media | Alto | `PRAGMA wal_checkpoint(TRUNCATE)` obligatorio antes de encrypt |
| Race condition: engram escribe durante encrypt | Baja | Medio | Retry logic en daemon — checkpoint falla → reintenta next tick |
| Múltiples instancias del daemon corrompiendo `.enc` | Muy baja | Alto | `flock` en startup |
| `engram.db.enc.tmp` residual tras crash durante decrypt | Baja | Bajo | `resolveDBState()` en startup limpia archivos `.tmp` |
| Keyring no disponible en el sistema del usuario | Media | Medio | Detección runtime + fallback automático a key file |
| WSL2 kernel sin soporte keyring | Muy baja | Crítico | Verificado: WSL2 kernel 5.15+ soporta keyring. Fallback: error descriptivo al usuario |
| Degradación de performance (encrypt/decrypt en cada sesión) | Segura | Bajo | `engram.db` típico <5MB — AES-NI cifra en <10ms |
| Backup key expuesta si atacante tiene machine-id + acceso físico | Baja | Medio | Limitación documentada y aceptada. Protege contra crash, no contra adversario con acceso físico |

---

## 10. Recomendación final

### ¿Es factible? SÍ

El diseño es sólido porque:
1. **No toca el binario `engram`** — zero riesgo de romper el sistema principal
2. **Toda la lógica en el daemon** — componente que ya gestiona el ciclo de vida de sesión
3. **Primitivas de stdlib** — AES-GCM es `crypto/cipher` puro Go, no CGO, no deps raras
4. **Linux Keyring ya funciona en WSL2** — la plataforma target está cubierta

### ¿Cuánto esfuerzo?

**2-3 días de trabajo real** para un senior:
- Día 1: `internal/crypto/` (crypto.go + keyring.go) + tests unitarios
- Día 2: Integración en daemon.go (`decryptDB`, `encryptDB`, `resolveDBState`, `flock`)
- Día 3: Testing de casos edge (crash recovery, backup key, primera instalación) + ajuste del error en reader.go

### Qué implementar primero

**Orden recomendado:**

1. **`internal/crypto/crypto.go`** — sin side effects, testeable en aislamiento total
2. **`internal/crypto/keyring.go`** — puede mockearse en tests con variable de entorno
3. **`internal/obsidian/daemon/daemon.go`** — integrar con feature flag (`ENGRAM_ENCRYPT=1`) para poder probar sin romper flujo existente
4. **`internal/store/reader.go`** — trivial, pero necesario para UX correcta
5. **Remover feature flag** — solo cuando los tests de integración pasen

### Lo que NO hacer

- **No usar SQLCipher o sqlcipher-go**: requiere CGO, rompe la compilación portátil del proyecto. El enfoque de cifrar el archivo completo es más simple y igualmente seguro para este threat model.
- **No cifrar en chunks/streaming en v1**: complejidad innecesaria para el tamaño típico de `engram.db`.
- **No poner la clave en `~/.engram/engram-key`** en plaintext como "solución rápida": completamente contraproducente para el threat model definido.
- **No asumir keyring disponible sin fallback**: detectar en runtime, tener estrategia alternativa para distros sin `keyutils` o kernels viejos.
- **No hardcodear nada específico del entorno del developer**: machine-id, UID, paths — todo debe leerse/derivarse en runtime. El documento ya hace esto, pero el código de implementación debe verificarlo explícitamente.
- **No activar encriptación por default**: debe ser opt-in desde el Config screen (`EncryptDB: false` por default). Un usuario que instala por primera vez no debe tener su DB encriptada sin entender las implicancias (recovery, dependencia del daemon).
