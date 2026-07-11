package attachment

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/teatak/pudding-core/internal/store"
)

const (
	MaxUploadBytes   int64 = 20 << 20
	DraftSessionID         = "draft"
	OriginTemp             = "temp"
	OriginTool             = "tool"
	OriginUpload           = "upload"
	OriginASRAudio         = "asr_audio"
	OriginVoiceAudio       = "voice_audio"

	attachmentsDirName = "attachments"
	tempDirName        = "temp"
	sessionDirName     = "sessions"
	blobDirName        = "blobs"
)

var ErrTooLarge = errors.New("attachment too large")

type Service struct {
	home string
}

func NewService(home string) *Service {
	return &Service{home: strings.TrimSpace(home)}
}

func (s *Service) StoreReader(sessionID, name, mimeType string, r io.Reader) (store.Attachment, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return store.Attachment{}, errors.New("attachment: session id is required")
	}
	displayName := sanitizeName(name)
	if displayName == "" {
		return store.Attachment{}, errors.New("attachment: filename is required")
	}
	mimeType = normalizeMIME(mimeType)
	if mimeType == "" || mimeType == "application/octet-stream" {
		if alt := MIMEFromExt(displayName); alt != "" {
			mimeType = alt
		}
	}
	if !AllowedMIME(mimeType, displayName) {
		return store.Attachment{}, fmt.Errorf("attachment: mime %q not allowed", mimeType)
	}
	id, err := randomID()
	if err != nil {
		return store.Attachment{}, err
	}
	now := time.Now().UTC()
	ext := attachmentExt(displayName, mimeType)
	if ext == "" {
		ext = filepath.Ext(displayName)
	}
	finalName := now.Format("2006_01") + "_" + id + ext
	root, err := s.sessionRoot(sessionID)
	if err != nil {
		return store.Attachment{}, err
	}
	dir := filepath.Join(root, blobDirName)
	if sessionID == DraftSessionID {
		dir = root
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return store.Attachment{}, err
	}
	finalPath := filepath.Join(dir, finalName)
	dst, err := os.OpenFile(finalPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return store.Attachment{}, err
	}
	defer dst.Close()

	written, err := io.Copy(dst, io.LimitReader(r, MaxUploadBytes+1))
	if err != nil {
		_ = os.Remove(finalPath)
		return store.Attachment{}, err
	}
	if written > MaxUploadBytes {
		_ = os.Remove(finalPath)
		return store.Attachment{}, ErrTooLarge
	}
	key := filepath.ToSlash(filepath.Join(sessionDirName, sessionID, blobDirName, finalName))
	return store.Attachment{
		ID:            id,
		Name:          displayName,
		AttachmentKey: key,
		URL:           URL(sessionID, key),
		MIME:          mimeType,
		Size:          written,
		Origin:        OriginUpload,
		CreatedAt:     now.Format(time.RFC3339),
	}, nil
}

func (s *Service) StorePath(sessionID, path string) (store.Attachment, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return store.Attachment{}, errors.New("attachment: path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return store.Attachment{}, err
	}
	if info.IsDir() {
		return store.Attachment{}, errors.New("attachment: path is a directory")
	}
	if info.Size() > MaxUploadBytes {
		return store.Attachment{}, ErrTooLarge
	}
	file, err := os.Open(path)
	if err != nil {
		return store.Attachment{}, err
	}
	defer file.Close()
	name := filepath.Base(path)
	mimeType := MIMEFromExt(name)
	if mimeType == "" {
		var header [512]byte
		n, readErr := file.Read(header[:])
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return store.Attachment{}, readErr
		}
		mimeType = http.DetectContentType(header[:n])
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return store.Attachment{}, err
		}
	}
	return s.StoreReader(sessionID, name, mimeType, file)
}

