# Implementation Plan: Transaction Modes, Locking, and MySQL DDL Warning

This plan implements 3 changes in small, testable steps:
1. ✅ `.no-db-txn.` filename marker + `-db-txn-mode` flag
2. ✅ Cross-process locking with `-no-lock` opt-out
3. ✅ MySQL DDL warning

---

## Phase 1: `.no-db-txn.` Detection and `-db-txn-mode` Flag ✅ COMPLETE

### Step 1.1: Verify `make test` passes (baseline)

Before making any code changes, confirm existing tests pass:

```bash
make test
```

All database drivers (postgres, mysql, mariadb, sqlite3, cql) should pass. This establishes the baseline.

---

### Step 1.2: Add `DbTxnMode` type and constants

**File:** `lib.go` (after line 17, after imports)

**Add:**
```go
// DbTxnMode controls how migrations are wrapped in transactions
type DbTxnMode string

const (
    // DbTxnModeAll wraps all pending migrations in a single transaction (existing behavior)
    DbTxnModeAll  DbTxnMode = "all"
    // DbTxnModePerFile wraps each migration file in its own transaction
    DbTxnModePerFile DbTxnMode = "per-file"
    // DbTxnModeNone runs migrations without transaction wrapping
    DbTxnModeNone DbTxnMode = "none"
)

// ValidDbTxnModes lists all valid transaction mode values
var ValidDbTxnModes = []DbTxnMode{DbTxnModeAll, DbTxnModePerFile, DbTxnModeNone}

// ParseDbTxnMode parses a string into DbTxnMode, returns error if invalid
func ParseDbTxnMode(s string) (DbTxnMode, error) {
    mode := DbTxnMode(s)
    for _, valid := range ValidDbTxnModes {
        if mode == valid {
            return mode, nil
        }
    }
    return "", errors.Errorf("invalid -db-txn-mode %q: must be one of: all, per-file, none", s)
}
```

**Test:** `lib_test.go`
```go
func TestParseDbTxnMode(t *testing.T) {
    tests := []struct {
        input    string
        expected DbTxnMode
        wantErr  bool
    }{
        {"all", DbTxnModeAll, false},
        {"per-file", DbTxnModePerFile, false},
        {"none", DbTxnModeNone, false},
        {"invalid", "", true},
        {"", "", true},
        // Partial matches should fail (exact match required)
        {"al", "", true},          // "all" missing last char
        {"ll", "", true},          // "all" missing first char
        {"per-fil", "", true},     // "per-file" missing last char
        {"er-file", "", true},     // "per-file" missing first char
        {"non", "", true},         // "none" missing last char
        // Case mismatches should fail (exact match required)
        {"All", "", true},
        {"ALL", "", true},
        {"Per-File", "", true},
        {"PER-FILE", "", true},
        {"None", "", true},
        {"NONE", "", true},
    }
    for _, tc := range tests {
        mode, err := ParseDbTxnMode(tc.input)
        if tc.wantErr {
            assert.Error(t, err)
        } else {
            assert.NoError(t, err)
            assert.Equal(t, tc.expected, mode)
        }
    }
}
```

**Verification:** `go test -run TestParseDbTxnMode ./...`

---

### Step 1.3: Add `requiresNoTransaction` helper function

**File:** `lib.go` (after `ParseDbTxnMode` function)

**Add:**
```go
const noDbTxnMarker = ".no-db-txn."

// requiresNoTransaction returns true if filename contains the .no-db-txn. marker
func requiresNoTransaction(filename string) bool {
    return strings.Contains(filename, noDbTxnMarker)
}
```

**Test:** `lib_test.go`
```go
func TestRequiresNoTransaction(t *testing.T) {
    tests := []struct {
        filename string
        expected bool
    }{
        {"20240101120000_create_users.up.sql", false},
        {"20240101120000_create_users.down.sql", false},
        {"20240101130000_add_index.no-db-txn.up.sql", true},
        {"20240101130000_add_index.no-db-txn.down.sql", true},
        {"some/path/20240101130000_add_index.no-db-txn.up.sql", true},
        // Partial matches should not trigger (exact ".no-db-txn." required)
        {"20240101130000_add_index.no-db-txnup.sql", false},   // missing trailing dot
        {"20240101130000_add_indexno-db-txn.up.sql", false},   // missing leading dot
        {"20240101130000_add_index.no-db-tx.up.sql", false},   // truncated marker
        {"20240101130000_add_index.o-db-txn.up.sql", false},   // missing 'n' at start
        // Case mismatches should not trigger (exact ".no-db-txn." required)
        {"20240101130000_add_index.No-Db-Txn.up.sql", false},
        {"20240101130000_add_index.NO-DB-TXN.up.sql", false},
    }
    for _, tc := range tests {
        result := requiresNoTransaction(tc.filename)
        assert.Equal(t, tc.expected, result, "filename: %s", tc.filename)
    }
}
```

**Verification:** `go test -run TestRequiresNoTransaction ./...`

---

### Step 1.4: Add `DbTxnModeConflictError` type

**File:** `lib.go` (after `requiresNoTransaction` function)

**Add:**
```go
// DbTxnModeConflictError is returned when .no-db-txn. files exist but mode is not "per-file" or "none"
type DbTxnModeConflictError struct {
    Files       []string
    CurrentMode DbTxnMode
}

func (e *DbTxnModeConflictError) Error() string {
    var sb strings.Builder
    sb.WriteString("Error: Cannot apply migrations in -db-txn-mode=all (default)\n\n")
    sb.WriteString("The following migrations require -db-txn-mode=per-file:\n")
    for _, f := range e.Files {
        sb.WriteString("  - ")
        sb.WriteString(f)
        sb.WriteString("\n")
    }
    sb.WriteString("\nRun with: dbmigrate -up -db-txn-mode=per-file")
    return sb.String()
}
```

**Test:** `lib_test.go`
```go
func TestDbTxnModeConflictError(t *testing.T) {
    err := &DbTxnModeConflictError{
        Files:       []string{"20240101130000_add_index.no-db-txn.up.sql"},
        CurrentMode: DbTxnModeAll,
    }
    msg := err.Error()
    assert.Contains(t, msg, "Cannot apply migrations in -db-txn-mode=all")
    assert.Contains(t, msg, "20240101130000_add_index.no-db-txn.up.sql")
    assert.Contains(t, msg, "-db-txn-mode=per-file")
}
```

