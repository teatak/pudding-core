package skill

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var ErrInvalidID = errors.New("skill: invalid id")
var ErrInvalidAsset = errors.New("skill: invalid asset")
var ErrNotFound = errors.New("skill: not found")

type Service struct {
	homeDir string
}

func NewService(homeDir string) *Service {
	return &Service{homeDir: homeDir}
}

func (s *Service) ListSkills(context.Context) ([]Skill, error) {
	builtin, err := LoadBuiltinSkills()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(builtin))
	out := make([]Skill, 0, len(builtin))
	for _, item := range builtin {
		seen[item.ID] = true
		out = append(out, cloneSkill(item))
	}
	if s.homeDir == "" {
		return out, nil
	}
	user, err := LoadUserSkills(filepath.Join(s.homeDir, "skills"))
	if err != nil {
		return nil, err
	}
	for _, item := range user {
		if seen[item.ID] {
			continue
		}
		out = append(out, cloneSkill(item))
	}
	sortSkills(out)
	return out, nil
}

func (s *Service) DeleteSkill(_ context.Context, id string) error {
	id = strings.TrimSpace(id)
	if !skillIDPattern.MatchString(id) {
		return ErrInvalidID
	}
	if s.homeDir == "" {
		return errors.New("skill: home dir is required")
	}
	return os.RemoveAll(filepath.Join(s.homeDir, "skills", id))
}

func (s *Service) ReadSkill(ctx context.Context, id string) (*Document, error) {
	id = strings.TrimSpace(id)
	if !skillIDPattern.MatchString(id) {
		return nil, ErrInvalidID
	}
	items, err := s.ListSkills(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.ID != id {
			continue
		}
		content, err := s.readSkillContent(item)
		if err != nil {
			return nil, err
		}
		return &Document{Skill: cloneSkill(item), Content: content}, nil
	}
	return nil, ErrNotFound
}

func (s *Service) readSkillContent(item Skill) (string, error) {
	switch item.Source {
	case SourceBuiltin:
		data, err := builtinFS.ReadFile(path.Join("embed", item.ID, SkillFileName))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return "", ErrNotFound
			}
			return "", err
		}
		return string(data), nil
	case SourceUser:
		if s.homeDir == "" {
			return "", ErrNotFound
		}
		data, err := os.ReadFile(filepath.Join(s.homeDir, "skills", item.ID, SkillFileName))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", ErrNotFound
			}
			return "", err
		}
		return string(data), nil
	default:
		return "", ErrNotFound
	}
}

func (s *Service) ReadAsset(_ context.Context, rel string) ([]byte, string, error) {
	rel = path.Clean(strings.TrimPrefix(strings.TrimSpace(rel), "/"))
	if rel == "." || strings.Contains(rel, "..") {
		return nil, "", ErrInvalidAsset
	}
	parts := strings.Split(rel, "/")
	offset := 0
	system := false
	draft := false
	if parts[0] == SystemSubdir {
		if len(parts) != 4 {
			return nil, "", ErrInvalidAsset
		}
		system = true
		offset = 1
	} else if parts[0] == DraftDirName {
		if len(parts) != 4 {
			return nil, "", ErrInvalidAsset
		}
		draft = true
		offset = 1
	} else if len(parts) != 3 {
		return nil, "", ErrInvalidAsset
	}
	id := parts[offset]
	if !skillIDPattern.MatchString(id) || parts[offset+1] != "assets" {
		return nil, "", ErrInvalidAsset
	}
	name := parts[offset+2]
	contentType, ok := iconContentType(name)
	if !ok {
		return nil, "", ErrInvalidAsset
	}
	if system {
		data, err := builtinFS.ReadFile(path.Join("embed", id, "assets", name))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, "", ErrInvalidAsset
			}
			return nil, "", err
		}
		return data, contentType, nil
	}
	if s.homeDir == "" {
		return nil, "", ErrInvalidAsset
	}
	root := "skills"
	if draft {
		root = DraftDirName
	}
	data, err := os.ReadFile(filepath.Join(s.homeDir, root, id, "assets", name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", ErrInvalidAsset
		}
		return nil, "", err
	}
	return data, contentType, nil
}

func iconContentType(name string) (string, bool) {
	switch name {
	case "icon.png":
		return "image/png", true
	case "icon.jpg":
		return "image/jpeg", true
	case "icon.svg":
		return "image/svg+xml", true
	default:
		return "", false
	}
}
