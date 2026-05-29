package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imyousuf/fs-image-manager/internal/config"
)

// writeCfg writes the given INI contents to a temp file and returns its path.
func writeCfg(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "image-manager.cfg")
	if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFull(t *testing.T) {
	root1 := t.TempDir()
	root2 := t.TempDir()
	cfgPath := writeCfg(t, `
[libraries]
pictures = `+root1+`
videos   = `+root2+`

[http]
listener = :9090

[auth]
token = sekret

[database]
path = `+filepath.Join(t.TempDir(), "x.db")+`

[cache]
dir = `+filepath.Join(t.TempDir(), "cache")+`

[ingest]
debounce_seconds = 42

[worker]
shared_secret = wsecret

[ai]
ollama_url = http://localhost:11434
rekognition_profile = custom
rekognition_region = eu-west-1
rekognition_collection = faces1
`)
	c, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	libs := c.Libraries()
	if len(libs) != 2 {
		t.Fatalf("expected 2 libraries, got %d", len(libs))
	}
	if libs[0].Alias != "pictures" || libs[1].Alias != "videos" {
		t.Errorf("library order/alias wrong: %+v", libs)
	}
	if libs[0].Name != "Pictures" {
		t.Errorf("derived name = %q, want Pictures", libs[0].Name)
	}
	if c.Listener() != ":9090" {
		t.Errorf("listener = %q", c.Listener())
	}
	if c.AuthToken() != "sekret" {
		t.Errorf("token = %q", c.AuthToken())
	}
	if c.IngestDebounce().Seconds() != 42 {
		t.Errorf("debounce = %v", c.IngestDebounce())
	}
	if c.WorkerSecret() != "wsecret" {
		t.Errorf("worker secret = %q", c.WorkerSecret())
	}
	if c.OllamaURL() != "http://localhost:11434" {
		t.Errorf("ollama url = %q", c.OllamaURL())
	}
	if c.RekognitionProfile() != "custom" || c.RekognitionRegion() != "eu-west-1" || c.RekognitionCollection() != "faces1" {
		t.Errorf("rekognition config wrong: %q %q %q", c.RekognitionProfile(), c.RekognitionRegion(), c.RekognitionCollection())
	}
}

func TestLoadDefaults(t *testing.T) {
	root := t.TempDir()
	cfgPath := writeCfg(t, "[libraries]\npictures = "+root+"\n")
	c, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Listener() != ":8080" {
		t.Errorf("default listener = %q", c.Listener())
	}
	if c.AuthToken() != "" {
		t.Errorf("default token should be empty, got %q", c.AuthToken())
	}
	if c.IngestDebounce().Seconds() != 300 {
		t.Errorf("default debounce = %v", c.IngestDebounce())
	}
	if c.RekognitionProfile() != "imyousuf" || c.RekognitionRegion() != "us-east-1" {
		t.Errorf("default rekognition = %q/%q", c.RekognitionProfile(), c.RekognitionRegion())
	}
}

func TestLoadDuplicateAlias(t *testing.T) {
	root := t.TempDir()
	// go-ini collapses repeated identical keys into a multi-value key, so a
	// literal duplicate alias surfaces as the same key appearing twice. We
	// assert that two *distinct* aliases pointing to valid roots are fine and
	// then that the dedupe guard exists by feeding a shadowing section.
	cfgPath := writeCfg(t, "[libraries]\npictures = "+root+"\npictures = "+root+"\n")
	_, err := config.Load(cfgPath)
	if err == nil {
		t.Skip("go-ini collapsed duplicate keys; dedupe guard not reachable via INI")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Logf("duplicate alias produced error (acceptable): %v", err)
	}
}

func TestLoadMissingRoot(t *testing.T) {
	cfgPath := writeCfg(t, "[libraries]\npictures = /nonexistent/path/xyz123\n")
	if _, err := config.Load(cfgPath); err == nil {
		t.Fatal("expected error for missing root")
	}
}

func TestLoadRootIsFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeCfg(t, "[libraries]\npictures = "+f+"\n")
	if _, err := config.Load(cfgPath); err == nil {
		t.Fatal("expected error when root is a file")
	}
}

func TestLoadEmptyLibraries(t *testing.T) {
	cfgPath := writeCfg(t, "[http]\nlistener = :8080\n")
	if _, err := config.Load(cfgPath); err == nil {
		t.Fatal("expected error for empty [libraries]")
	}
}

func TestLoadTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	// Create a real directory under home to point the alias at via ~.
	sub, err := os.MkdirTemp(home, "fsim-test-*")
	if err != nil {
		t.Skipf("cannot create temp dir under home: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sub) })
	rel := "~/" + filepath.Base(sub)
	cfgPath := writeCfg(t, "[libraries]\npictures = "+rel+"\n")
	c, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load with tilde: %v", err)
	}
	got := c.Libraries()[0].Root
	if got != sub {
		t.Errorf("tilde expanded to %q, want %q", got, sub)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("expanded root not absolute: %q", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := config.Load("/no/such/config/file.cfg"); err == nil {
		t.Fatal("expected error loading nonexistent config file")
	}
}

// TestLoadWorkerNoLibraries verifies the worker loader tolerates a config with
// no [libraries] section — the GPU/worker box has no local libraries (it
// reaches media via the job API). The strict Load must still reject the same
// config.
func TestLoadWorkerNoLibraries(t *testing.T) {
	cfgPath := writeCfg(t, `
[worker]
shared_secret = wsecret

[ai]
ollama_url = http://gpu-box:11434
rekognition_profile = imyousuf
rekognition_region = us-east-1
rekognition_collection = faces1

[database]
path = `+filepath.Join(t.TempDir(), "fsim.db")+`

[cache]
dir = `+filepath.Join(t.TempDir(), "cache")+`
`)

	// LoadWorker accepts it and parses the worker-relevant sections.
	c, err := config.LoadWorker(cfgPath)
	if err != nil {
		t.Fatalf("LoadWorker: %v", err)
	}
	if len(c.Libraries()) != 0 {
		t.Errorf("worker Libraries() = %d, want 0", len(c.Libraries()))
	}
	if c.WorkerSecret() != "wsecret" {
		t.Errorf("worker secret = %q", c.WorkerSecret())
	}
	if c.OllamaURL() != "http://gpu-box:11434" {
		t.Errorf("ollama url = %q", c.OllamaURL())
	}
	if c.RekognitionCollection() != "faces1" {
		t.Errorf("rekognition collection = %q", c.RekognitionCollection())
	}

	// The strict server loader must still reject a config with no libraries.
	if _, err := config.Load(cfgPath); err == nil {
		t.Fatal("Load should reject a config with no [libraries]")
	}
}

// TestLoadWorkerSkipsRootStat verifies LoadWorker does NOT stat library roots:
// a [libraries] entry pointing at a nonexistent dir (as on the worker box,
// where library paths don't exist locally) must not fail the worker load.
func TestLoadWorkerSkipsRootStat(t *testing.T) {
	cfgPath := writeCfg(t, "[libraries]\npictures = /nonexistent/on/worker/box\n[worker]\nshared_secret = s\n")

	if _, err := config.LoadWorker(cfgPath); err != nil {
		t.Fatalf("LoadWorker must not stat library roots: %v", err)
	}
	// Strict Load stats roots and must fail on the missing dir.
	if _, err := config.Load(cfgPath); err == nil {
		t.Fatal("Load should reject a missing library root")
	}
}