**Verification:** `go test -run TestDbTxnModeConflictError ./...`

---

### Step 1.5: Add `validateDbTxnMode` function

**File:** `lib.go` (after `DbTxnModeConflictError`)

**Add:**
```go
// validateDbTxnMode checks if pending files are compatible with the transaction mode
// Returns error if mode is "all" but .no-db-txn. files exist
func validateDbTxnMode(files []string, mode DbTxnMode) error {
    if mode != DbTxnModeAll {
        return nil
    }
    var conflicts []string
    for _, f := range files {
        if requiresNoTransaction(f) {
            conflicts = append(conflicts, f)
        }
    }
    if len(conflicts) > 0 {
        return &DbTxnModeConflictError{
            Files:       conflicts,
            CurrentMode: mode,
        }
    }
    return nil
}
```

**Test:** `lib_test.go`
```go
func TestValidateDbTxnMode(t *testing.T) {
    tests := []struct {
        name    string
        files   []string
        mode    DbTxnMode
        wantErr bool
    }{
        {
            name:    "all mode with normal files",
            files:   []string{"20240101_create.up.sql", "20240102_add.up.sql"},
            mode:    DbTxnModeAll,
            wantErr: false,
        },
        {
            name:    "all mode with no-db-txn file",
            files:   []string{"20240101_create.up.sql", "20240102_add.no-db-txn.up.sql"},
            mode:    DbTxnModeAll,
            wantErr: true,
        },
        {
            name:    "per-file mode with no-db-txn file",
            files:   []string{"20240101_create.up.sql", "20240102_add.no-db-txn.up.sql"},
            mode:    DbTxnModePerFile,
            wantErr: false,
        },
        {
            name:    "none mode with no-db-txn file",
            files:   []string{"20240101_create.up.sql", "20240102_add.no-db-txn.up.sql"},
            mode:    DbTxnModeNone,
            wantErr: false,
        },
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            err := validateDbTxnMode(tc.files, tc.mode)
            if tc.wantErr {
                assert.Error(t, err)
                var conflictErr *DbTxnModeConflictError
                assert.ErrorAs(t, err, &conflictErr)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

**Verification:** `go test -run TestValidateDbTxnMode ./...`

---

### Step 1.6: Add `driverName` field to Config struct

**File:** `lib.go`

**Change line 71-76 (Config struct):**
```go
// A Config holds on to an open database to perform dbmigrate
type Config struct {
    dir            fs.FS
    db             *sql.DB
    driverName     string   // NEW FIELD
    adapter        Adapter
    migrationFiles []string
}
```

**Change line 118-124 (New function return):**
```go
return &Config{
    dir:            dir,
    db:             db,
    driverName:     driverName,  // NEW LINE
    adapter:        adapter,
    migrationFiles: migrationFiles,
}, nil
```

**Test:** No new test needed, existing tests should pass.

**Verification:** `go test ./...`

---

### Step 1.7: Add `DriverName()` getter method to Config

**File:** `lib.go` (after `CloseDB` method, around line 130)

**Add:**
```go
// DriverName returns the database driver name for this config
func (c *Config) DriverName() string {
    return c.driverName
}
```

**Test:** No separate test needed, will be used in Phase 3.

**Verification:** `go build ./...`

---

### Step 1.8: Create `MigrateUpWithMode` method (refactor of MigrateUp)

This step adds a new method that accepts `DbTxnMode`. The existing `MigrateUp` will call this with `DbTxnModeAll`.

**File:** `lib.go`

**Add new method after existing `MigrateUp` (after line 237):**
```go
// MigrateUpWithMode applies pending migrations with the specified transaction mode
func (c *Config) MigrateUpWithMode(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), mode DbTxnMode) error {
    migratedVersions, err := c.existingVersions(ctx, schema)
    if err != nil {
        return errors.Wrapf(err, "unable to query existing versions")
    }

    migrationFiles := c.migrationFiles
    sort.SliceStable(migrationFiles, func(i int, j int) bool {
        return strings.Compare(migrationFiles[i], migrationFiles[j]) == -1 // in ascending order
    })

    // Collect pending files for validation
    var pendingFiles []string
    for i := range migrationFiles {
        currName := migrationFiles[i]
        if !strings.HasSuffix(currName, "up.sql") {
            continue
        }
        currVer := strings.Split(currName, "_")[0]
        if _, found := migratedVersions.Find(currVer); found {
            continue
        }
        pendingFiles = append(pendingFiles, currName)
    }

    // Validate transaction mode compatibility
    if err := validateDbTxnMode(pendingFiles, mode); err != nil {
        return err
    }

    // Dispatch to appropriate migration strategy
    switch mode {
    case DbTxnModeAll:
        return c.migrateUpAll(ctx, txOpts, schema, logFilename, pendingFiles)
    case DbTxnModePerFile:
        return c.migrateUpPerFile(ctx, txOpts, schema, logFilename, pendingFiles)
    case DbTxnModeNone:
        return c.migrateUpNoTx(ctx, schema, logFilename, pendingFiles)
    default:
        return errors.Errorf("unknown transaction mode: %s", mode)
    }
}
```

**Verification:** `go build ./...` (will fail until Step 1.9-1.11 complete the helper methods)

---

### Step 1.9: Extract `migrateUpAll` helper (existing behavior)

**File:** `lib.go`

**Add after `MigrateUpWithMode`:**
```go
// migrateUpAll runs all pending migrations in a single transaction (existing behavior)
func (c *Config) migrateUpAll(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), pendingFiles []string) error {
    tx, err := c.adapter.BeginTx(ctx, c.db, txOpts)
    if err != nil {
        return errors.Wrapf(err, "unable to create transaction")
    }
    defer tx.Rollback()

    for _, currName := range pendingFiles {
        currVer := strings.Split(currName, "_")[0]

        filecontent, err := c.fileContent(currName)
        if err != nil {
            return errors.Wrapf(err, currName)
        }

        if len(bytes.TrimSpace(filecontent)) == 0 {
            // treat empty file as success; don't run it
        } else if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
            return errors.Wrapf(err, currName)
        }
        if _, err := tx.ExecContext(ctx, c.adapter.InsertNewVersion(schema), currVer); err != nil {
            return errors.Wrapf(err, "fail to register version %q", currVer)
        }
        logFilename(currName)
    }

    err = tx.Commit()
    if err != nil && err.Error() == "pq: unexpected transaction status idle" {
        return nil
    }
    return errors.Wrapf(err, "unable to commit transaction")
}
```

**Verification:** `go build ./...`

---

### Step 1.10: Add `migrateUpPerFile` helper

**File:** `lib.go`

**Add after `migrateUpAll`:**
```go
// migrateUpPerFile runs each migration in its own transaction
// .no-db-txn. files run without transaction
func (c *Config) migrateUpPerFile(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), pendingFiles []string) error {
    applied := 0
    for _, currName := range pendingFiles {
        currVer := strings.Split(currName, "_")[0]

        filecontent, err := c.fileContent(currName)
        if err != nil {
            return errors.Wrapf(err, currName)
        }

        if requiresNoTransaction(currName) {
            // Run without transaction
            if len(bytes.TrimSpace(filecontent)) > 0 {
                if _, err := c.db.ExecContext(ctx, string(filecontent)); err != nil {
                    if applied > 0 {
                        logFilename(fmt.Sprintf("%d migrations applied before failure.", applied))
                    }
                    return errors.Wrapf(err, currName)
                }
            }
            if _, err := c.db.ExecContext(ctx, c.adapter.InsertNewVersion(schema), currVer); err != nil {
                return errors.Wrapf(err, "fail to register version %q", currVer)
            }
        } else {
            // Run in transaction
            tx, err := c.adapter.BeginTx(ctx, c.db, txOpts)
            if err != nil {
                return errors.Wrapf(err, "unable to create transaction for %s", currName)
            }

            if len(bytes.TrimSpace(filecontent)) > 0 {
                if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
                    tx.Rollback()
                    if applied > 0 {
                        logFilename(fmt.Sprintf("%d migrations applied before failure.", applied))
                    }
                    return errors.Wrapf(err, currName)
                }
            }
            if _, err := tx.ExecContext(ctx, c.adapter.InsertNewVersion(schema), currVer); err != nil {
                tx.Rollback()
                return errors.Wrapf(err, "fail to register version %q", currVer)
            }

            if err := tx.Commit(); err != nil {
                if err.Error() != "pq: unexpected transaction status idle" {
                    return errors.Wrapf(err, "unable to commit transaction for %s", currName)
                }
            }
        }
        logFilename(currName)
        applied++
    }
    return nil
}
```

**Add import `"fmt"` if not already present (line 4-17).**

**Verification:** `go build ./...`

---

### Step 1.11: Add `migrateUpNoTx` helper

**File:** `lib.go`

**Add after `migrateUpPerFile`:**
```go
// migrateUpNoTx runs all migrations without any transaction wrapping
func (c *Config) migrateUpNoTx(ctx context.Context, schema *string, logFilename func(string), pendingFiles []string) error {
    applied := 0
    for _, currName := range pendingFiles {
        currVer := strings.Split(currName, "_")[0]

        filecontent, err := c.fileContent(currName)
        if err != nil {
            return errors.Wrapf(err, currName)
        }

        if len(bytes.TrimSpace(filecontent)) > 0 {
            if _, err := c.db.ExecContext(ctx, string(filecontent)); err != nil {
                if applied > 0 {
                    logFilename(fmt.Sprintf("%d migrations applied before failure.", applied))
                }
                return errors.Wrapf(err, currName)
            }
        }
        if _, err := c.db.ExecContext(ctx, c.adapter.InsertNewVersion(schema), currVer); err != nil {
            return errors.Wrapf(err, "fail to register version %q", currVer)
        }
        logFilename(currName)
        applied++
    }
    return nil
}
```

**Verification:** `go build ./...`

---

### Step 1.12: Update existing `MigrateUp` to call `MigrateUpWithMode`

**File:** `lib.go`

**Replace lines 185-237 (the entire MigrateUp function):**
```go
// MigrateUp applies pending migrations in ascending order, in a transaction
//
// Transaction is committed on success, rollback on error. Different databases will behave
// differently, e.g. postgres & sqlite3 can rollback DDL changes but mysql cannot
func (c *Config) MigrateUp(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string)) error {
    return c.MigrateUpWithMode(ctx, txOpts, schema, logFilename, DbTxnModeAll)
}
```

**Verification:** `go test ./...` (existing tests should pass)

---

### Step 1.13: Apply same pattern to `MigrateDown`

**File:** `lib.go`

**Add `MigrateDownWithMode` method after `MigrateDown`:**
```go
// MigrateDownWithMode un-applies migrations with the specified transaction mode
func (c *Config) MigrateDownWithMode(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), downStep int, mode DbTxnMode) error {
    migratedVersions, err := c.existingVersions(ctx, schema)
    if err != nil {
        return errors.Wrapf(err, "unable to query existing versions")
    }

    migrationFiles := c.migrationFiles
    sort.SliceStable(migrationFiles, func(i int, j int) bool {
        return strings.Compare(migrationFiles[i], migrationFiles[j]) == 1 // descending order
    })

    // Collect applicable down files
    var downFiles []string
    counted := 0
    for i := range migrationFiles {
        currName := migrationFiles[i]
        if !strings.HasSuffix(currName, "down.sql") {
            continue
        }
        currVer := strings.Split(currName, "_")[0]
        if _, found := migratedVersions.Find(currVer); !found {
            continue
        }
        counted++
        if counted > downStep {
            break
        }
        downFiles = append(downFiles, currName)
    }

    // Validate transaction mode compatibility
    if err := validateDbTxnMode(downFiles, mode); err != nil {
        return err
    }

    // Dispatch to appropriate strategy
    switch mode {
    case DbTxnModeAll:
        return c.migrateDownAll(ctx, txOpts, schema, logFilename, downFiles)
    case DbTxnModePerFile:
        return c.migrateDownPerFile(ctx, txOpts, schema, logFilename, downFiles)
    case DbTxnModeNone:
        return c.migrateDownNoTx(ctx, schema, logFilename, downFiles)
    default:
        return errors.Errorf("unknown transaction mode: %s", mode)
    }
}

