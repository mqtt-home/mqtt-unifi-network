package unifi

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"github.com/philipparndt/go-logger"
)

// ErrForbidden is returned when UniFi OS accepts the login but the Network
// application refuses the request (HTTP 403). This is a permissions problem on
// the account, not a transient fault: it is worth distinguishing because
// retrying and restarting are both useless until a human changes the role.
var ErrForbidden = errors.New("the UniFi account has no access to the Network application")

// Client talks to the UniFi Network application through the UniFi OS proxy on
// the gateway. The authentication dance (CSRF token, TOKEN cookie, re-login on
// 401) is the same one the sibling unifi-access service uses against the same
// host — see that repo if this ever needs to change.
type Client struct {
	host       string
	username   string
	password   string
	site       string
	httpClient *http.Client

	mu        sync.RWMutex
	csrfToken string
}

func NewClient(host, username, password, site string, verifySSL bool) *Client {
	jar, _ := cookiejar.New(nil)

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			// The gateway serves a self-signed certificate.
			InsecureSkipVerify: !verifySSL,
		},
	}

	return &Client{
		host:     strings.TrimSuffix(host, "/"),
		username: username,
		password: password,
		site:     site,
		httpClient: &http.Client{
			Jar:       jar,
			Transport: transport,
			// The controller is on the LAN; 10s is generous. Keep this well
			// under the poll interval — Login does two sequential requests, and
			// a longer timeout means an unreachable gateway blocks the poll
			// loop past its own tick and delays the first error in the log.
			Timeout: 10 * time.Second,
		},
	}
}

// Login authenticates against UniFi OS and stores the session in the cookie jar.
func (c *Client) Login() error {
	if err := c.acquireCSRFToken(); err != nil {
		// Not every firmware hands out a token before login; the login response
		// carries one too, so this is not fatal.
		logger.Debug("Failed to acquire initial CSRF token", "error", err)
	}

	url := fmt.Sprintf("%s/api/auth/login", c.host)
	payload := map[string]any{
		"username":   c.username,
		"password":   c.password,
		"token":      "",
		"rememberMe": true,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal login payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	c.mu.RLock()
	token := c.csrfToken
	c.mu.RUnlock()
	if token != "" {
		req.Header.Set("X-Csrf-Token", token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login failed with status %d: %s", resp.StatusCode, truncate(string(bodyBytes), 200))
	}

	c.storeCSRF(resp)
	logger.Info("Logged in to UniFi controller", "host", c.host, "user", c.username)
	return nil
}

func (c *Client) acquireCSRFToken() error {
	req, err := http.NewRequest("GET", c.host, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	c.storeCSRF(resp)
	return nil
}

func (c *Client) storeCSRF(resp *http.Response) {
	token := resp.Header.Get("X-Updated-Csrf-Token")
	if token == "" {
		token = resp.Header.Get("X-Csrf-Token")
	}
	if token == "" {
		return
	}
	c.mu.Lock()
	c.csrfToken = token
	c.mu.Unlock()
}

// GetClients returns every client currently associated with the site.
func (c *Client) GetClients() ([]APIClient, error) {
	data, err := c.get(c.networkURL("/stat/sta"))
	if err != nil {
		return nil, fmt.Errorf("stat/sta request failed: %w", err)
	}

	var parsed clientsResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse client list: %w", err)
	}
	return parsed.Data, nil
}

// GetAccessPoints returns MAC -> name for every UniFi device on the site, so a
// client's `ap_mac` can be resolved to something a human configured a floor for.
func (c *Client) GetAccessPoints() (map[string]string, error) {
	data, err := c.get(c.networkURL("/stat/device"))
	if err != nil {
		return nil, fmt.Errorf("stat/device request failed: %w", err)
	}

	var parsed devicesResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse device list: %w", err)
	}

	aps := make(map[string]string, len(parsed.Data))
	for _, d := range parsed.Data {
		if d.MAC == "" {
			continue
		}
		aps[NormalizeMAC(d.MAC)] = d.Name
	}
	return aps, nil
}

// GetKnownClients returns MAC -> last-seen time for every client the controller
// remembers, including ones not currently associated. Used once at startup to
// seed presence, so a restart does not report sleeping phones as away.
func (c *Client) GetKnownClients() (map[string]time.Time, error) {
	data, err := c.get(c.networkURL("/rest/user"))
	if err != nil {
		return nil, fmt.Errorf("rest/user request failed: %w", err)
	}

	var parsed usersResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse known client list: %w", err)
	}

	seen := make(map[string]time.Time, len(parsed.Data))
	for _, u := range parsed.Data {
		if u.MAC == "" || u.LastSeen == 0 {
			continue
		}
		seen[NormalizeMAC(u.MAC)] = time.Unix(u.LastSeen, 0)
	}
	return seen, nil
}

func (c *Client) networkURL(path string) string {
	return fmt.Sprintf("%s/proxy/network/api/s/%s%s", c.host, c.site, path)
}

func (c *Client) get(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	return c.doRequest(req)
}

// doRequest sends a request and, on 401, re-authenticates once and retries. The
// UniFi session expires on its own schedule, and a bridge that only polls would
// otherwise fail forever after the first expiry.
func (c *Client) doRequest(req *http.Request) ([]byte, error) {
	retryReq := req.Clone(req.Context())

	body, status, err := c.sendRequest(req)
	if err != nil {
		return nil, err
	}

	if status == http.StatusUnauthorized {
		logger.Warn("Request unauthorized (401), re-authenticating", "path", req.URL.Path)
		if loginErr := c.Login(); loginErr != nil {
			return nil, fmt.Errorf("re-login after 401 failed: %w", loginErr)
		}
		body, status, err = c.sendRequest(retryReq)
		if err != nil {
			return nil, err
		}
	}

	if status == http.StatusForbidden {
		return nil, fmt.Errorf("%w (%s): %s", ErrForbidden, req.URL.Path, truncate(string(body), 200))
	}

	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("request failed with status %d: %s", status, truncate(string(body), 200))
	}
	return body, nil
}

func (c *Client) sendRequest(req *http.Request) ([]byte, int, error) {
	c.mu.RLock()
	csrfToken := c.csrfToken
	c.mu.RUnlock()

	if csrfToken != "" {
		req.Header.Set("X-Csrf-Token", csrfToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	c.storeCSRF(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
