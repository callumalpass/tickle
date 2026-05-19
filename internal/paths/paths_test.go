package paths

import (
	"path/filepath"
	"testing"
)

func TestExpandPathToken(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, "config")
	dataHome := filepath.Join(root, "data")
	t.Setenv("TICKLE_CONFIG_HOME", configHome)
	t.Setenv("TICKLE_DATA_HOME", dataHome)

	got, err := ExpandPathToken("@config/scripts/demo/run.sh")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(configHome, "scripts", "demo", "run.sh")
	if got != want {
		t.Fatalf("ExpandPathToken(@config) = %q, want %q", got, want)
	}

	got, err = ExpandPathToken("@data/runs/demo")
	if err != nil {
		t.Fatal(err)
	}
	want = filepath.Join(dataHome, "runs", "demo")
	if got != want {
		t.Fatalf("ExpandPathToken(@data) = %q, want %q", got, want)
	}

	got, err = ExpandPathToken("./relative")
	if err != nil {
		t.Fatal(err)
	}
	if got != "./relative" {
		t.Fatalf("relative path changed to %q", got)
	}
}
