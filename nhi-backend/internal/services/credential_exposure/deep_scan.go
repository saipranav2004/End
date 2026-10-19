package credential_exposure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	models "nhi-backend-api/internal/models/credential_exposure"
	repo "nhi-backend-api/internal/repository/credential_exposure"
	"nhi-backend-api/internal/scope"
)

var ErrScannerProxyUnavailable = errors.New("scanner proxy is not configured")

type DeepScanService struct {
	repo         *repo.DeepScanRepository
	resolver     *scope.Resolver
	baseURL, key string
	client       *http.Client
}

func NewDeepScanService(repo *repo.DeepScanRepository, resolver *scope.Resolver, baseURL, key string) *DeepScanService {
	return &DeepScanService{
		repo:     repo,
		resolver: resolver,
		baseURL:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		key:      strings.TrimSpace(key),
		client: &http.Client{
			Timeout: 25 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (s *DeepScanService) Status(ctx context.Context, userID, tenantID, dbName, clientID, repoKey, platform string) ([]models.DeepScanResult, error) {
	resolved, err := s.resolver.Resolve(ctx, userID, tenantID, dbName)
	if err != nil {
		return nil, err
	}
	return s.repo.Status(ctx, resolved, clientID, repoKey, platform)
}

func (s *DeepScanService) Trigger(ctx context.Context, userID, tenantID, dbName string, req models.DeepScanRequest) (int, []byte, error) {
	// Validate tenant scope even when proxy is unavailable — fail closed on scope, not on proxy config.
	if _, err := s.resolver.Resolve(ctx, userID, tenantID, dbName); err != nil {
		return 0, nil, err
	}
	if s.baseURL == "" || s.key == "" {
		return 0, nil, ErrScannerProxyUnavailable
	}
	u, err := url.Parse(s.baseURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return 0, nil, fmt.Errorf("scanner proxy URL must be an https URL")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return 0, nil, fmt.Errorf("encode deep scan request: %w", err)
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/deep-scan", bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("create scanner request: %w", err)
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "application/json")
	hreq.Header.Set("X-Dashboard-Key", s.key)
	resp, err := s.client.Do(hreq)
	if err != nil {
		return 0, nil, fmt.Errorf("scanner aggregator unavailable: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil {
		return 0, nil, fmt.Errorf("read scanner response: %w", err)
	}
	if len(b) > 1<<20 {
		return 0, nil, fmt.Errorf("scanner response exceeds limit")
	}
	return resp.StatusCode, b, nil
}