func (s *Service) Delete(sessionID, key string) error {
	path, ok, err := s.Path(sessionID, key)
	if err != nil || !ok {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func WithSourcePath(item store.Attachment, path string) store.Attachment {
	item.SourcePath = strings.TrimSpace(path)
	return item
}

func (s *Service) Path(sessionID, raw string) (string, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	key := normalizeKey(raw)
	if sessionID == "" || key == "" {
		return "", false, nil
	}
	prefix := filepath.ToSlash(filepath.Join(sessionDirName, sessionID, blobDirName)) + "/"
	if !strings.HasPrefix(key, prefix) {
		// GET route passes only the wildcard after /attachments, so accept blobs/...
		shortPrefix := blobDirName + "/"
		if !strings.HasPrefix(key, shortPrefix) {
			return "", false, nil
		}
		key = filepath.ToSlash(filepath.Join(sessionDirName, sessionID, key))
	}
	root, err := s.root()
	if err != nil {
		return "", false, err
	}
	if sessionID == DraftSessionID {
		root, err = s.tempAttachmentRoot()
		if err != nil {
			return "", false, err
		}
		name := filepath.Base(filepath.FromSlash(key))
		if name == "." || name == string(filepath.Separator) {
			return "", false, nil
		}
		abs := filepath.Join(root, name)
		cleanRoot := filepath.Clean(root)
		cleanAbs := filepath.Clean(abs)
		if cleanAbs != cleanRoot && !strings.HasPrefix(cleanAbs, cleanRoot+string(os.PathSeparator)) {
			return "", false, nil
		}
		return cleanAbs, true, nil
	}
	abs := filepath.Join(root, filepath.FromSlash(key))
	cleanRoot := filepath.Clean(root)
	cleanAbs := filepath.Clean(abs)
	if cleanAbs != cleanRoot && !strings.HasPrefix(cleanAbs, cleanRoot+string(os.PathSeparator)) {
		return "", false, nil
	}
	return cleanAbs, true, nil
}

func (s *Service) CopyToSession(sourceSessionID, targetSessionID string, item store.Attachment) (store.Attachment, error) {
	sourceSessionID = strings.TrimSpace(sourceSessionID)
	targetSessionID = strings.TrimSpace(targetSessionID)
	if sourceSessionID == "" || targetSessionID == "" {
		return store.Attachment{}, errors.New("attachment: session id is required")
	}
	sourcePath, ok, err := s.Path(sourceSessionID, item.AttachmentKey)
	if err != nil {
		return store.Attachment{}, err
	}
	if !ok {
		return store.Attachment{}, os.ErrNotExist
	}
	key := normalizeKey(item.AttachmentKey)
	if key == "" {
		return store.Attachment{}, errors.New("attachment: key is required")
	}
	targetKey := filepath.ToSlash(filepath.Join(sessionDirName, targetSessionID, blobDirName, filepath.Base(key)))
	root, err := s.root()
	if err != nil {
		return store.Attachment{}, err
	}
	targetPath := filepath.Join(root, filepath.FromSlash(targetKey))
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return store.Attachment{}, err
	}
	if info, err := os.Stat(targetPath); err == nil && !info.IsDir() {
		out := item
		out.AttachmentKey = targetKey
		out.URL = URL(targetSessionID, targetKey)
		return out, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return store.Attachment{}, err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return store.Attachment{}, err
	}
	defer source.Close()
	target, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return store.Attachment{}, err
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		_ = os.Remove(targetPath)
		return store.Attachment{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(targetPath)
		return store.Attachment{}, closeErr
	}
	out := item
	out.AttachmentKey = targetKey
	out.URL = URL(targetSessionID, targetKey)
	return out, nil
}

func URL(sessionID, key string) string {
	key = normalizeKey(key)
	sessionID = strings.TrimSpace(sessionID)
	if key == "" || sessionID == "" {
		return ""
	}
	prefix := filepath.ToSlash(filepath.Join(sessionDirName, sessionID)) + "/"
	key = strings.TrimPrefix(key, prefix)
	return "/sessions/" + sessionID + "/attachments/" + key
}

func (s *Service) root() (string, error) {
	if s == nil || strings.TrimSpace(s.home) == "" {
		return "", errors.New("attachment: home dir is required")
	}
	return filepath.Join(s.home, attachmentsDirName), nil
}

func (s *Service) sessionRoot(sessionID string) (string, error) {
	root, err := s.root()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(sessionID) == DraftSessionID {
		root, err = s.tempAttachmentRoot()
		if err != nil {
			return "", err
		}
		return root, nil
	}
	return filepath.Join(root, sessionDirName, sessionID), nil
}

func (s *Service) tempAttachmentRoot() (string, error) {
	if s == nil || strings.TrimSpace(s.home) == "" {
		return "", errors.New("attachment: home dir is required")
	}
	return filepath.Join(s.home, tempDirName, attachmentsDirName), nil
}

func normalizeKey(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "/")
	raw = strings.TrimPrefix(raw, "attachments/")
	raw = strings.TrimPrefix(raw, "/attachments/")
	raw = strings.ReplaceAll(raw, "\\", "/")
	clean := filepath.ToSlash(filepath.Clean(raw))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return ""
	}
	return clean
}

