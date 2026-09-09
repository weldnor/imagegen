// Command server is the Imagen backend: it serves the static frontend, exposes
// the JSON API, proxies image generation to OpenRouter, and stores generated
// images per authenticated user.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	var (
		genHash     = flag.Bool("gen-hash", false, "read a password from stdin (or -password) and print its bcrypt hash, then exit")
		password    = flag.String("password", "", "password to hash with -gen-hash (if empty, read from stdin)")
		migrateOnly = flag.Bool("migrate-only", false, "apply database migrations and exit without serving")
	)
	flag.Parse()

	if *genHash {
		if err := runGenHash(*password); err != nil {
			fmt.Fprintln(os.Stderr, "gen-hash:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(*migrateOnly); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

// runGenHash prints a bcrypt hash for the given password (or one read from
// stdin) suitable for pasting into AUTH_USERS.
func runGenHash(password string) error {
	if password == "" {
		fmt.Fprint(os.Stderr, "Password: ")
		r := bufio.NewReader(os.Stdin)
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return fmt.Errorf("reading password: %w", err)
		}
		password = strings.TrimRight(line, "\r\n")
	}
	if password == "" {
		return fmt.Errorf("password is empty")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	fmt.Println(string(hash))
	return nil
}