// migrateDownAll runs all down migrations in a single transaction
func (c *Config) migrateDownAll(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), downFiles []string) error {
    tx, err := c.adapter.BeginTx(ctx, c.db, txOpts)
    if err != nil {
        return errors.Wrapf(err, "unable to create transaction")
    }
    defer tx.Rollback()

    for _, currName := range downFiles {
        currVer := strings.Split(currName, "_")[0]

        filecontent, err := c.fileContent(currName)
        if err != nil {
            return errors.Wrapf(err, currName)
        }

        if len(bytes.TrimSpace(filecontent)) == 0 {
            // treat empty file as success
        } else if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
            return errors.Wrapf(err, currName)
        }
        if _, err := tx.ExecContext(ctx, c.adapter.DeleteOldVersion(schema), currVer); err != nil {
            return errors.Wrapf(err, "fail to unregister version %q", currVer)
        }
        logFilename(currName)
    }

    err = tx.Commit()
    if err != nil && err.Error() == "pq: unexpected transaction status idle" {
        return nil
    }
    return errors.Wrapf(err, "unable to commit transaction")
}

// migrateDownPerFile runs each down migration in its own transaction
func (c *Config) migrateDownPerFile(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), downFiles []string) error {
    applied := 0
    for _, currName := range downFiles {
        currVer := strings.Split(currName, "_")[0]

        filecontent, err := c.fileContent(currName)
        if err != nil {
            return errors.Wrapf(err, currName)
        }

        if requiresNoTransaction(currName) {
            if len(bytes.TrimSpace(filecontent)) > 0 {
                if _, err := c.db.ExecContext(ctx, string(filecontent)); err != nil {
                    if applied > 0 {
                        logFilename(fmt.Sprintf("%d migrations rolled back before failure.", applied))
                    }
                    return errors.Wrapf(err, currName)
                }
            }
            if _, err := c.db.ExecContext(ctx, c.adapter.DeleteOldVersion(schema), currVer); err != nil {
                return errors.Wrapf(err, "fail to unregister version %q", currVer)
            }
        } else {
            tx, err := c.adapter.BeginTx(ctx, c.db, txOpts)
            if err != nil {
                return errors.Wrapf(err, "unable to create transaction for %s", currName)
            }

            if len(bytes.TrimSpace(filecontent)) > 0 {
                if _, err := tx.ExecContext(ctx, string(filecontent)); err != nil {
                    tx.Rollback()
                    if applied > 0 {
                        logFilename(fmt.Sprintf("%d migrations rolled back before failure.", applied))
                    }
                    return errors.Wrapf(err, currName)
                }
            }
            if _, err := tx.ExecContext(ctx, c.adapter.DeleteOldVersion(schema), currVer); err != nil {
                tx.Rollback()
                return errors.Wrapf(err, "fail to unregister version %q", currVer)
            }

            if err := tx.Commit(); err != nil {
                if err.Error() != "pq: unexpected transaction status idle" {
                    return errors.Wrapf(err, "unable to commit transaction for %s", currName)
                }
            }
        }
        logFilename(currName)
        applied++
    }
    return nil
}

