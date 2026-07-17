package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	errInvalidStreamToken = errors.New("invalid stream token")
	errExpiredStreamToken = errors.New("expired stream token")
)

type streamToken struct {
	UserID     int64
	StreamPath string
	ExpiresAt  time.Time
}

var streamTokens sync.Map

var streamTokenCleanupOnce sync.Once

func startStreamTokenCleanup() {
	streamTokenCleanupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				now := time.Now().UTC()
				streamTokens.Range(func(key, value any) bool {
					rec, ok := value.(*streamToken)
					if !ok || rec == nil || now.After(rec.ExpiresAt) {
						streamTokens.Delete(key)
					}
					return true
				})
			}
		}()
	})
}

func IssueStreamToken(userID int64, streamPath string, ttl time.Duration) (string, error) {
	streamPath = normalizeStreamPath(streamPath)
	if userID <= 0 || streamPath == "" || ttl <= 0 {
		return "", errInvalidStreamToken
	}

	startStreamTokenCleanup()

	token, err := newStreamToken()
	if err != nil {
		return "", err
	}

	streamTokens.Store(token, &streamToken{
		UserID:     userID,
		StreamPath: streamPath,
		ExpiresAt:  time.Now().UTC().Add(ttl),
	})

	return token, nil
}

func ConsumeStreamToken(token string, streamPath string, now time.Time) (int64, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, errInvalidStreamToken
	}

	streamPath = normalizeStreamPath(streamPath)
	if streamPath == "" {
		return 0, errInvalidStreamToken
	}

	value, ok := streamTokens.LoadAndDelete(token)
	if !ok {
		return 0, errInvalidStreamToken
	}

	rec, ok := value.(*streamToken)
	if !ok || rec == nil {
		return 0, errInvalidStreamToken
	}
	if now.UTC().After(rec.ExpiresAt) {
		return 0, errExpiredStreamToken
	}
	if rec.StreamPath != streamPath {
		return 0, errInvalidStreamToken
	}
	if rec.UserID <= 0 {
		return 0, errInvalidStreamToken
	}

	return rec.UserID, nil
}

func normalizeStreamPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if idx := strings.IndexAny(path, "?#"); idx >= 0 {
		path = path[:idx]
	}
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return "/" + strings.TrimLeft(path, "/")
}

func newStreamToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
