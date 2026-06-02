// Package assets embeds Rover's non-Go runtime assets (Azure scripts, Bicep
// templates, cloud-init, and the Ansible playbook) into the binary so an
// installed `rover` is fully self-contained — install.sh only fetches the
// binary, yet `rover up`/`provision` still have everything they need.
//
// At runtime the embedded tree is materialized into the user cache directory
// (once per version) and the scripts/playbook are executed from there.
package assets

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:scripts all:infra all:ansible
var Files embed.FS

// Dir returns a filesystem path containing the materialized asset tree
// (scripts/, infra/, ansible/). The embedded copy is extracted into the user
// cache directory keyed by version, so upgrades get fresh assets.
//
// Set ROVER_ASSET_DIR to point at a checkout for local development.
func Dir(version string) (string, error) {
	if override := os.Getenv("ROVER_ASSET_DIR"); override != "" {
		return override, nil
	}

	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "rover", "assets-"+version)
	marker := filepath.Join(dir, ".materialized")
	if _, err := os.Stat(marker); err == nil {
		return dir, nil
	}

	if err := extract(dir); err != nil {
		return "", err
	}
	if err := os.WriteFile(marker, []byte(version), 0o644); err != nil {
		return "", err
	}
	return dir, nil
}

func extract(dir string) error {
	return fs.WalkDir(Files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dest := filepath.Join(dir, path)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := Files.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		// Scripts are invoked via `bash <path>`, but make them executable too
		// so the materialized tree is usable directly.
		mode := fs.FileMode(0o644)
		if filepath.Dir(path) == filepath.Join("scripts", "azure") {
			mode = 0o755
		}
		return os.WriteFile(dest, data, mode)
	})
}