// migrateDownNoTx runs all down migrations without transaction
func (c *Config) migrateDownNoTx(ctx context.Context, schema *string, logFilename func(string), downFiles []string) error {
    applied := 0
    for _, currName := range downFiles {
        currVer := strings.Split(currName, "_")[0]

        filecontent, err := c.fileContent(currName)
        if err != nil {
            return errors.Wrapf(err, currName)
        }

        if len(bytes.TrimSpace(filecontent)) > 0 {
            if _, err := c.db.ExecContext(ctx, string(filecontent)); err != nil {
                if applied > 0 {
                    logFilename(fmt.Sprintf("%d migrations rolled back before failure.", applied))
                }
                return errors.Wrapf(err, currName)
            }
        }
        if _, err := c.db.ExecContext(ctx, c.adapter.DeleteOldVersion(schema), currVer); err != nil {
            return errors.Wrapf(err, "fail to unregister version %q", currVer)
        }
        logFilename(currName)
        applied++
    }
    return nil
}
```

**Update existing `MigrateDown` (lines 239-296):**
```go
// MigrateDown un-applies at most N migrations in descending order, in a transaction
//
// Transaction is committed on success, rollback on error. Different databases will behave
// differently, e.g. postgres & sqlite3 can rollback DDL changes but mysql cannot
func (c *Config) MigrateDown(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), downStep int) error {
    return c.MigrateDownWithMode(ctx, txOpts, schema, logFilename, downStep, DbTxnModeAll)
}
```

**Verification:** `go test ./...`

---

### Step 1.14: Add `-db-txn-mode` flag to CLI

**File:** `cmd/dbmigrate/main.go`

**Add variable declaration (after line 42, in the var block):**
```go
dbTxnMode     string
```

**Add flag definition (after line 66, before `flag.Parse()`):**
```go
flag.StringVar(&dbTxnMode,
    "db-txn-mode", "all", "transaction mode: all (default, existing behavior), per-file, or none")
