package passdriver

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

var (
	// errNoSecretData indicates that there is not data associated with an id
	errNoSecretData = errors.New("no secret data with ID")

	// errNoSecretData indicates that there is secret data already associated with an id
	errSecretIDExists = errors.New("secret data with ID already exists")

	// errInvalidKey indicates that something about your key is wrong
	errInvalidKey = errors.New("invalid key")
)

type driverConfig struct {
	// Root contains the root directory where the secrets are stored
	Root string
	// KeyID contains the key id that will be used for encryption (i.e. user@domain.tld)
	KeyID string
}

func (cfg *driverConfig) ParseOpts(opts map[string]string) {
	if val, ok := opts["root"]; ok {
		cfg.Root = val
		cfg.findGpgID() // try to find a .gpg-id in the parent directories of Root
	}
	if val, ok := opts["key"]; ok {
		cfg.KeyID = val
	}
}

func defaultDriverConfig() *driverConfig {
	cfg := &driverConfig{}

	if home, err := os.UserHomeDir(); err == nil {
		defaultLocations := []string{
			filepath.Join(home, ".password-store"),
			filepath.Join(home, ".local/share/gopass/stores/root"),
		}
		for _, path := range defaultLocations {
			if stat, err := os.Stat(path); err != nil || stat.IsDir() {
				continue
			}
			cfg.Root = path
			bs, err := ioutil.ReadFile(filepath.Join(path, ".gpg-id"))
			if err != nil {
				continue
			}
			cfg.KeyID = string(bs)
			break
		}
	}

	return cfg
}

func (cfg *driverConfig) findGpgID() {
	path := cfg.Root
	for len(path) > 1 {
		if _, err := os.Stat(filepath.Join(path, ".gpg-id")); err == nil {
			bs, err := ioutil.ReadFile(filepath.Join(path, ".gpg-id"))
			if err != nil {
				continue
			}
			cfg.KeyID = string(bs)
			break
		}
		path = filepath.Dir(path)
	}
}

// Driver is the passdriver object
type Driver struct {
	driverConfig
}

// NewDriver creates a new secret driver.
func NewDriver(opts map[string]string) (*Driver, error) {
	cfg := defaultDriverConfig()
	cfg.ParseOpts(opts)

	driver := &Driver{
		driverConfig: *cfg,
	}

	return driver, nil
}

// List returns all secret IDs
func (d *Driver) List() (secrets []string, err error) {
	files, err := ioutil.ReadDir(d.Root)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read secret directory")
	}
	for _, f := range files {
		secrets = append(secrets, f.Name())
	}
	sort.Strings(secrets)
	return secrets, nil
}

// Lookup returns the bytes associated with a secret ID
func (d *Driver) Lookup(id string) ([]byte, error) {
	out := &bytes.Buffer{}
	key, err := d.getPath(id)
	if err != nil {
		return nil, err
	}
	if err := d.gpg(context.TODO(), nil, out, "--decrypt", key); err != nil {
		return nil, errors.Wrapf(errNoSecretData, id)
	}
	if out.Len() == 0 {
		return nil, errors.Wrapf(errNoSecretData, id)
	}
	return out.Bytes(), nil
}

// Store saves the bytes associated with an ID. An error is returned if the ID already exists
func (d *Driver) Store(id string, data []byte) error {
	if _, err := d.Lookup(id); err == nil {
		return errors.Wrap(errSecretIDExists, id)
	}
	in := bytes.NewReader(data)
	key, err := d.getPath(id)
	if err != nil {
		return err
	}
	return d.gpg(context.TODO(), in, nil, "--encrypt", "-r", d.KeyID, "-o", key)
}

// Delete removes the secret associated with the specified ID.  An error is returned if no matching secret is found.
func (d *Driver) Delete(id string) error {
	key, err := d.getPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(key); err != nil {
		return errors.Wrap(errNoSecretData, id)
	}
	return nil
}

func (d *Driver) gpg(ctx context.Context, in io.Reader, out io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "gpg", args...)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = ioutil.Discard
	return cmd.Run()
}

func (d *Driver) getPath(id string) (string, error) {
	path, err := filepath.Abs(filepath.Join(d.Root, id))
	if err != nil {
		return "", errInvalidKey
	}
	if !strings.HasPrefix(path, d.Root) {
		return "", errInvalidKey
	}
	return path, nil
}
