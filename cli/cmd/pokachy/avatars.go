package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Avatars are downloaded here and shown by the Omarchy panel from local files.
// Loading them over HTTPS inside the shell made Qt initialise TLS in its image
// thread during startup, which crashed quickshell; the panel now never touches
// the network.

const (
	avatarMaxBytes     = 2 << 20
	avatarFetchBudget  = 5 * time.Second
	avatarFetchWorkers = 4
	// A concurrent sync may have just downloaded a file the other has not seen.
	avatarPruneAge = 10 * time.Minute
)

var (
	avatarFileName  = regexp.MustCompile(`^[0-9a-f]{64}\.(png|jpg|webp|avif)$`)
	avatarExtension = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp", "image/avif": "avif"}
	// githubAvatarOrigin is the only third-party origin avatars come from.
	githubAvatarOrigin = &url.URL{Scheme: "https", Host: "avatars.githubusercontent.com"}
)

// avatarRef points at one avatar URL in a State and the local file shown for it.
type avatarRef struct{ url, file *string }

func avatarDir(dir string) string { return filepath.Join(dir, "avatars") }

// avatarAllowed accepts only GitHub avatars and the configured server's own
// /avatars/ path, mirroring what the server itself returns.
func avatarAllowed(raw string, server *url.URL) bool {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	if sameOrigin(githubAvatarOrigin, u) {
		return strings.HasPrefix(u.Path, "/")
	}
	return sameOrigin(server, u) && strings.HasPrefix(u.Path, "/avatars/") && !strings.Contains(u.Path, "..")
}

func avatarKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func cachedAvatar(dir, raw string) string {
	key := avatarKey(raw)
	for _, ext := range avatarExtension {
		path := filepath.Join(dir, key+"."+ext)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

// fetchAvatar downloads one image without credentials or redirects and stores it
// only if it is a supported image no larger than the server's upload limit.
func fetchAvatar(ctx context.Context, client *http.Client, dir, raw string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "Pokachy/"+version)
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("avatar unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, avatarMaxBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > avatarMaxBytes {
		return "", errors.New("avatar size not allowed")
	}
	ext := avatarExtension[profileImageContentType(data[:min(len(data), 512)])]
	if ext == "" {
		return "", errors.New("avatar type not allowed")
	}
	file, err := os.CreateTemp(dir, ".avatar-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(data)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, avatarKey(raw)+"."+ext)
	return path, os.Rename(file.Name(), path)
}

func avatarClient() *http.Client {
	return &http.Client{Timeout: avatarFetchBudget, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// cacheAvatars fills ImageFile for every person and poke whose avatar is cached,
// downloading missing ones within a short budget. Failures only mean a missing
// picture; the next sync tries again.
func (c *Client) cacheAvatars(ctx context.Context, s *State) {
	server, err := serverURL(c.Config.Server)
	if err != nil {
		return
	}
	dir := avatarDir(c.Dir)
	images := []avatarRef{{&s.Me.Image, &s.Me.ImageFile}}
	for _, people := range [][]Person{s.Friends, s.Requests, s.Blocked} {
		for i := range people {
			images = append(images, avatarRef{&people[i].Image, &people[i].ImageFile})
		}
	}
	for _, pokes := range [][]Poke{s.Inbox, s.History} {
		for i := range pokes {
			images = append(images, avatarRef{&pokes[i].Image, &pokes[i].ImageFile})
		}
	}
	files := map[string]string{}
	var missing []string
	for _, image := range images {
		raw := *image.url
		if _, seen := files[raw]; seen || raw == "" || !avatarAllowed(raw, server) {
			continue
		}
		files[raw] = cachedAvatar(dir, raw)
		if files[raw] == "" {
			missing = append(missing, raw)
		}
	}
	if len(missing) > 0 && os.MkdirAll(dir, 0700) == nil && os.Chmod(dir, 0700) == nil {
		fetchCtx, cancel := context.WithTimeout(ctx, avatarFetchBudget)
		defer cancel()
		client := c.AvatarHTTP
		if client == nil {
			client = avatarClient()
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		queue := make(chan string)
		for range min(avatarFetchWorkers, len(missing)) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for raw := range queue {
					if path, err := fetchAvatar(fetchCtx, client, dir, raw); err == nil {
						mu.Lock()
						files[raw] = path
						mu.Unlock()
					}
				}
			}()
		}
		for _, raw := range missing {
			queue <- raw
		}
		close(queue)
		wg.Wait()
	}
	keep := map[string]bool{}
	for _, image := range images {
		*image.file = files[*image.url]
		if *image.file != "" {
			keep[filepath.Base(*image.file)] = true
		}
	}
	pruneAvatars(dir, keep, time.Now())
}

func pruneAvatars(dir string, keep map[string]bool, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || keep[entry.Name()] || !avatarFileName.MatchString(entry.Name()) || !info.Mode().IsRegular() || now.Sub(info.ModTime()) < avatarPruneAge {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

// avatarFiles lists cached avatars by their exact generated names, so cleanup
// never removes anything Pokachy did not write.
func avatarFiles(dir string) []string {
	entries, err := os.ReadDir(avatarDir(dir))
	if err != nil {
		return nil
	}
	var paths []string
	for _, entry := range entries {
		if avatarFileName.MatchString(entry.Name()) {
			paths = append(paths, filepath.Join(avatarDir(dir), entry.Name()))
		}
	}
	return paths
}