```

**Modify migrate up section (around line 157-159):**
```go
// 3. MIGRATE UP; exit
if doMigrateUp {
    mode, err := dbmigrate.ParseDbTxnMode(dbTxnMode)
    if err != nil {
        return err
    }
    return m.MigrateUpWithMode(ctx, &sql.TxOptions{}, dbSchema, filenameLogger("[up]"), mode)
}
```

**Modify migrate down section (around line 162-165):**
```go
// 4. MIGRATE DOWN; exit
if doMigrateDown > 0 {
    mode, err := dbmigrate.ParseDbTxnMode(dbTxnMode)
    if err != nil {
        return err
    }
    return m.MigrateDownWithMode(ctx, &sql.TxOptions{}, dbSchema, filenameLogger("[down]"), doMigrateDown, mode)
}
```

**Verification:**
```bash
go build ./cmd/dbmigrate
./cmd/dbmigrate/dbmigrate -help  # should show -db-txn-mode flag
```

### Phase 1 Completion Notes

**Additional work beyond original plan:**
- Added `check_row_count()` helper in `tests/scenario.sh` to verify actual data state
- Tests now verify both `schema_migrations` table AND actual row data for `txn-first`/`txn-second`
- Added `test-clean` target to Makefile for cleanup between test runs
- Documented driver-specific behavior for `-db-txn-mode=none`:
  - sqlite3/mysql/mariadb: `txn-second=YES` (execute statements independently)
  - postgres: `txn-second=NO` (executes multi-statement SQL atomically)

---

## Phase 2: Cross-Process Locking ✅ COMPLETE

### Step 2.1: Add locking fields to Adapter struct

**File:** `lib.go`

**Modify Adapter struct (lines 316-327) to add new fields:**
```go
// Adapter defines raw sql statements to run for an sql.DB adapter
type Adapter struct {
    CreateVersionsTable    func(*string) string
    SelectExistingVersions func(*string) string
    InsertNewVersion       func(*string) string
    DeleteOldVersion       func(*string) string
    PingQuery              string
    CreateDatabaseQuery    func(string) string
    CreateSchemaQuery      func(string) string
    BaseDatabaseURL        func(string) (connString string, dbName string, err error)
    BeginTx                func(ctx context.Context, db *sql.DB, opts *sql.TxOptions) (ExecCommitRollbacker, error)
    // NEW: Locking support
    SupportsLocking        bool
    AcquireLock            func(ctx context.Context, conn *sql.Conn, lockID string, log func(string)) error
    ReleaseLock            func(ctx context.Context, conn *sql.Conn, lockID string) error
}
```

**Verification:** `go build ./...`

---

### Step 2.2: Add `generateLockID` function

**File:** `lib.go` (after Adapter struct definition)

**Add imports at top (line 4-17):**
```go
"hash/crc32"
```

**Add function:**
```go
// generateLockID creates a lock ID from database name, schema, and table name
func generateLockID(databaseName string, schema *string, tableName string) int64 {
    parts := []string{databaseName}
    if schema != nil && *schema != "" {
        parts = append(parts, *schema)
    }
    parts = append(parts, tableName)

    input := strings.Join(parts, "\x00")
    sum := crc32.ChecksumIEEE([]byte(input))
    return int64(sum)
}
```

**Test:** `lib_test.go`
```go
func TestGenerateLockID(t *testing.T) {
    schema := "myschema"
    tests := []struct {
        name     string
        dbName   string
        schema   *string
        table    string
    }{
        {"basic", "mydb", nil, "dbmigrate_versions"},
        {"with schema", "mydb", &schema, "dbmigrate_versions"},
    }
    for _, tc := range tests {
        t.Run(tc.name, func(t *testing.T) {
            id := generateLockID(tc.dbName, tc.schema, tc.table)
            assert.NotZero(t, id)
            // Same inputs should produce same ID
            id2 := generateLockID(tc.dbName, tc.schema, tc.table)
            assert.Equal(t, id, id2)
        })
    }

    // Different inputs should produce different IDs
    id1 := generateLockID("db1", nil, "dbmigrate_versions")
    id2 := generateLockID("db2", nil, "dbmigrate_versions")
    assert.NotEqual(t, id1, id2)
}
```

**Verification:** `go test -run TestGenerateLockID ./...`

---

### Step 2.3: Implement PostgreSQL locking

**File:** `lib.go`

**Modify postgres adapter (lines 337-371) to add locking fields:**

After line 367 (after `CreateSchemaQuery`), add:
```go
BeginTx: func(ctx context.Context, db *sql.DB, opts *sql.TxOptions) (ExecCommitRollbacker, error) {
    return db.BeginTx(ctx, opts)
},
SupportsLocking: true,
AcquireLock: func(ctx context.Context, conn *sql.Conn, lockID string, log func(string)) error {
    for {
        var acquired bool
        err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&acquired)
        if err != nil {
            return err
        }
        if acquired {
            return nil
        }
        log("Waiting for migration lock...")
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(2 * time.Second):
        }
    }
},
ReleaseLock: func(ctx context.Context, conn *sql.Conn, lockID string) error {
    _, err := conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", lockID)
    return err
},
```

**Verification:** `go build ./...`

---

### Step 2.4: Implement MySQL locking

**File:** `lib.go`

**Modify mysql adapter (lines 372-397) to add locking fields:**

After line 395 (after `BeginTx`), add:
```go
SupportsLocking: true,
AcquireLock: func(ctx context.Context, conn *sql.Conn, lockID string, log func(string)) error {
    for {
        var result sql.NullInt64
        err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", lockID).Scan(&result)
        if err != nil {
            return err
        }
        if result.Valid && result.Int64 == 1 {
            return nil
        }
        log("Waiting for migration lock...")
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-time.After(2 * time.Second):
        }
    }
},
ReleaseLock: func(ctx context.Context, conn *sql.Conn, lockID string) error {
    _, err := conn.ExecContext(ctx, "SELECT RELEASE_LOCK(?)", lockID)
    return err
},
```

**Verification:** `go build ./...`

---

### Step 2.5: Mark SQLite as not supporting locking

**File:** `cmd/dbmigrate/sqlite3.go`

**Modify the adapter registration (lines 16-27):**

After line 24 (after `BeginTx`), add:
```go
SupportsLocking: false,
AcquireLock:     nil,
ReleaseLock:     nil,
```

**Verification:** `go build ./cmd/dbmigrate`

---

### Step 2.6: Add `LockingNotSupportedError` type

**File:** `lib.go` (after `DbTxnModeConflictError`)

**Add:**
```go
// LockingNotSupportedError is returned when locking is required but not supported
type LockingNotSupportedError struct {
    DriverName string
}

