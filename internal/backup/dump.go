package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DumpInfo is what a dump reports about the database it read.
type DumpInfo struct {
	PgDumpVersion   string
	PostgresVersion string
	DatabaseName    string
}

// Dumper writes a database dump and reads the table inventory the completeness
// guard and the manifest need. A test injects a fake; the server wires
// PostgresDumper.
type Dumper interface {
	// Dump writes a pg_dump custom-format archive of the database to w.
	Dump(ctx context.Context, w io.Writer) (DumpInfo, error)
	// Inventory reports each core table's presence and row count in the live
	// database. An empty result means the caller requires no inventory.
	Inventory(ctx context.Context) ([]TableCount, error)
}

// Restorer replaces the current database with an archive read from r.
type Restorer interface {
	Restore(ctx context.Context, r io.Reader) error
}

var (
	// ErrToolMissing reports that pg_dump or pg_restore is not installed.
	ErrToolMissing = errors.New("backup: pg_dump and pg_restore are not installed on this server")
	// ErrToolTooOld reports a pg_dump older than the database server.
	ErrToolTooOld = errors.New("backup: pg_dump is older than the database server")
)

// Tools are the PostgreSQL client programs a backup and a restore run.
type Tools struct {
	PgDump    string
	PgRestore string
	// Psql reads the table inventory. Optional.
	Psql string
	// Major is pg_dump's major version; Version is its full version string.
	Major   int
	Version string
}

// pgLibDirs is where Debian's PostgreSQL packages install each major version's
// programs.
const pgLibDirs = "/usr/lib/postgresql"

var versionPattern = regexp.MustCompile(`\(PostgreSQL\)\s+(\d+)(?:\.(\d+))?`)

