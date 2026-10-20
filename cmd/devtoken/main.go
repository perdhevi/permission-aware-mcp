package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"permission-aware-mcp/internal/authn"
	"permission-aware-mcp/internal/config"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: devtoken keygen | mint --subject alice")
	}
	switch args[0] {
	case "keygen":
		fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
		dir := fs.String("dir", ".local/keys", "key directory")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("unexpected arguments")
		}
		privatePath, publicPath := filepath.Join(*dir, "private.pem"), filepath.Join(*dir, "public.pem")
		for _, path := range []string{privatePath, publicPath} {
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				return errors.New("key file already exists or is inaccessible; refusing to overwrite")
			}
		}
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return err
		}
		pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(*dir, 0700); err != nil {
			return err
		}
		if err := writeExclusive(privatePath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})); err != nil {
			return err
		}
		if err := writeExclusive(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "development keys created in", *dir)
		return nil
	case "mint":
		c, err := config.Load()
		if err != nil {
			return err
		}
		if c.AuthMode != "dev" {
			return errors.New("devtoken mint requires AUTH_MODE=dev; use cmd/login for OIDC")
		}
		fs := flag.NewFlagSet("mint", flag.ContinueOnError)
		subject := fs.String("subject", "", "required external subject")
		keyPath := fs.String("key", ".local/keys/private.pem", "private key")
		ttl := fs.Duration("ttl", time.Hour, "token lifetime")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *subject == "" || *ttl <= 0 || fs.NArg() != 0 {
			return errors.New("mint requires --subject and a positive --ttl")
		}
		b, err := os.ReadFile(*keyPath)
		if err != nil {
			return err
		}
		key, err := jwt.ParseRSAPrivateKeyFromPEM(b)
		if err != nil {
			return errors.New("invalid private key")
		}
		now := time.Now()
		token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, authn.Claims{RegisteredClaims: jwt.RegisteredClaims{Issuer: c.Issuer, Subject: *subject, Audience: jwt.ClaimStrings{c.Audience}, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(*ttl))}, Scope: c.RequiredScope}).SignedString(key)
		if err != nil {
			return err
		}
		fmt.Println(token)
		return nil
	default:
		return errors.New("unknown command; use keygen or mint")
	}
}
func writeExclusive(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(b)
	closeErr := f.Close()
	return errors.Join(writeErr, closeErr)
}
