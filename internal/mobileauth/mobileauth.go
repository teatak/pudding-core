package mobileauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

const (
	defaultPairingTTL = 5 * time.Minute
	deviceTokenBytes  = 32
	pairingCodeBytes  = 5
)

var ErrPairingInvalid = errors.New("pairing invalid")

type Device struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type storedDevice struct {
	Device
	TokenHash string `json:"tokenHash"`
}

type deviceFile struct {
	Devices []storedDevice `json:"devices"`
}

type DeviceStore struct {
	path    string
	mu      sync.Mutex
	devices map[string]storedDevice
}

func OpenDeviceStore(path string) (*DeviceStore, error) {
	s := &DeviceStore{path: path, devices: map[string]storedDevice{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f deviceFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("mobile devices: decode: %w", err)
	}
	for _, device := range f.Devices {
		if device.ID != "" && device.TokenHash != "" {
			s.devices[device.ID] = device
		}
	}
	return s, nil
}

func (s *DeviceStore) ValidToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	hash := hashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, device := range s.devices {
		if subtle.ConstantTimeCompare([]byte(hash), []byte(device.TokenHash)) == 1 {
			return true
		}
	}
	return false
}

func (s *DeviceStore) AddDevice(name string, now time.Time) (Device, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Mobile device"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	id, err := randomHex(12)
	if err != nil {
		return Device{}, "", err
	}
	token, err := randomHex(deviceTokenBytes)
	if err != nil {
		return Device{}, "", err
	}
	device := Device{ID: "dev_" + id, Name: name, CreatedAt: now.UTC()}
	stored := storedDevice{Device: device, TokenHash: hashToken(token)}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices[device.ID] = stored
	if err := s.saveLocked(); err != nil {
		delete(s.devices, device.ID)
		return Device{}, "", err
	}
	return device, token, nil
}

func (s *DeviceStore) saveLocked() error {
	f := deviceFile{Devices: make([]storedDevice, 0, len(s.devices))}
	for _, device := range s.devices {
		f.Devices = append(f.Devices, device)
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

type Pairing struct {
	Code      string    `json:"code"`
	URL       string    `json:"url"`
	URLs      []string  `json:"urls"`
	ExpiresAt time.Time `json:"expiresAt"`
	QRDataURL string    `json:"qrDataURL,omitempty"`
}

type ClaimResult struct {
	Token  string `json:"token"`
	Device Device `json:"device"`
}

type Manager struct {
	devices  *DeviceStore
	baseURLs []string
	ttl      time.Duration
	now      func() time.Time
	mu       sync.Mutex
	pending  map[string]time.Time
}

func NewManager(devices *DeviceStore, baseURLs []string) *Manager {
	return &Manager{
		devices:  devices,
		baseURLs: normalizeBaseURLs(baseURLs),
		ttl:      defaultPairingTTL,
		now:      time.Now,
		pending:  map[string]time.Time{},
	}
}

func (m *Manager) Create(fallbackBaseURL string) (Pairing, error) {
	now := m.now().UTC()
	code, err := randomHex(pairingCodeBytes)
	if err != nil {
		return Pairing{}, err
	}
	baseURLs := m.baseURLs
	if len(baseURLs) == 0 {
		baseURLs = normalizeBaseURLs([]string{fallbackBaseURL})
	}
	if len(baseURLs) == 0 {
		return Pairing{}, errors.New("pairing base url unavailable")
	}
	urls := make([]string, 0, len(baseURLs))
	for _, base := range baseURLs {
		urls = append(urls, pairingURL(base, code))
	}
	expiresAt := now.Add(m.ttl)
	qr, err := QRDataURL(urls[0])
	if err != nil {
		return Pairing{}, err
	}
	m.mu.Lock()
	m.gcLocked(now)
	m.pending[code] = expiresAt
	m.mu.Unlock()
	return Pairing{Code: code, URL: urls[0], URLs: urls, ExpiresAt: expiresAt, QRDataURL: qr}, nil
}

func (m *Manager) Claim(code, deviceName string) (ClaimResult, error) {
	now := m.now().UTC()
	code = strings.TrimSpace(code)
	m.mu.Lock()
	expiresAt, ok := m.pending[code]
	if !ok || !now.Before(expiresAt) {
		m.gcLocked(now)
		m.mu.Unlock()
		return ClaimResult{}, ErrPairingInvalid
	}
	delete(m.pending, code)
	m.mu.Unlock()

	device, token, err := m.devices.AddDevice(deviceName, now)
	if err != nil {
		return ClaimResult{}, err
	}
	return ClaimResult{Token: token, Device: device}, nil
}

func (m *Manager) gcLocked(now time.Time) {
	for code, expiresAt := range m.pending {
		if !now.Before(expiresAt) {
			delete(m.pending, code)
		}
	}
}

func QRDataURL(text string) (string, error) {
	png, err := qrcode.Encode(text, qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func normalizeBaseURLs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		base := normalizeBaseURL(value)
		if base == "" || seen[base] {
			continue
		}
		seen[base] = true
		out = append(out, base)
	}
	return out
}

func normalizeBaseURL(value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	u.Path = "/"
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}

func pairingURL(base, code string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	q.Set("pair", code)
	u.RawQuery = q.Encode()
	return u.String()
}
