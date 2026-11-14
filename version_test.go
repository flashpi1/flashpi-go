package flashpi

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

// The release PR fails here if release-please did not write the new version to version.go.
func TestVersionMatchesTheReleaseManifest(t *testing.T) {
	data, err := os.ReadFile(".release-please-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]string
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["."] != Version {
		t.Errorf("Version is %q, the manifest says %q", Version, manifest["."])
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`).MatchString(Version) || userAgent != "flashpi-go/"+Version {
		t.Errorf("Version %q, User-Agent %q", Version, userAgent)
	}
}
