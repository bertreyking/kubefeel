package integration

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type EndpointConfig struct {
	Endpoint      string
	Username      string
	Secret        string
	SkipTLSVerify bool
}

type endpointClient struct {
	baseURL     *url.URL
	httpClient  *http.Client
	username    string
	secret      string
	staticToken bool
	tokenCache  map[string]string
	mu          sync.Mutex
}

func newEndpointClient(cfg EndpointConfig) (*endpointClient, error) {
	baseURL, err := normalizeEndpointURL(cfg.Endpoint)
	if err != nil {
		return nil, err
	}

	return &endpointClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
			Transport: &http.Transport{
				Proxy:           http.ProxyFromEnvironment,
				TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.SkipTLSVerify}, //nolint:gosec
			},
		},
		username:    strings.TrimSpace(cfg.Username),
		secret:      strings.TrimSpace(cfg.Secret),
		staticToken: strings.TrimSpace(cfg.Username) == "" && strings.TrimSpace(cfg.Secret) != "",
		tokenCache:  map[string]string{},
	}, nil
}

func normalizeEndpointURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("endpoint is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("endpoint must include scheme and host")
	}

	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed, nil
}

func (c *endpointClient) BaseURL() string {
	return c.baseURL.String()
}

func (c *endpointClient) ResolvePath(rawPath string) string {
	return c.resolveURL(rawPath, nil).String()
}

func (c *endpointClient) DoJSON(
	ctx context.Context,
	method string,
	rawPath string,
	query url.Values,
	headers map[string]string,
	scope string,
	target any,
) error {
	resp, err := c.do(ctx, method, rawPath, query, headers, nil, scope)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
		return fmt.Errorf("request failed: %s", strings.TrimSpace(string(body)))
	}

	if target == nil {
		return nil
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

func (c *endpointClient) DoBytes(
	ctx context.Context,
	method string,
	rawPath string,
	query url.Values,
	headers map[string]string,
	scope string,
) ([]byte, http.Header, error) {
	resp, err := c.do(ctx, method, rawPath, query, headers, nil, scope)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
		return nil, nil, fmt.Errorf("request failed: %s", strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	return body, resp.Header.Clone(), nil
}

func (c *endpointClient) do(
	ctx context.Context,
	method string,
	rawPath string,
	query url.Values,
	headers map[string]string,
	body io.Reader,
	scope string,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.resolveURL(rawPath, query).String(), body)
	if err != nil {
		return nil, err
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	if c.staticToken {
		req.Header.Set("Authorization", "Bearer "+c.secret)
	} else if c.username != "" || c.secret != "" {
		req.SetBasicAuth(c.username, c.secret)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	challenge := resp.Header.Get("Www-Authenticate")
	_ = resp.Body.Close()
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return resp, nil
	}

	token, err := c.issueBearerToken(ctx, challenge, scope)
	if err != nil {
		return nil, err
	}

	retryReq, err := http.NewRequestWithContext(ctx, method, c.resolveURL(rawPath, query).String(), body)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		retryReq.Header.Set(key, value)
	}
	retryReq.Header.Set("Authorization", "Bearer "+token)

	return c.httpClient.Do(retryReq)
}

func (c *endpointClient) issueBearerToken(ctx context.Context, challenge string, scope string) (string, error) {
	params := parseAuthHeaderParams(strings.TrimSpace(challenge[len("Bearer "):]))
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("registry auth challenge missing realm")
	}

	effectiveScope := strings.TrimSpace(scope)
	if effectiveScope == "" {
		effectiveScope = strings.TrimSpace(params["scope"])
	}

	cacheKey := realm + "|" + effectiveScope
	c.mu.Lock()
	if token, ok := c.tokenCache[cacheKey]; ok && token != "" {
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()

	tokenURL, err := url.Parse(realm)
	if err != nil {
		return "", fmt.Errorf("invalid token realm: %w", err)
	}

	query := tokenURL.Query()
	if service := strings.TrimSpace(params["service"]); service != "" {
		query.Set("service", service)
	}
	if effectiveScope != "" {
		query.Set("scope", effectiveScope)
	}
	tokenURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL.String(), nil)
	if err != nil {
		return "", err
	}
	if c.username != "" || (!c.staticToken && c.secret != "") {
		req.SetBasicAuth(c.username, c.secret)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
		return "", fmt.Errorf("registry token request failed: %s", strings.TrimSpace(string(body)))
	}

	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	token := strings.TrimSpace(payload.Token)
	if token == "" {
		token = strings.TrimSpace(payload.AccessToken)
	}
	if token == "" {
		return "", fmt.Errorf("registry token response missing token")
	}

	c.mu.Lock()
	c.tokenCache[cacheKey] = token
	c.mu.Unlock()
	return token, nil
}

func (c *endpointClient) resolveURL(rawPath string, query url.Values) *url.URL {
	base := *c.baseURL
	path := strings.TrimSpace(rawPath)
	switch {
	case path == "":
		base.Path = strings.TrimRight(base.Path, "/")
	case strings.HasPrefix(path, "/"):
		base.Path = strings.TrimRight(base.Path, "/") + path
	default:
		base.Path = strings.TrimRight(base.Path, "/") + "/" + path
	}
	base.RawQuery = ""
	if query != nil {
		base.RawQuery = query.Encode()
	}
	return &base
}

func parseAuthHeaderParams(value string) map[string]string {
	result := map[string]string{}
	for _, segment := range splitIgnoringQuotes(value, ',') {
		parts := strings.SplitN(strings.TrimSpace(segment), "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		raw := strings.TrimSpace(parts[1])
		result[strings.ToLower(key)] = strings.Trim(raw, `"`)
	}

	return result
}

func splitIgnoringQuotes(value string, separator rune) []string {
	segments := []string{}
	start := 0
	inQuotes := false
	for index, char := range value {
		switch char {
		case '"':
			inQuotes = !inQuotes
		case separator:
			if inQuotes {
				continue
			}
			segments = append(segments, value[start:index])
			start = index + 1
		}
	}

	segments = append(segments, value[start:])
	return segments
}