func (e *LockingNotSupportedError) Error() string {
    return fmt.Sprintf(`%s does not support cross-process locking.

If you are certain only one migration process runs at a time, use:

  dbmigrate -up -no-lock

This is safe for single-process deployments (e.g., local development,
single-node production with migrations run before app starts).`, e.DriverName)
}
```

**Test:** `lib_test.go`
```go
func TestLockingNotSupportedError(t *testing.T) {
    err := &LockingNotSupportedError{DriverName: "sqlite3"}
    msg := err.Error()
    assert.Contains(t, msg, "sqlite3 does not support cross-process locking")
    assert.Contains(t, msg, "-no-lock")
}
```

**Verification:** `go test -run TestLockingNotSupportedError ./...`

---

### Step 2.7: Add `databaseName` field to Config

**File:** `lib.go`

**Modify Config struct (lines 71-77):**
```go
type Config struct {
    dir            fs.FS
    db             *sql.DB
    driverName     string
    databaseName   string   // NEW FIELD
    adapter        Adapter
    migrationFiles []string
}
```

**Modify `New` function to extract database name (around line 84-124):**

After `driverName, databaseURL, err := SanitizeDriverNameURL(...)` (line 85-88), add:
```go
// Extract database name for lock ID
var databaseName string
if adapter.BaseDatabaseURL != nil {
    _, databaseName, _ = adapter.BaseDatabaseURL(databaseURL)
}
if databaseName == "" {
    // Fallback: use the whole URL as identifier
    databaseName = databaseURL
}
```

Update the return statement (around line 118-124):
```go
return &Config{
    dir:            dir,
    db:             db,
    driverName:     driverName,
    databaseName:   databaseName,  // NEW LINE
    adapter:        adapter,
    migrationFiles: migrationFiles,
}, nil
```

**Verification:** `go build ./...`

---

### Step 2.8: Add `acquireLock` and `releaseLock` methods to Config

**File:** `lib.go` (after `DriverName` method)

**Add:**
```go
// acquireLock acquires the migration lock, returns the connection holding the lock
// Returns nil conn if noLock is true or adapter doesn't support locking
func (c *Config) acquireLock(ctx context.Context, schema *string, noLock bool, log func(string)) (*sql.Conn, error) {
    if noLock {
        if c.adapter.SupportsLocking {
            log("Warning: Running without cross-process locking. Concurrent migrations may cause corruption.")
        }
        return nil, nil
    }

    if !c.adapter.SupportsLocking {
        return nil, &LockingNotSupportedError{DriverName: c.driverName}
    }

    conn, err := c.db.Conn(ctx)
    if err != nil {
        return nil, errors.Wrap(err, "unable to get connection for locking")
    }

    lockID := generateLockID(c.databaseName, schema, "dbmigrate_versions")
    if err := c.adapter.AcquireLock(ctx, conn, fmt.Sprint(lockID), log); err != nil {
        conn.Close()
        return nil, errors.Wrap(err, "unable to acquire migration lock")
    }

    return conn, nil
}

// releaseLock releases the migration lock
func (c *Config) releaseLock(ctx context.Context, conn *sql.Conn, schema *string) {
    if conn == nil {
        return
    }
    lockID := generateLockID(c.databaseName, schema, "dbmigrate_versions")
    _ = c.adapter.ReleaseLock(ctx, conn, fmt.Sprint(lockID))
    conn.Close()
}
```

**Verification:** `go build ./...`

---

### Step 2.9: Add locking to `MigrateUpWithMode`

**File:** `lib.go`

**Change `MigrateUpWithMode` signature and add locking:**
```go
// MigrateUpWithMode applies pending migrations with the specified transaction mode
func (c *Config) MigrateUpWithMode(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), mode DbTxnMode, noLock bool) error {
    // Acquire lock
    conn, err := c.acquireLock(ctx, schema, noLock, func(s string) { logFilename(s) })
    if err != nil {
        return err
    }
    defer c.releaseLock(ctx, conn, schema)

    // ... rest of existing implementation unchanged ...
```

**Update `MigrateUp` to pass noLock=false:**
```go
func (c *Config) MigrateUp(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string)) error {
    return c.MigrateUpWithMode(ctx, txOpts, schema, logFilename, DbTxnModeAll, false)
}
```

**Verification:** `go build ./...`

---

### Step 2.10: Add locking to `MigrateDownWithMode`

**File:** `lib.go`

**Change `MigrateDownWithMode` signature and add locking:**
```go
func (c *Config) MigrateDownWithMode(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), downStep int, mode DbTxnMode, noLock bool) error {
    // Acquire lock
    conn, err := c.acquireLock(ctx, schema, noLock, func(s string) { logFilename(s) })
    if err != nil {
        return err
    }
    defer c.releaseLock(ctx, conn, schema)

    // ... rest of existing implementation unchanged ...
```

**Update `MigrateDown` to pass noLock=false:**
```go
func (c *Config) MigrateDown(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), downStep int) error {
    return c.MigrateDownWithMode(ctx, txOpts, schema, logFilename, downStep, DbTxnModeAll, false)
}
```

**Verification:** `go build ./...`

---

### Step 2.11: Add `-no-lock` flag to CLI

**File:** `cmd/dbmigrate/main.go`

**Add variable declaration (in the var block):**
```go
noLock        bool
```

**Add flag definition (before `flag.Parse()`):**
```go
flag.BoolVar(&noLock,
    "no-lock", false, "disable cross-process locking (required for SQLite)")
```

**Update migrate up section:**
```go
if doMigrateUp {
    mode, err := dbmigrate.ParseDbTxnMode(dbTxnMode)
    if err != nil {
        return err
    }
    return m.MigrateUpWithMode(ctx, &sql.TxOptions{}, dbSchema, filenameLogger("[up]"), mode, noLock)
}
```

**Update migrate down section:**
```go
if doMigrateDown > 0 {
    mode, err := dbmigrate.ParseDbTxnMode(dbTxnMode)
    if err != nil {
        return err
    }
    return m.MigrateDownWithMode(ctx, &sql.TxOptions{}, dbSchema, filenameLogger("[down]"), doMigrateDown, mode, noLock)
}
```

**Verification:**
```bash
go build ./cmd/dbmigrate
./cmd/dbmigrate/dbmigrate -help  # should show -no-lock flag
```

---

### Step 2.12: Verify `make test` fails for SQLite (expected)

After implementing locking (Steps 2.1-2.11), run:

```bash
make test
```

**Expected results:**
- ✅ postgres: PASS (locking works automatically)
- ✅ mysql: PASS (locking works automatically)
- ✅ mariadb: PASS (locking works automatically)
- ❌ sqlite3: FAIL (error: "sqlite3 does not support cross-process locking")
- ✅ cql: PASS (locking disabled, same as SQLite but CQL adapter marks `SupportsLocking: false`)

This confirms:
1. PostgreSQL/MySQL backward compatibility is preserved
2. SQLite correctly requires `-no-lock` flag
3. The breaking change for SQLite users is working as designed

---

### Step 2.13: Fix SQLite tests with `-no-lock` flag

**File:** `tests/withdb.sh`

**Change line 52:**
```bash
# Before
env DATABASE_DRIVER=sqlite3 DBMIGRATE_OPT="" DATABASE_URL="./tests/sqlite3.db" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}