func normalizeMIME(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if cleaned, _, err := mime.ParseMediaType(raw); err == nil {
		return strings.ToLower(cleaned)
	}
	return strings.ToLower(raw)
}

func AllowedMIME(mimeType, name string) bool {
	mimeType = normalizeMIME(mimeType)
	if strings.HasPrefix(mimeType, "image/") || strings.HasPrefix(mimeType, "audio/") || strings.HasPrefix(mimeType, "text/") {
		return true
	}
	switch mimeType {
	case "application/json", "application/pdf", "application/xml", "application/x-yaml", "application/yaml":
		return true
	}
	if mimeType == "" || mimeType == "application/octet-stream" {
		return MIMEFromExt(name) != ""
	}
	return false
}

func MIMEFromExt(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	case ".txt", ".log", ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".rs", ".java", ".c", ".cpp", ".h", ".css", ".html", ".sh":
		return "text/plain"
	case ".md":
		return "text/markdown"
	case ".csv":
		return "text/csv"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".xml":
		return "application/xml"
	case ".pdf":
		return "application/pdf"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/mp4"
	case ".aac":
		return "audio/aac"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".flac":
		return "audio/flac"
	case ".opus":
		return "audio/opus"
	case ".webm":
		return "audio/webm"
	default:
		return ""
	}
}

func sanitizeName(raw string) string {
	base := filepath.Base(strings.TrimSpace(raw))
	base = strings.ReplaceAll(base, "\\", "/")
	base = filepath.Base(base)
	if base == "." || base == ".." {
		return ""
	}
	var b strings.Builder
	prevUnderscore := false
	for _, r := range base {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			if !prevUnderscore {
				b.WriteRune('_')
				prevUnderscore = true
			}
		case unicode.IsControl(r) || !unicode.IsPrint(r):
			if !prevUnderscore {
				b.WriteRune('_')
				prevUnderscore = true
			}
		case unicode.IsSpace(r):
			if !prevUnderscore {
				b.WriteRune('_')
				prevUnderscore = true
			}
		default:
			b.WriteRune(r)
			prevUnderscore = false
		}
	}
	out := strings.Trim(b.String(), "._")
	if len(out) > 120 {
		ext := filepath.Ext(out)
		head := out
		if len(ext) < 120 {
			head = out[:120-len(ext)]
		} else {
			ext = ""
			head = out[:120]
		}
		out = head + ext
	}
	return out
}

func attachmentExt(name, mimeType string) string {
	if ext := strings.ToLower(filepath.Ext(name)); ext != "" && len(ext) <= 12 {
		return ext
	}
	switch normalizeMIME(mimeType) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "audio/wav", "audio/wave", "audio/x-wav":
		return ".wav"
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4":
		return ".m4a"
	case "application/pdf":
		return ".pdf"
	case "application/json":
		return ".json"
	default:
		return ""
	}
}

func randomID() (string, error) {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("attachment: random id: %w", err)
	}
	return "att_" + hex.EncodeToString(buf[:]), nil
}
