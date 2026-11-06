// sign-update refreshes MyGo's archive signature after P12 signing changes a
// macOS bundle. The archive is SHA-256 hashed, then the digest is Ed25519 signed,
// matching the pinned MyGo internal/update format (not a signature of the JSON).
package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	Slug    string `json:"slug"`
	Version string `json:"version"`
	Updates struct {
		PublicKey string `json:"publicKey"`
		GitHub    string `json:"github"`
		Deltas    *int   `json:"deltas"`
	} `json:"updates"`
}

func main() {
	check := flag.Bool("check", false, "only validate the configured update key pair")
	manifest := flag.String("manifest", "", "MyGo-generated update-darwin-<arch>.json")
	archive := flag.String("archive", "", "archive of the final P12-signed app")
	flag.Parse()
	if err := run(*check, *manifest, *archive); err != nil {
		fmt.Fprintln(os.Stderr, "sign-update:", err)
		os.Exit(1)
	}
}

func run(check bool, manifest, archive string) error {
	data, err := os.ReadFile("mygo.json")
	if err != nil {
		return err
	}
	var c config
	if err := json.Unmarshal(data, &c); err != nil {
		return err
	}
	public, err := base64.StdEncoding.DecodeString(strings.TrimSpace(c.Updates.PublicKey))
	if err != nil || len(public) != ed25519.PublicKeySize {
		return errors.New("invalid updates.publicKey")
	}
	secret, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("MYGO_UPDATER_PRIVATE_KEY")))
	if err != nil || len(secret) != ed25519.PrivateKeySize {
		return errors.New("missing or invalid MYGO_UPDATER_PRIVATE_KEY")
	}
	key := ed25519.NewKeyFromSeed(secret[:ed25519.SeedSize])
	if !bytes.Equal(key, secret) || !bytes.Equal(key.Public().(ed25519.PublicKey), public) {
		return errors.New("update private key does not match the configured public key")
	}
	if c.Updates.Deltas == nil || *c.Updates.Deltas != 0 {
		return errors.New("updates.deltas must be 0: macOS archives change after the MyGo build")
	}
	if check {
		return nil
	}
	target := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(manifest), "update-"), ".json")
	if target != "darwin-arm64" && target != "darwin-amd64" {
		return errors.New("expected a macOS update manifest")
	}
	name := c.Slug + "-" + c.Version + "-" + target + ".tar.gz"
	if filepath.Base(archive) != name {
		return errors.New("archive does not match the configured version and target")
	}
	data, err = os.ReadFile(manifest)
	if err != nil {
		return err
	}
	// Keep the full-update fields of the pinned MyGo manifest. Deltas and
	// previous archives are intentionally omitted after P12 repackaging.
	var m struct {
		Version   string `json:"version"`
		Notes     string `json:"notes,omitempty"`
		Date      string `json:"date,omitempty"`
		URL       string `json:"url"`
		Size      int64  `json:"size"`
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	expectedURL := "https://github.com/" + c.Updates.GitHub + "/releases/download/v" + url.PathEscape(c.Version) + "/" + url.PathEscape(name)
	if m.Version != c.Version || m.URL != expectedURL {
		return errors.New("manifest version or download URL does not match mygo.json")
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	digest := sha256.New()
	size, err := io.Copy(digest, f)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if size == 0 || size > 1<<30 {
		return errors.New("archive size is outside MyGo updater limits")
	}
	m.Size = size
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, digest.Sum(nil)))
	data, err = json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(manifest), ".update-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(append(data, '\n'))
	closeErr = tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), manifest)
}