# After
env DATABASE_DRIVER=sqlite3 DBMIGRATE_OPT="-no-lock" DATABASE_URL="./tests/sqlite3.db" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
```

**Also update lines 40-45** to test the error case first, similar to how `-server-ready` and `-create-db` are tested:

```bash
    sqlite3)
    rm -f "./tests/sqlite3.db"
    # Test that -server-ready is not supported
    if env DATABASE_DRIVER=sqlite3 DBMIGRATE_OPT='-server-ready 60s' DATABASE_URL="./tests/sqlite3.db" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}; then
        fail "should not support -server-ready"
        exit 1
    else
        pass "should not support -server-ready"
    fi
    # Test that -create-db is not supported
    if env DATABASE_DRIVER=sqlite3 DBMIGRATE_OPT='-create-db' DATABASE_URL="./tests/sqlite3.db" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}; then
        fail "should not support -create-db"
        exit 1
    else
        pass "should not support -create-db"
    fi
    # Test that locking is not supported (requires -no-lock)
    if env DATABASE_DRIVER=sqlite3 DBMIGRATE_OPT="" DATABASE_URL="./tests/sqlite3.db" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}; then
        fail "should require -no-lock flag"
        exit 1
    else
        pass "should require -no-lock flag"
    fi
    # Run actual tests with -no-lock
    env DATABASE_DRIVER=sqlite3 DBMIGRATE_OPT="-no-lock" DATABASE_URL="./tests/sqlite3.db" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    rm -f "./tests/sqlite3.db"
    ;;
```

---

### Step 2.14: Update CQL tests for `-no-lock` flag

**File:** `tests/withdb.sh`

CQL (Cassandra) also doesn't support locking. Update the CQL case (around line 55-72):

```bash
    cql)
    docker run --rm -p ${PORT}:9042 -d --cidfile cid.txt cassandra
    # Test that -create-db is not supported
    if env DATABASE_DRIVER=cql DBMIGRATE_OPT='-server-ready 60s -create-db' DATABASE_URL="localhost:${PORT}?keyspace=${DB_NAME}" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}; then
        fail "should not support -create-db"
        exit 1
    else
        pass "should not support -create-db"
    fi
    docker logs --since 1m `cat cid.txt`
    until docker exec -t `cat cid.txt` cqlsh -e 'describe cluster' >/dev/null; do docker logs --since 1m `cat cid.txt`; echo waiting for cassandra; sleep 1; done
    until docker exec -t `cat cid.txt` cqlsh -e "CREATE KEYSPACE IF NOT EXISTS ${DB_NAME} WITH replication = {'class':'SimpleStrategy', 'replication_factor' : 1};"; do
        docker logs --since 1m `cat cid.txt`;
        fail "unexpected error pre-creating keyspace ${DB_NAME}; retrying..."
        sleep 1
    done
    # Test that locking is not supported (requires -no-lock)
    if env DATABASE_DRIVER=cql DBMIGRATE_OPT='-server-ready 60s' DATABASE_URL="localhost:${PORT}?keyspace=${DB_NAME}&timeout=3m" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}; then
        fail "should require -no-lock flag"
        exit 1
    else
        pass "should require -no-lock flag"
    fi
    # Run actual tests with -no-lock
    env DATABASE_DRIVER=cql DBMIGRATE_OPT='-server-ready 60s -no-lock' DATABASE_URL="localhost:${PORT}?keyspace=${DB_NAME}&timeout=3m" DB_MIGRATIONS_DIR=${DB_MIGRATIONS_DIR} bash ${TARGET_SCRIPT}
    finish
    ;;
