package ktda

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// IndexURL is the GKV-Datenaustausch landing page that lists current KE0
// files for the SoLE (Sonstige Leistungserbringer) Kostenträgerdatei.
//
// We scrape it because the filenames carry the quarter and year and change
// every three months — hardcoding them rots fast.
const IndexURL = "https://www.gkv-datenaustausch.de/leistungserbringer/sonstige_leistungserbringer/kostentraegerdateien_sle/kostentraegerdateien.jsp"

const baseHost = "https://www.gkv-datenaustausch.de"

// kassenartRe matches the .ke0 filenames we want. The first 2 letters identify
// the Kassenart (AO/EK/BK/IK/BN/LK), then verfahren (05 for SoLE), then
// quarter (Q1..Q4), then 2-digit year.
var kassenartRe = regexp.MustCompile(`(?i)(AO|EK|BK|IK|BN|LK)\d{2}Q\d\d{2}\.ke0`)

// linkRe pulls href targets out of HTML. We only want the ones that look like
// /media/... .ke0 paths.
var linkRe = regexp.MustCompile(`href="([^"]+\.ke0)"`)

// DiscoverFiles fetches the GKV-Datenaustausch index page and returns absolute
// download URLs for every current SoLE KE0 file (one per Kassenart).
func DiscoverFiles() ([]string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(IndexURL)
	if err != nil {
		return nil, fmt.Errorf("fetch index: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("index returned %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	matches := linkRe.FindAllStringSubmatch(string(body), -1)
	seen := make(map[string]bool)
	var urls []string
	for _, m := range matches {
		path := m[1]
		base := filepath.Base(path)
		if !kassenartRe.MatchString(base) {
			continue
		}
		var u string
		if strings.HasPrefix(path, "http") {
			u = path
		} else {
			u = baseHost + path
		}
		if !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("no KE0 download links found at %s", IndexURL)
	}
	return urls, nil
}

// Download fetches each URL into dir, returning the local file paths.
func Download(urls []string, dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	var paths []string
	for _, u := range urls {
		name := filepath.Base(u)
		dst := filepath.Join(dir, name)
		if err := downloadOne(client, u, dst); err != nil {
			return paths, fmt.Errorf("%s: %w", name, err)
		}
		paths = append(paths, dst)
	}
	return paths, nil
}

func downloadOne(client *http.Client, url, dst string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s", resp.Status)
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	return os.Rename(tmp, dst)
}

// KassenartFromFilename returns the 2-letter Kassenart prefix from a .ke0
// filename like "EK05Q226.ke0".
func KassenartFromFilename(path string) string {
	base := strings.ToUpper(filepath.Base(path))
	if len(base) < 2 {
		return ""
	}
	return base[:2]
}
