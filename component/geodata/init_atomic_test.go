package geodata

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadToPathIsAtomicAndCleansTemp(t *testing.T) {
	const body = "complete-database"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dst := filepath.Join(dir, "geoip.metadb")
	if err := downloadToPath(srv.URL, dst); err != nil {
		t.Fatalf("downloadToPath: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(got) != body {
		t.Fatalf("destination=%q want %q", got, body)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".geoip.metadb.tmp-*"))
	if err != nil {
		t.Fatalf("glob temp files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left behind: %v", matches)
	}
}

func TestDownloadToPathKeepsExistingFileOnHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "broken", http.StatusBadGateway)
	}))
	defer srv.Close()

	dir := t.TempDir()
	dst := filepath.Join(dir, "geoip.metadb")
	if err := os.WriteFile(dst, []byte("old-good"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := downloadToPath(srv.URL, dst); err == nil {
		t.Fatal("expected HTTP failure")
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-good" {
		t.Fatalf("existing file changed after failed download: %q", got)
	}
}
