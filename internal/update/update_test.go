package update

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckFindsMatchingAssetsAndUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Fatal("missing user agent")
		}
		fmt.Fprint(w, `{
			"tag_name": "v0.2.0",
			"html_url": "https://example.test/releases/v0.2.0",
			"assets": [
				{"name":"tickle-linux-amd64","browser_download_url":"https://example.test/tickle-linux-amd64"},
				{"name":"tickle-skill-linux-amd64.zip","browser_download_url":"https://example.test/tickle-skill-linux-amd64.zip"},
				{"name":"tickle-darwin-arm64","browser_download_url":"https://example.test/tickle-darwin-arm64"}
			]
		}`)
	}))
	defer server.Close()

	result, err := Check(context.Background(), Options{
		CurrentVersion: "0.1.0",
		GOOS:           "linux",
		GOARCH:         "amd64",
		LatestURL:      server.URL,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.UpdateAvailable || !result.CurrentComparable {
		t.Fatalf("expected comparable update, got %#v", result)
	}
	if result.Platform != "linux-amd64" {
		t.Fatalf("platform = %q", result.Platform)
	}
	if result.BinaryAsset == nil || result.BinaryAsset.Name != "tickle-linux-amd64" {
		t.Fatalf("binary asset = %#v", result.BinaryAsset)
	}
	if result.SkillAsset == nil || result.SkillAsset.Name != "tickle-skill-linux-amd64.zip" {
		t.Fatalf("skill asset = %#v", result.SkillAsset)
	}
}

func TestCheckEqualVersionIsUpToDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v0.1.0","html_url":"https://example.test/releases/v0.1.0","assets":[]}`)
	}))
	defer server.Close()

	result, err := Check(context.Background(), Options{
		CurrentVersion: "v0.1.0",
		GOOS:           "linux",
		GOARCH:         "amd64",
		LatestURL:      server.URL,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.UpdateAvailable {
		t.Fatalf("expected no update, got %#v", result)
	}
	if !result.CurrentComparable {
		t.Fatalf("expected comparable versions, got %#v", result)
	}
}

func TestCheckUnknownCurrentVersionIsNotComparable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v0.1.0","html_url":"https://example.test/releases/v0.1.0","assets":[]}`)
	}))
	defer server.Close()

	result, err := Check(context.Background(), Options{
		CurrentVersion: "dev",
		GOOS:           "linux",
		GOARCH:         "amd64",
		LatestURL:      server.URL,
		HTTPClient:     server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.CurrentComparable || result.UpdateAvailable {
		t.Fatalf("expected non-comparable current version, got %#v", result)
	}
}
