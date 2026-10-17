package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"mail-manager/internal/config"
	"mail-manager/internal/storage"
)

func main() {
	username := flag.String("username", "", "first administrator username")
	flag.Parse()
	if *username == "" {
		log.Fatal("usage: go run ./cmd/admin -username yourname < password-file-or-pipe")
	}
	fmt.Fprintln(os.Stderr, "Enter admin password on stdin (12-72 bytes):")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 128), 1024)
	if !scanner.Scan() {
		log.Fatal("password missing")
	}
	password := strings.TrimSuffix(scanner.Text(), "\r")
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := storage.Open(context.Background(), cfg.Database.Path)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(context.Background()); err != nil {
		log.Fatal(err)
	}
	if err = db.BootstrapAdmin(context.Background(), *username, password); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Initial administrator created. Remove password from stdin history and temporary files.")
}
