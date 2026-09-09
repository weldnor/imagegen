package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestRunGenHashProducesAcceptedHash verifies the -gen-hash output is a hash
// that bcrypt.CompareHashAndPassword accepts for the same password.
func TestRunGenHashProducesAcceptedHash(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = oldStdout }()

	const pw = "correct horse battery staple"
	if err := runGenHash(pw); err != nil {
		t.Fatalf("runGenHash: %v", err)
	}
	w.Close()

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	hash := strings.TrimSpace(string(buf[:n]))

	if !strings.HasPrefix(hash, "$2") {
		t.Fatalf("output %q is not a bcrypt hash", hash)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)); err != nil {
		t.Fatalf("bcrypt rejected the generated hash: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("wrong")); err == nil {
		t.Fatal("bcrypt accepted a wrong password against the generated hash")
	}
}

// TestGenHashCLI exercises the subcommand through the built binary.
func TestGenHashCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping binary build in -short mode")
	}
	bin := t.TempDir() + "/server"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "-gen-hash", "-password", "hunter2").CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	hash := strings.TrimSpace(string(out))
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("hunter2")); err != nil {
		t.Fatalf("CLI hash rejected: %v (out=%q)", err, hash)
	}
}
