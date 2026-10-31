// credit: github.com/Nef10/RogersBankDownloader

package rogers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

// The Rogers Bank web app talks to a cookie-session JSON API.
// This client mirrors the requests made by the browser.

const defaultBaseURL = "https://rbaccess.rogersbank.com"

// deviceInfo is the browser fingerprint the web app sends.
// Rogers only checks that it is present
const deviceInfo = `[{"key":"language","value":"en-US"},{"key":"color_depth","value":30},{"key":"device_memory","value":8},{"key":"hardware_concurrency","value":10},{"key":"resolution","value":[1728,1117]},{"key":"available_resolution","value":[1728,1005]},{"key":"timezone_offset","value":420},{"key":"session_storage","value":1},{"key":"local_storage","value":1},{"key":"indexed_db","value":1},{"key":"cpu_class","value":"unknown"},{"key":"navigator_platform","value":"MacIntel"},{"key":"regular_plugins","value":["PDF Viewer::Portable Document Format::application/pdf~pdf,text/pdf~pdf","Chrome PDF Viewer::Portable Document Format::application/pdf~pdf,text/pdf~pdf","Chromium PDF Viewer::Portable Document Format::application/pdf~pdf,text/pdf~pdf","WebKit built-in PDF::Portable Document Format::application/pdf~pdf,text/pdf~pdf"]},{"key":"webgl_vendor","value":"Google Inc. (Apple)~ANGLE (Apple, ANGLE Metal Renderer: Apple M1 Pro, Unspecified Version)"},{"key":"adblock","value":false},{"key":"has_lied_languages","value":false},{"key":"has_lied_resolution","value":false},{"key":"has_lied_os","value":false},{"key":"has_lied_browser","value":false},{"key":"touch_support","value":[0,false,false]},{"key":"js_fonts","value":["Arial","Arial Black","Arial Hebrew","Arial Narrow","Arial Rounded MT Bold","Arial Unicode MS","Comic Sans MS","Courier","Courier New","Geneva","Georgia","Helvetica","Helvetica Neue","Impact","Microsoft Sans Serif","Monaco","Palatino","Tahoma","Times","Times New Roman","Trebuchet MS","Verdana","Wingdings","Wingdings 2","Wingdings 3"]}]`

var ErrWrongVerificationCode = errors.New("rogers: one-time verification code rejected")

type client struct {
	http *http.Client
	base string
}

func newClient(base string) *client {
	jar, _ := cookiejar.New(nil)
	if base == "" {
		base = defaultBaseURL
	}
	return &client{http: &http.Client{Jar: jar, Timeout: 60 * time.Second}, base: base}
}

// newDeviceID is basically two random hex strings joined by an already-encoded "|"
func newDeviceID() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b[:16]) + "%7C" + hex.EncodeToString(b[16:])
}

// start seeds the session cookies and must run before authenticate.
func (c *client) start(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/?product=ROGERSBRAND", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("rogers start: %w", err)
	}
	readBody(resp)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("rogers start: status %d", resp.StatusCode)
	}
	return nil
}

func (c *client) authenticate(ctx context.Context, username, password, deviceID string) (u *user, challenge bool, err error) {
	data, status, err := c.post(ctx, "/issuing/digital/authenticate/user", true, map[string]string{
		"username":   username,
		"password":   password,
		"deviceId":   deviceID,
		"deviceInfo": deviceInfo,
	})
	if err != nil {
		return nil, false, err
	}
	if status == http.StatusUnauthorized {
		var e struct {
			Title  string `json:"title"`
			Status int    `json:"status"`
		}
		if json.Unmarshal(data, &e) == nil && e.Title == "Device Not Found" && e.Status == 412 {
			return nil, true, nil
		}
	}
	if status != http.StatusOK {
		return nil, false, fmt.Errorf("rogers authenticate: status %d", status)
	}
	u, err = decodeUser(data)
	return u, false, err
}

func (c *client) twoFactorPreferences(ctx context.Context, username string) (*twoFactorPreferences, error) {
	data, status, err := c.post(ctx, "/issuing/digital/twofactorpasscode/preferences/user", false, map[string]string{"username": username})
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("rogers 2fa preferences: status %d", status)
	}
	var p twoFactorPreferences
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("rogers 2fa preferences: %w", err)
	}
	return &p, nil
}

func (c *client) sendCode(ctx context.Context, p *twoFactorPreferences, username, deviceID, method string) error {
	path := fmt.Sprintf("/issuing/digital/twofactorpasscode/%s/customer/%s/generatecodefordevice",
		url.PathEscape(p.AccountID), url.PathEscape(p.CustomerID))
	data, status, err := c.post(ctx, path, false, map[string]string{
		"username":       username,
		"deviceId":       deviceID,
		"deviceInfo":     deviceInfo,
		"preferenceType": method,
	})
	if err != nil {
		return err
	}
	var res struct {
		Success bool `json:"success"`
	}
	if status != http.StatusOK || json.Unmarshal(data, &res) != nil || !res.Success {
		return fmt.Errorf("rogers: could not send passcode (status %d)", status)
	}
	return nil
}

// validatePasscode confirms the one-time passcode. Rogers answers a wrong code with 404.
func (c *client) validatePasscode(ctx context.Context, username, deviceID, otp string) (*user, error) {
	data, status, err := c.post(ctx, "/issuing/digital/authenticate/validatepasscode", true, map[string]string{
		"username":        username,
		"deviceId":        deviceID,
		"deviceInfo":      deviceInfo,
		"oneTimePassCode": otp,
	})
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, ErrWrongVerificationCode
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("rogers validate verification code: status %d", status)
	}
	return decodeUser(data)
}

// activities returns one statement period. An empty cycleStart means the current, unbilled period.
func (c *client) activities(ctx context.Context, accountID, customerID, cycleStart string) ([]activity, error) {
	u := fmt.Sprintf("%s/issuing/digital/account/%s/customer/%s/activity",
		c.base, url.PathEscape(accountID), url.PathEscape(customerID))
	if cycleStart != "" {
		u += "?cycleStartDate=" + url.QueryEscape(cycleStart)
	}
	var lastErr error
	// The endpoint is flaky, so it is tried 3 times.
	for range 3 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			continue
		}
		data, err := readBody(resp)
		if err != nil || resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			continue
		}
		var wrap struct {
			Activities []activity `json:"activities"`
		}
		if err := json.Unmarshal(data, &wrap); err != nil {
			return nil, fmt.Errorf("rogers activities: %w", err)
		}
		return wrap.Activities, nil
	}
	return nil, fmt.Errorf("rogers activities: %w", lastErr)
}

// The main bread and butter.
func (c *client) post(ctx context.Context, path string, auth bool, body map[string]string) ([]byte, int, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, &buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Datatype", "json")
	if auth {
		req.Header.Set("Brand_id", "ROGERSBRAND")
		req.Header.Set("Sourcetype", "web")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("rogers request %s: %w", path, err)
	}
	data, err := readBody(resp)
	return data, resp.StatusCode, err
}

func decodeUser(data []byte) (*user, error) {
	var u user
	if err := json.Unmarshal(data, &u); err != nil {
		return nil, fmt.Errorf("rogers user: %w", err)
	}
	return &u, nil
}

func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}
