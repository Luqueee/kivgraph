package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const defaultNoticeCacheTTL = 24 * time.Hour
const defaultNoticeTimeout = 800 * time.Millisecond

// NoticeOptions controls the non-mutating release lookup used by the bare CLI
// invocation. It deliberately has a shorter timeout and a cache unlike Run,
// because checking for an update must never make a local command feel remote.
type NoticeOptions struct {
	Client         *http.Client
	APIBaseURL     string
	Token          string
	CurrentVersion string
	CachePath      string
	CacheTTL       time.Duration
	Timeout        time.Duration
	Now            func() time.Time
	Channel        string
}

// NoticeResult is the part of Result that the CLI needs to decide whether to
// print an update hint. FromCache makes the source observable to callers and
// tests without exposing the cache representation.
type NoticeResult struct {
	CurrentVersion  string
	LatestVersion   string
	UpdateAvailable bool
	FromCache       bool
	Channel         string
}

type noticeCache struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version"`
	Channel       string    `json:"channel,omitempty"`
}

// CheckNotice checks the latest published release at most once per cache TTL.
// It never installs a release. A caller may treat a returned error as
// non-fatal when the notice is merely an optional terminal convenience.
func CheckNotice(ctx context.Context, options NoticeOptions) (NoticeResult, error) {
	if ctx == nil {
		return NoticeResult{}, errors.New("update notice context must not be nil")
	}
	current := strings.TrimSpace(options.CurrentVersion)
	if current == "" {
		return NoticeResult{}, errors.New("current Kivgraph version must not be empty")
	}
	currentSemver := semanticVersion(current)
	if !semver.IsValid(currentSemver) {
		return NoticeResult{}, fmt.Errorf("current Kivgraph version %q is not valid semver", current)
	}
	channel, err := resolveChannel(current, options.Channel)
	if err != nil {
		return NoticeResult{}, err
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.CacheTTL <= 0 {
		options.CacheTTL = defaultNoticeCacheTTL
	}
	cachePath := options.CachePath
	if cachePath == "" {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			return NoticeResult{}, fmt.Errorf("resolve update cache directory: %w", err)
		}
		cachePath = filepath.Join(cacheDir, "kivgraph", "update-check.json")
	}
	now := options.Now()
	if cached, ok := readNoticeCache(cachePath, now, options.CacheTTL); ok {
		latestSemver := semanticVersion(cached.LatestVersion)
		cachedChannel := cached.Channel
		if cachedChannel == "" {
			// Cache files written before channels existed describe the stable
			// endpoint and remain valid for the default stable stream.
			cachedChannel = ChannelStable
		}
		if cachedChannel == channel && semver.IsValid(latestSemver) {
			return NoticeResult{
				CurrentVersion:  current,
				LatestVersion:   cached.LatestVersion,
				UpdateAvailable: semver.Compare(currentSemver, latestSemver) < 0,
				FromCache:       true,
				Channel:         channel,
			}, nil
		}
	}

	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultNoticeTimeout
	}
	checkContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	result, err := Run(checkContext, Options{
		Client:         client,
		APIBaseURL:     options.APIBaseURL,
		Token:          options.Token,
		CurrentVersion: current,
		CheckOnly:      true,
		Channel:        channel,
	})
	if err != nil {
		return NoticeResult{}, err
	}
	cached := noticeCache{CheckedAt: now, LatestVersion: result.LatestVersion, Channel: channel}
	if err := writeNoticeCache(cachePath, cached); err != nil {
		return NoticeResult{}, fmt.Errorf("write update notice cache: %w", err)
	}
	return NoticeResult{
		CurrentVersion:  result.CurrentVersion,
		LatestVersion:   result.LatestVersion,
		UpdateAvailable: result.UpdateAvailable,
		Channel:         channel,
	}, nil
}

func readNoticeCache(path string, now time.Time, ttl time.Duration) (noticeCache, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return noticeCache{}, false
	}
	var cache noticeCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return noticeCache{}, false
	}
	if cache.CheckedAt.IsZero() || cache.LatestVersion == "" || now.Before(cache.CheckedAt) || now.Sub(cache.CheckedAt) >= ttl {
		return noticeCache{}, false
	}
	return cache, true
}

func writeNoticeCache(path string, cache noticeCache) error {
	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("encode cache: %w", err)
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	temporary, err := os.CreateTemp(parent, ".update-check-*")
	if err != nil {
		return fmt.Errorf("create cache temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set cache permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write cache: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync cache: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close cache: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace cache: %w", err)
	}
	return nil
}