```

---

### Step 2.15: Verify `make test` passes after fixes

```bash
make test
```

**Expected results:**
- ✅ postgres: PASS
- ✅ mysql: PASS
- ✅ mariadb: PASS
- ✅ sqlite3: PASS (now using `-no-lock`)
- ✅ cql: PASS (now using `-no-lock`)

### Phase 2 Completion Notes

**Implementation details:**
- PostgreSQL uses `pg_try_advisory_lock` / `pg_advisory_unlock` for locking
- MySQL uses `GET_LOCK` / `RELEASE_LOCK` for locking
- SQLite and CQL adapters marked as `SupportsLocking: false`
- Lock ID generated using CRC32 hash of database name, schema, and table name
- Locking uses `sql.Conn` to ensure lock is held on same connection throughout migration
- Added `-no-lock` flag to CLI for SQLite/CQL (required) and optionally for other databases

**Test verification:**
- SQLite tests first verify failure without `-no-lock`, then run with `-no-lock`
- CQL tests first verify failure without `-no-lock`, then run with `-no-lock`
- PostgreSQL and MySQL tests run with locking enabled (default)
- Tests serve as living documentation that sqlite3/cql require `-no-lock`
- All tests pass for sqlite3, postgres, mysql databases

---

## Phase 3: MySQL DDL Warning ✅ COMPLETE

### Step 3.1: Add MySQL DDL warning function

**File:** `lib.go` (after `validateDbTxnMode`)

**Add:**
```go
// warnMySQLDDL prints a warning about MySQL DDL limitations
func warnMySQLDDL(driverName string, log func(string)) {
    if driverName != "mysql" {
        return
    }
    log("Warning: MySQL does not support transactional DDL.")
    log("         DDL statements (CREATE, ALTER, DROP) commit implicitly.")
    log("         Transaction mode has limited effect on DDL-heavy migrations.")
}
```

**Verification:** `go build ./...`

---

### Step 3.2: Call warning in MigrateUpWithMode

**File:** `lib.go`

**Add call to `warnMySQLDDL` in `MigrateUpWithMode`, after acquiring lock:**
```go
func (c *Config) MigrateUpWithMode(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), mode DbTxnMode, noLock bool) error {
    // Acquire lock
    conn, err := c.acquireLock(ctx, schema, noLock, func(s string) { logFilename(s) })
    if err != nil {
        return err
    }
    defer c.releaseLock(ctx, conn, schema)

    // MySQL DDL warning
    warnMySQLDDL(c.driverName, logFilename)

    // ... rest unchanged ...
```

**Verification:** `go build ./...`

---

### Step 3.3: Call warning in MigrateDownWithMode

**File:** `lib.go`

**Add call to `warnMySQLDDL` in `MigrateDownWithMode`, after acquiring lock:**
```go
func (c *Config) MigrateDownWithMode(ctx context.Context, txOpts *sql.TxOptions, schema *string, logFilename func(string), downStep int, mode DbTxnMode, noLock bool) error {
    // Acquire lock
    conn, err := c.acquireLock(ctx, schema, noLock, func(s string) { logFilename(s) })
    if err != nil {
        return err
    }
    defer c.releaseLock(ctx, conn, schema)

    // MySQL DDL warning
    warnMySQLDDL(c.driverName, logFilename)

    // ... rest unchanged ...
```

**Verification:** `go build ./...`

---

### Step 3.4: Verify MySQL DDL warning in test output

The MySQL DDL warning is printed via `logFilename`, so it will appear in `make test` output for mysql/mariadb drivers.

**Verification:**

```bash
DATABASE_DRIVER=mysql bash -euxo pipefail tests/withdb.sh tests/scenario.sh 2>&1 | grep -q "MySQL does not support transactional DDL"
```

This confirms the warning is printed during MySQL migrations. No changes to test infrastructure needed—the warning is informational only and doesn't affect test pass/fail status.

**Final verification:**

```bash
make test
```

All drivers should pass, and MySQL/MariaDB output should include the DDL warning.

### Phase 3 Completion Notes

**Implementation details:**
- Added `warnMySQLDDL` function in lib.go
- Called in both `MigrateUpWithMode` and `MigrateDownWithMode` after acquiring lock
- Warning printed for mysql driver only (mariadb also uses mysql driver)

**Test verification:**
- MySQL tests verify warning is shown: `[PASS] mysql: DDL warning shown`
- PostgreSQL tests verify warning is NOT shown: `[PASS] postgres: DDL warning correctly NOT shown`
- Tests serve as living documentation that MySQL DDL warning is displayed

---

## Phase 4: Final Cleanup and Documentation

### Step 4.1: Update imports in lib.go

Ensure all new imports are present at the top of `lib.go`:
```go
import (
    "bytes"
    "context"
    "database/sql"
    "fmt"           // NEW
    "hash/crc32"    // NEW
    "io/fs"
    "io/ioutil"
    "net/url"
    "path/filepath"
    "sort"
    "strings"
    "time"

    "github.com/derekparker/trie"
    "github.com/pkg/errors"
)
```

---

### Step 4.2: Run full test suite

```bash
cd dbmigrate
go test ./...
go build ./...
go build ./cmd/dbmigrate
```

---

### Step 4.3: Update cql.go adapter

**File:** `cmd/dbmigrate/cql.go`

Add locking fields (Cassandra doesn't support advisory locks):
```go
SupportsLocking: false,
AcquireLock:     nil,
ReleaseLock:     nil,
```

---

## Summary of Changes

### lib.go

| Line Range | Change |
|------------|--------|
| 4-17 | Add `"fmt"`, `"hash/crc32"` imports |
| After 17 | Add `DbTxnMode` type, constants, `ParseDbTxnMode` |
| After 17 | Add `noDbTxnMarker`, `requiresNoTransaction` |
| After 17 | Add `DbTxnModeConflictError`, `LockingNotSupportedError` |
| After 17 | Add `validateDbTxnMode`, `warnMySQLDDL`, `generateLockID` |
| 71-76 | Add `driverName`, `databaseName` fields to Config |
| 84-124 | Extract databaseName in `New` function |
| After 127 | Add `DriverName()` getter |
| After 127 | Add `acquireLock`, `releaseLock` methods |
| 185-237 | Replace `MigrateUp` body with call to `MigrateUpWithMode` |
| After 237 | Add `MigrateUpWithMode`, `migrateUpAll`, `migrateUpPerFile`, `migrateUpNoTx` |
| 239-296 | Replace `MigrateDown` body with call to `MigrateDownWithMode` |
| After 296 | Add `MigrateDownWithMode`, `migrateDownAll`, `migrateDownPerFile`, `migrateDownNoTx` |
| 316-327 | Add `SupportsLocking`, `AcquireLock`, `ReleaseLock` to Adapter |
| 337-371 | Add locking implementation to postgres adapter |
| 372-397 | Add locking implementation to mysql adapter |

### cmd/dbmigrate/main.go

| Line Range | Change |
|------------|--------|
| 30-43 | Add `dbTxnMode`, `noLock` variable declarations |
| Before 67 | Add `-db-txn-mode` and `-no-lock` flag definitions |
| 157-159 | Update migrate up to use `MigrateUpWithMode` with mode and noLock |
| 162-165 | Update migrate down to use `MigrateDownWithMode` with mode and noLock |

### cmd/dbmigrate/sqlite3.go

| Line Range | Change |
|------------|--------|
| 16-27 | Add `SupportsLocking: false`, `AcquireLock: nil`, `ReleaseLock: nil` |

### cmd/dbmigrate/cql.go

| Line Range | Change |
|------------|--------|
| 17-41 | Add `SupportsLocking: false`, `AcquireLock: nil`, `ReleaseLock: nil` |

### tests/withdb.sh

| Line Range | Change |
|------------|--------|
| 38-54 | Update sqlite3 case: add test for `-no-lock` requirement, run tests with `-no-lock` |
| 55-72 | Update cql case: add test for `-no-lock` requirement, run tests with `-no-lock` |

### lib_test.go

| Addition | Description |
|----------|-------------|
| `TestParseDbTxnMode` | Tests mode parsing |
| `TestRequiresNoTransaction` | Tests filename marker detection |
| `TestDbTxnModeConflictError` | Tests error message format |
| `TestValidateDbTxnMode` | Tests mode validation |
| `TestLockingNotSupportedError` | Tests locking error message |
| `TestGenerateLockID` | Tests lock ID generation |

---

## Execution Order

1. **Step 1.1: Verify `make test` passes** (baseline before any changes)
2. Steps 1.2-1.5: Add types and helper functions (no behavior change)
3. Steps 1.6-1.7: Add Config fields (no behavior change)
4. Steps 1.8-1.11: Add new methods (no behavior change, existing methods unchanged)
5. Step 1.12: Wire up MigrateUp (behavior preserved via DbTxnModeAll default)
6. Step 1.13: Wire up MigrateDown (behavior preserved)
7. Step 1.14: Add CLI flag
8. Steps 2.1-2.5: Add locking infrastructure (no behavior change)
9. Step 2.6: Add error type
10. Steps 2.7-2.8: Add Config fields and methods
11. Steps 2.9-2.10: Add locking to migrate methods
12. Step 2.11: Add CLI flag
13. **Step 2.12: Verify `make test` fails for SQLite/CQL** (expected breaking change)
14. **Steps 2.13-2.14: Fix tests/withdb.sh** for SQLite and CQL with `-no-lock`
15. **Step 2.15: Verify `make test` passes** (all drivers)
16. Steps 3.1-3.3: Add MySQL warning
17. Step 3.4: Verify MySQL DDL warning in output
18. Steps 4.1-4.3: Final cleanup
