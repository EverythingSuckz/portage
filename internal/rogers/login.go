package rogers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.wrench.rocks/portage/internal/bank"
)

type session struct {
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	// this is what skips the 2fa
	DeviceID string `json:"device_id"`
}

func sessionDir(dataDir string) string { return filepath.Join(dataDir, "rogers") }

func sessionPath(dataDir, name string) string {
	return filepath.Join(sessionDir(dataDir), name+".json")
}

// Prompter asks the person at the terminal for two-factor details.
type Prompter interface {
	Choose(prefs []TwoFactorPreference) (TwoFactorPreference, error)
	VerificationCode() (string, error)
	Notify(msg string)
}

type LoginParams struct {
	DataDir       string
	Name          string
	Username      string
	Password      string
	StorePassword bool
	BaseURL       string
}

// Full login flow: load any saved session, start a new client, authenticate, verify device if needed, and save the session.
func Login(ctx context.Context, p LoginParams, pr Prompter) error {
	var old session
	_ = bank.LoadSecret(sessionPath(p.DataDir, p.Name), &old)
	deviceID := old.DeviceID
	if deviceID == "" {
		deviceID = newDeviceID()
	}

	c := newClient(p.BaseURL)
	if err := c.start(ctx); err != nil {
		return err
	}
	_, challenge, err := c.authenticate(ctx, p.Username, p.Password, deviceID)
	if err != nil {
		return err
	}
	if challenge {
		if err := verifyDevice(ctx, c, p.Username, deviceID, pr); err != nil {
			return err
		}
	}

	s := session{Username: p.Username, DeviceID: deviceID}
	if p.StorePassword {
		s.Password = p.Password
	}
	return bank.SaveSecret(sessionPath(p.DataDir, p.Name), s)
}

func verifyDevice(ctx context.Context, c *client, username, deviceID string, pr Prompter) error {
	prefs, err := c.twoFactorPreferences(ctx, username)
	if err != nil {
		return err
	}

	// doesn't happen but just in case
	if len(prefs.Preferences) == 0 {
		return errors.New("rogers offered no way to send a passcode")
	}
	pref, err := pr.Choose(prefs.Preferences)
	if err != nil {
		return err
	}
	if err := c.sendCode(ctx, prefs, username, deviceID, pref.Type); err != nil {
		return err
	}
	const attempts = 3 // rogers locks you out after x attempts, need to confirm x
	for i := 1; ; i++ {
		otp, err := pr.VerificationCode()
		if err != nil {
			return err
		}
		_, err = c.validatePasscode(ctx, username, deviceID, otp)
		if errors.Is(err, ErrWrongVerificationCode) && i < attempts {
			pr.Notify(fmt.Sprintf("That verification code was rejected. Try again (%d of %d).", i+1, attempts))
			continue
		}
		return err
	}
}

// LoadAll returns one source per saved Rogers login.
func LoadAll(dataDir string) ([]*Source, error) {
	files, err := filepath.Glob(filepath.Join(sessionDir(dataDir), "*.json"))
	if err != nil {
		return nil, err
	}
	var out []*Source
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		out = append(out, &Source{name: name, dataDir: dataDir})
	}
	return out, nil
}

func (s *Source) loadSession() (session, error) {
	var sess session
	err := bank.LoadSecret(sessionPath(s.dataDir, s.name), &sess)
	if errors.Is(err, os.ErrNotExist) {
		return sess, fmt.Errorf("no saved login; run `portage rogers login -name %s`", s.name)
	}
	if err != nil {
		return sess, err
	}
	if sess.Password == "" {
		sess.Password = os.Getenv("ROGERS_PASSWORD")
	}
	if sess.Username == "" || sess.Password == "" {
		return sess, errors.New("missing username or password; set ROGERS_PASSWORD or log in again")
	}
	return sess, nil
}
