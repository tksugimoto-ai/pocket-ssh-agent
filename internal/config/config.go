// Package config persists the pairing between this PC and the phone.
package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Pairing is the state shared with one paired phone.
type Pairing struct {
	// PublicKey is the phone's SSH public key in authorized_keys format.
	PublicKey string `json:"publicKey"`
	// Secret is the base64url-encoded key shared with the phone. Both sides
	// authenticate their messages with HMAC-SHA256 under it, so the secret
	// itself never travels after pairing.
	Secret     string    `json:"secret"`
	DeviceName string    `json:"deviceName,omitempty"`
	PairedAt   time.Time `json:"pairedAt"`
}

type file struct {
	Pairing *Pairing `json:"pairing,omitempty"`
}

// Store is a concurrency-safe, file-backed configuration.
type Store struct {
	path string

	mu      sync.RWMutex
	pairing *Pairing
	pub     ssh.PublicKey
}

// DefaultPath returns the default location of the configuration file.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pocket-ssh-agent", "config.json"), nil
}

// Open loads the configuration at path. A missing file is not an error.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if f.Pairing != nil {
		pub, err := parsePublicKey(f.Pairing.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		s.pairing, s.pub = f.Pairing, pub
	}
	return s, nil
}

// Path returns the file location.
func (s *Store) Path() string { return s.path }

// Pairing returns the current pairing and its parsed key, or nil.
func (s *Store) Pairing() (*Pairing, ssh.PublicKey) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.pairing == nil {
		return nil, nil
	}
	p := *s.pairing
	return &p, s.pub
}

// SetPairing replaces the pairing and writes the file.
func (s *Store) SetPairing(p *Pairing) error {
	pub, err := parsePublicKey(p.PublicKey)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(file{Pairing: p}, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(s.path, b); err != nil {
		return err
	}
	s.mu.Lock()
	cp := *p
	s.pairing, s.pub = &cp, pub
	s.mu.Unlock()
	return nil
}

// Secret returns the key shared with the paired phone, or nil.
func (s *Store) Secret() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.pairing == nil {
		return nil
	}
	k, err := base64.RawURLEncoding.DecodeString(s.pairing.Secret)
	if err != nil || len(k) < 32 {
		return nil
	}
	return k
}

func parsePublicKey(s string) (ssh.PublicKey, error) {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(s))
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}
	if pub.Type() != ssh.KeyAlgoED25519 {
		return nil, fmt.Errorf("unsupported key type %q (only %s)", pub.Type(), ssh.KeyAlgoED25519)
	}
	return pub, nil
}

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return err
	}
	// os.CreateTemp creates the file with mode 0600 on Unix. On Windows the
	// ACL of the user's profile directory applies.
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