// FindTools picks the newest pg_dump installed, with the pg_restore and psql
// beside it: a versioned Debian directory first, then PATH. Several majors can
// be installed side by side and the PATH wrapper may not pick the newest.
func FindTools(ctx context.Context) (Tools, error) {
	var dirs []string
	if matches, err := filepath.Glob(filepath.Join(pgLibDirs, "*", "bin")); err == nil {
		dirs = append(dirs, matches...)
	}
	if p, err := exec.LookPath("pg_dump"); err == nil {
		dirs = append(dirs, filepath.Dir(p))
	}
	var best Tools
	for _, dir := range dirs {
		dump, restore := filepath.Join(dir, "pg_dump"), filepath.Join(dir, "pg_restore")
		if !executable(dump) || !executable(restore) {
			continue
		}
		major, version, err := toolVersion(ctx, dump)
		if err != nil {
			continue
		}
		if major > best.Major {
			psql := filepath.Join(dir, "psql")
			if !executable(psql) {
				psql = ""
			}
			best = Tools{PgDump: dump, PgRestore: restore, Psql: psql, Major: major, Version: version}
		}
	}
	if best.PgDump == "" {
		return Tools{}, ErrToolMissing
	}
	return best, nil
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func toolVersion(ctx context.Context, path string) (int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output() //nolint:gosec // a discovered path with a fixed flag
	if err != nil {
		return 0, "", err
	}
	match := versionPattern.FindStringSubmatch(string(out))
	if match == nil {
		return 0, "", fmt.Errorf("unrecognized version output %q", strings.TrimSpace(string(out)))
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, "", err
	}
	return major, strings.TrimSpace(string(out)), nil
}

// PostgresDumper is the real Dumper: it runs pg_dump and psql from the host.
type PostgresDumper struct {
	DatabaseURL string
	Tools       Tools
	// Tables is the core inventory the manifest records and the completeness
	// guard checks. Empty means no inventory.
	Tables []string
}

// NewPostgresDumper discovers the client tools and returns a dumper for
// databaseURL, recording the presence and row count of tables.
func NewPostgresDumper(ctx context.Context, databaseURL string, tables []string) (*PostgresDumper, error) {
	tools, err := FindTools(ctx)
	if err != nil {
		return nil, err
	}
	return &PostgresDumper{DatabaseURL: databaseURL, Tools: tools, Tables: tables}, nil
}

// Dump streams a custom-format, zstd-compressed archive into w. The password,
// when the URL carries one, goes to the environment, never the command line.
func (d *PostgresDumper) Dump(ctx context.Context, w io.Writer) (DumpInfo, error) {
	conn, env, err := libpqTarget(d.DatabaseURL)
	if err != nil {
		return DumpInfo{}, err
	}
	args := []string{"--format=custom", "--compress=zstd:3", "--no-password", "--dbname=" + conn}
	cmd := exec.CommandContext(ctx, d.Tools.PgDump, args...) //nolint:gosec // G204: the discovered pg_dump with arguments built here, no shell
	cmd.Env = append(os.Environ(), env...)
	stderr := &limitedBuffer{limit: 64 << 10}
	cmd.Stderr = stderr
	cmd.Stdout = w
	if err := cmd.Run(); err != nil {
		return DumpInfo{}, fmt.Errorf("pg_dump failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return DumpInfo{PgDumpVersion: d.Tools.Version, DatabaseName: databaseName(d.DatabaseURL)}, nil
}

// Inventory reads each core table's presence and exact row count with psql.
func (d *PostgresDumper) Inventory(ctx context.Context) ([]TableCount, error) {
	if len(d.Tables) == 0 {
		return nil, nil
	}
	if d.Tools.Psql == "" {
		return nil, errors.New("backup: psql is needed to read the table inventory but was not found")
	}
	conn, env, err := libpqTarget(d.DatabaseURL)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, d.Tools.Psql, //nolint:gosec // G204: the discovered psql with arguments built here, no shell
		"--no-psqlrc", "-X", "-A", "-t", "-F", "\t", "--dbname="+conn, "-c", coreInventorySQL(d.Tables))
	cmd.Env = append(os.Environ(), env...)
	out := &limitedBuffer{limit: 1 << 20}
	stderr := &limitedBuffer{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = out, stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("reading the table inventory: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseInventory(out.String())
}

// PostgresRestorer is the real Restorer: pg_restore replaces the configured
// database with the archive.
type PostgresRestorer struct {
	DatabaseURL string
	Tools       Tools
}

// NewPostgresRestorer discovers the client tools and returns a restorer for
// databaseURL.
func NewPostgresRestorer(ctx context.Context, databaseURL string) (*PostgresRestorer, error) {
	tools, err := FindTools(ctx)
	if err != nil {
		return nil, err
	}
	return &PostgresRestorer{DatabaseURL: databaseURL, Tools: tools}, nil
}

// Restore runs pg_restore against the database, dropping the objects the archive
// names first (--clean --if-exists), without ownership or grants. It reads the
// archive from stdin, so nothing is spooled to disk.
func (r *PostgresRestorer) Restore(ctx context.Context, in io.Reader) error {
	conn, env, err := libpqTarget(r.DatabaseURL)
	if err != nil {
		return err
	}
	args := []string{
		"--clean", "--if-exists", "--no-owner", "--no-privileges", "--no-password",
		"--exit-on-error", "--dbname=" + conn,
	}
	cmd := exec.CommandContext(ctx, r.Tools.PgRestore, args...) //nolint:gosec // G204: the discovered pg_restore with arguments built here, no shell
	cmd.Env = append(os.Environ(), env...)
	stderr := &limitedBuffer{limit: 64 << 10}
	cmd.Stderr = stderr
	cmd.Stdin = in
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_restore failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// libpqTarget turns a DATABASE_URL into what pg_dump and pg_restore accept,
// moving a password out of the command line (where ps shows it) into PGPASSWORD.
func libpqTarget(databaseURL string) (conn string, env []string, err error) {
	if !strings.Contains(databaseURL, "://") {
		return databaseURL, nil, nil
	}
	u, err := url.Parse(databaseURL)
	if err != nil {
		return "", nil, fmt.Errorf("parsing the database URL: %w", err)
	}
	if u.User != nil {
		if password, ok := u.User.Password(); ok {
			env = append(env, "PGPASSWORD="+password)
			u.User = url.User(u.User.Username())
		}
	}
	return u.String(), env, nil
}

// databaseName is the database a URL names, read from its path.
func databaseName(databaseURL string) string {
	if !strings.Contains(databaseURL, "://") {
		return ""
	}
	u, err := url.Parse(databaseURL)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

// coreInventorySQL builds one query that reports each table's presence and row
// count. to_regclass keeps an absent table from failing the query; the CASE
// keeps the count from running on a table that is not there.
func coreInventorySQL(tables []string) string {
	parts := make([]string, 0, len(tables))
	for _, t := range tables {
		if !validIdent(t) {
			continue
		}
		reg := "to_regclass(" + quoteLiteral("public."+t) + ")"
		parts = append(parts, fmt.Sprintf(
			"SELECT %s AS name, %s IS NOT NULL AS present, CASE WHEN %s IS NOT NULL THEN (SELECT count(*) FROM public.%s) ELSE 0 END AS rows",
			quoteLiteral(t), reg, reg, t))
	}
	return strings.Join(parts, " UNION ALL ")
}

func validIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != '_' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func quoteLiteral(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `''`).Replace(s) + "'"
}

// parseInventory reads psql's unaligned tab-separated rows: name, t or f, rows.
func parseInventory(out string) ([]TableCount, error) {
	var tables []TableCount
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, fmt.Errorf("backup: unexpected inventory row %q", line)
		}
		rows, err := strconv.ParseInt(strings.TrimSpace(fields[2]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("backup: unexpected row count %q", fields[2])
		}
		tables = append(tables, TableCount{
			Name:    fields[0],
			Present: strings.TrimSpace(fields[1]) == "t",
			Rows:    rows,
		})
	}
	return tables, nil
}

// limitedBuffer keeps the first limit bytes written and discards the rest.
type limitedBuffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.limit - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
