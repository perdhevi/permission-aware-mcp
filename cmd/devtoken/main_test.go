package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestKeygenRefusesOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")
	if err := run([]string{"keygen", "--dir", dir}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "private.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jwt.ParseRSAPrivateKeyFromPEM(before); err != nil {
		t.Fatal(err)
	}
	public, err := os.ReadFile(filepath.Join(dir, "public.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jwt.ParseRSAPublicKeyFromPEM(public); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"keygen", "--dir", dir}); err == nil {
		t.Fatal("overwrote existing keys")
	}
	after, err := os.ReadFile(filepath.Join(dir, "private.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("private key changed")
	}
}
