package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"filippo.io/age"

	"github.com/cdescobedo/envctl/internal/fsutil"
)

// File is an age-encrypted store for machines with no OS keyring: containers,
// CI, WSL, or a plain SSH session with no D-Bus.
//
// Unlike the platform backends this one holds every secret in a single
// encrypted document, because a per-item file would leak the inventory through
// filenames and multiply the decryption cost.
type File struct {
	path       string
	passphrase string
}

// NewFile returns a file-backed store at path, encrypted under passphrase.
func NewFile(path, passphrase string) *File {
	return &File{path: path, passphrase: passphrase}
}

// load decrypts and parses the whole document. Every call pays the scrypt KDF
// cost in full, since there is no per-item access: the document is one blob.
func (f *File) load() (map[string]string, error) {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", f.path, err)
	}

	id, err := age.NewScryptIdentity(f.passphrase)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	r, err := age.Decrypt(bytes.NewReader(data), id)
	if err != nil {
		return nil, fmt.Errorf("decrypt %s (wrong passphrase?): %w", f.path, err)
	}
	plain, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read decrypted %s: %w", f.path, err)
	}

	items := map[string]string{}
	if err := json.Unmarshal(plain, &items); err != nil {
		return nil, fmt.Errorf("parse decrypted %s: %w", f.path, err)
	}
	return items, nil
}

// save re-encrypts and writes the whole document. Every mutation (a single
// Set or Delete) re-encrypts the entire set of items, so the scrypt KDF cost
// is paid again on every call, the same as load.
func (f *File) save(items map[string]string) error {
	// Sorted keys keep the plaintext stable, so re-encrypting an unchanged set
	// does not produce a gratuitously different file.
	keys := make([]string, 0, len(items))
	for k := range items {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]string, len(items))
	for _, k := range keys {
		ordered[k] = items[k]
	}
	plain, err := json.Marshal(ordered)
	if err != nil {
		return fmt.Errorf("encode items: %w", err)
	}

	rec, err := age.NewScryptRecipient(f.passphrase)
	if err != nil {
		return fmt.Errorf("derive key: %w", err)
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, rec)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	if _, err := w.Write(plain); err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}

	// The secrets directory itself is created 0700, tighter than the 0755
	// fsutil.WriteAtomic would use on its own: this file is the only copy of
	// every secret it holds, unlike a rendered .env that can be regenerated.
	// MkdirAll is a no-op once the directory exists, so this does not fight
	// with WriteAtomic's own (looser) MkdirAll below it.
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(f.path), err)
	}
	// A crash or a killed process mid-write must not destroy the only copy of
	// these secrets, so this goes through the same temp-file-then-rename
	// primitive render uses for rendered .env files, rather than truncating
	// f.path in place.
	if err := fsutil.WriteAtomic(f.path, buf.Bytes()); err != nil {
		return fmt.Errorf("write %s: %w", f.path, err)
	}
	return nil
}

func (f *File) Has(service string) (bool, error) {
	items, err := f.load()
	if err != nil {
		return false, err
	}
	_, ok := items[service]
	return ok, nil
}

func (f *File) Get(service string) (string, error) {
	items, err := f.load()
	if err != nil {
		return "", err
	}
	v, ok := items[service]
	if !ok {
		return "", fmt.Errorf("%s: %w", service, ErrNotFound)
	}
	return v, nil
}

func (f *File) Set(service, value string) error {
	items, err := f.load()
	if err != nil {
		return err
	}
	items[service] = value
	return f.save(items)
}

func (f *File) Delete(service string) error {
	items, err := f.load()
	if err != nil {
		return err
	}
	if _, ok := items[service]; !ok {
		return fmt.Errorf("%s: %w", service, ErrNotFound)
	}
	delete(items, service)
	return f.save(items)
}
