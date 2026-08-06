package main

import (
	"regexp"
	"strings"
	"testing"
)

// Every asset the routes hand out has to be in the embedded FS. A missing one
// is invisible in development -- the page still renders, the link preview just
// silently loses its image -- so pin the names here.
func TestStaticAssetsEmbedded(t *testing.T) {
	for _, name := range []string{
		"favicon.ico",
		"og-image.png",
		"robots.txt",
		"security.txt",
		"manifest.webmanifest",
		"icon.svg",
		"apple-touch-icon.png",
	} {
		data, err := staticAssets.ReadFile("static/" + name)
		if err != nil {
			t.Errorf("static/%s not embedded: %v", name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("static/%s is empty", name)
		}
	}
}

// security.txt is only valid with these two fields (RFC 9116), and an expired
// file is treated as no file at all by the tools that read it.
func TestSecurityTxtRequiredFields(t *testing.T) {
	data, err := staticAssets.ReadFile("static/security.txt")
	if err != nil {
		t.Fatalf("read security.txt: %v", err)
	}
	body := string(data)
	for _, field := range []string{"Contact:", "Expires:"} {
		if !strings.Contains(body, field) {
			t.Errorf("security.txt missing required field %q", field)
		}
	}
}

// The link-preview tags carry absolute URLs because scrapers do not resolve
// relative paths, and og:image must point at a file we actually serve. Both
// are easy to break by editing web/index.html without rebuilding.
func TestOpenGraphTagsResolve(t *testing.T) {
	data, err := staticAssets.ReadFile("static/app/index.html")
	if err != nil {
		t.Fatalf("read built index.html (run npm --prefix web run build): %v", err)
	}
	html := string(data)

	ogImage := regexp.MustCompile(`property="og:image" content="([^"]+)"`).FindStringSubmatch(html)
	if ogImage == nil {
		t.Fatal("no og:image tag in the built index.html")
	}
	const prefix = "https://"
	if !strings.HasPrefix(ogImage[1], prefix) {
		t.Errorf("og:image = %q, want an absolute https URL", ogImage[1])
	}

	// Map the URL back to the asset name the mux serves it under.
	name := ogImage[1][strings.LastIndex(ogImage[1], "/")+1:]
	if _, err := staticAssets.ReadFile("static/" + name); err != nil {
		t.Errorf("og:image points at %q, which is not an embedded asset: %v", name, err)
	}

	for _, tag := range []string{
		`property="og:title"`,
		`property="og:description"`,
		`property="og:url"`,
		`name="twitter:card" content="summary_large_image"`,
		`name="description"`,
	} {
		if !strings.Contains(html, tag) {
			t.Errorf("built index.html missing %s", tag)
		}
	}
}
