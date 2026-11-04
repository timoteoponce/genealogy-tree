// Command familytree is a self-contained family tree web application.
//
// It is a single statically-linked binary (SQLite is pure Go, all assets
// are embedded) with the following subcommands:
//
//	familytree serve                       run the web server (default)
//	familytree version                     print build information
//
// Additional subcommands are added in later stages (admin-create, import,
// backup, healthcheck).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"genealogy-tree/internal/auth"
	"genealogy-tree/internal/config"
	"genealogy-tree/internal/db"
	"genealogy-tree/internal/gedcom"
	"genealogy-tree/internal/store"
	"genealogy-tree/internal/web"
)

// buildVersion is set with -ldflags at build time.
var buildVersion = "dev"

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "serve":
		err = runServe()
	case "admin-create":
		err = runAdminCreate(os.Args[2:])
	case "import":
		err = runImport(os.Args[2:])
	case "backup":
		err = runBackup(os.Args[2:])
	case "healthcheck":
		err = runHealthcheck(os.Args[2:])
	case "version":
		fmt.Println("familytree", buildVersion)
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		fmt.Fprintln(os.Stderr, "usage: familytree [serve|admin-create|import|backup|healthcheck|version]")
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
}

// runImport replaces the tree with a GEDCOM file:
// runBackup writes an online, consistent copy of the database:
//
//	familytree backup /var/backups/familytree-2026-11-04.db
func runBackup(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: familytree backup <destination.db>")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	d, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer d.Close()

	size, err := db.BackupTo(d, fs.Arg(0))
	if err != nil {
		return err
	}
	fmt.Printf("backed up %s → %s (%d bytes)\n", cfg.DBPath, fs.Arg(0), size)
	return nil
}

// runHealthcheck exits 0 when the server answers /healthz on localhost
// (used by the Docker HEALTHCHECK), 1 otherwise.
func runHealthcheck(args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ExitOnError)
	port := fs.Int("port", 8080, "port the server listens on")
	if err := fs.Parse(args); err != nil {
		return err
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", *port))
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		fmt.Println("healthy")
		return nil
	}
	return fmt.Errorf("unhealthy: status %d", resp.StatusCode)
}

// runImport replaces the tree with a GEDCOM file:
//
//	familytree import family_tree.ged
func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: familytree import <file.ged>")
	}
	path := fs.Arg(0)

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	d, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer d.Close()

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	ctx := context.Background()
	rep, err := gedcom.Import(ctx, store.New(d), f, filepath.Base(path))
	if err != nil {
		return err
	}
	w, _ := json.MarshalIndent(rep.Warnings, "", "  ")
	fmt.Printf("imported %s:\n  people: %d\n  families: %d\n  sources: %d\n  events: %d\n  media refs: %d\n  warnings:\n%s\n",
		path, rep.People, rep.Families, rep.Sources, rep.Events, rep.Media, w)
	return nil
}

// runAdminCreate creates the initial admin (or any-role) account from the
// command line: familytree admin-create -u admin -p 'LongPassword'
func runAdminCreate(args []string) error {
	fs := flag.NewFlagSet("admin-create", flag.ExitOnError)
	username := fs.String("u", "", "username (required)")
	password := fs.String("p", "", "password (required)")
	role := fs.String("role", "admin", "role: admin|editor|viewer")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" || *password == "" {
		return fmt.Errorf("both -u (username) and -p (password) are required")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	d, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer d.Close()
	q := store.New(d).Queries()
	ctx := context.Background()

	if n, err := q.CountUsers(ctx); err != nil {
		return err
	} else if n == 0 && *role != "admin" {
		log.Printf("this is the first account; forcing role=admin")
		*role = "admin"
	}

	hash, err := auth.HashPassword(*password)
	if err != nil {
		return err
	}
	if _, err := q.CreateUser(ctx, *username, hash, *role); err != nil {
		return err
	}
	if err := q.InsertAudit(ctx, q.DB, nil, "admin-create", "user", nil, *username); err != nil {
		return err
	}
	fmt.Printf("created user %q with role %q\n", *username, *role)
	return nil
}

func runServe() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir %s: %w", cfg.DataDir, err)
	}

	d, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer d.Close()

	webServer, err := web.NewServer(cfg, store.New(d))
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           webServer.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown on Ctrl-C / SIGTERM (docker stop).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.ListenAndServe() }()

	log.Printf("familytree %s listening on :%d (data dir %s)", buildVersion, cfg.Port, cfg.DataDir)
	select {
	case err := <-srvErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Println("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
	}
	return nil
}
