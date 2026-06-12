package hurricane

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const defaultBaseURL = "https://dyn.dns.he.net/nic/update"

const (
	codeGood     = "good"
	codeNoChg    = "nochg"
	codeAbuse    = "abuse"
	codeBadAgent = "badagent"
	codeBadAuth  = "badauth"
	codeInterval = "interval"
	codeNoHost   = "nohost"
	codeNotFqdn  = "notfqdn"
)

const defaultBurst = 5

// Client the Hurricane Electric client.
type Client struct {
	HTTPClient   *http.Client
	rateLimiters sync.Map

	baseURL string

	credentials map[string]string
	credMu      sync.Mutex
}

type DiagnosticCode string

const (
	DiagnosticUnknown  DiagnosticCode = "unknown"
	DiagnosticInterval DiagnosticCode = codeInterval
	DiagnosticBadAuth  DiagnosticCode = codeBadAuth
	DiagnosticNoHost   DiagnosticCode = codeNoHost
)

type DiagnosticError struct {
	Code     DiagnosticCode
	Host     string
	Response string
	Message  string
}

func (e *DiagnosticError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// NewClient Creates a new Client.
func NewClient(credentials map[string]string) *Client {
	return &Client{
		HTTPClient:  &http.Client{Timeout: 5 * time.Second},
		baseURL:     defaultBaseURL,
		credentials: credentials,
	}
}

// UpdateTxtRecord updates a TXT record.
func (c *Client) UpdateTxtRecord(ctx context.Context, hostname, txt string) error {
	domain := strings.TrimPrefix(hostname, "_acme-challenge.")

	c.credMu.Lock()
	token, ok := c.credentials[domain]
	if !ok {
		// Try with the full hostname if the domain key is not found
		token, ok = c.credentials[hostname]
	}
	c.credMu.Unlock()

	if !ok {
		return fmt.Errorf("domain %s (or %s) not found in credentials, check your credentials map", domain, hostname)
	}

	data := url.Values{}
	data.Set("password", token)
	data.Set("hostname", hostname)
	data.Set("txt", txt)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("unable to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rl, _ := c.rateLimiters.LoadOrStore(hostname, rate.NewLimiter(limit(defaultBurst), defaultBurst))

	err = rl.(*rate.Limiter).Wait(ctx)
	if err != nil {
		return err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	return evaluateBody(string(bytes.TrimSpace(raw)), hostname)
}

func evaluateBody(body, hostname string) error {
	code, _, _ := strings.Cut(body, " ")

	switch code {
	case codeGood:
		return nil
	case codeNoChg:
		log.Printf("%s: unchanged content written to TXT record %s", body, hostname)
		return nil
	case codeAbuse:
		return fmt.Errorf("%s: blocked hostname for abuse: %s", body, hostname)
	case codeBadAgent:
		return fmt.Errorf("%s: user agent not sent or HTTP method not recognized; open an issue on go-acme/lego on GitHub", body)
	case codeBadAuth:
		return diagnosticError(DiagnosticBadAuth, hostname, body, "%s: wrong authentication token provided for TXT record %s", body, hostname)
	case codeInterval:
		return diagnosticError(DiagnosticInterval, hostname, body, "%s: TXT records update exceeded API rate limit", body)
	case codeNoHost:
		return diagnosticError(DiagnosticNoHost, hostname, body, "%s: the record provided does not exist in this account: %s", body, hostname)
	case codeNotFqdn:
		return fmt.Errorf("%s: the record provided isn't an FQDN: %s", body, hostname)
	default:
		// This is basically only server errors.
		return diagnosticError(DiagnosticUnknown, hostname, body, "attempt to change TXT record %s returned %s", hostname, body)
	}
}

func diagnosticError(code DiagnosticCode, hostname, response, format string, args ...any) error {
	return &DiagnosticError{
		Code:     code,
		Host:     hostname,
		Response: response,
		Message:  fmt.Sprintf(format, args...),
	}
}

// limit computes the rate based on burst.
// The API rate limit per-record is 10 reqs / 2 minutes.
//
//	10 reqs / 2 minutes = freq 1/12 (burst = 1)
//	6 reqs / 2 minutes = freq 1/20 (burst = 5)
//
// https://github.com/go-acme/lego/issues/1415
func limit(burst int) rate.Limit {
	return 1 / rate.Limit(120/(10-burst+1))
}
