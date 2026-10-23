package projectgit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MutationRequest identifies one reviewed operation. Receipts live in local Git
// metadata and survive daemon restarts without becoming tracked project files.
type MutationRequest struct {
	ID               string
	Scope            string
	ExpectedRevision string
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)
var revisionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func CommitOnce(ctx context.Context, repo Repository, message string, stageAll bool, request MutationRequest) (Status, error) {
	if request.ID == "" {
		return Commit(ctx, repo, message, stageAll)
	}
	return mutateOnce(ctx, repo, request, "commit", []any{strings.TrimSpace(message), stageAll}, func() (Status, error) {
		return commit(ctx, repo, message, stageAll)
	})
}

func CreateBranchOnce(ctx context.Context, repo Repository, name string, request MutationRequest) (Status, error) {
	if request.ID == "" {
		return CreateBranch(ctx, repo, name)
	}
	return mutateOnce(ctx, repo, request, "branch-create", strings.TrimSpace(name), func() (Status, error) {
		return createBranch(ctx, repo, name)
	})
}

func mutateOnce(ctx context.Context, repo Repository, request MutationRequest, operation string, payload any, execute func() (Status, error)) (Status, error) {
	if !requestIDPattern.MatchString(request.ID) || !revisionPattern.MatchString(request.ExpectedRevision) {
		return Status{}, newError(CodeInvalidRequest, "Request identity and reviewed Git revision are required", nil)
	}
	body, _ := json.Marshal([]any{operation, request.ExpectedRevision, payload})
	fingerprint := sha256.Sum256(body)
	identity := sha256.Sum256([]byte(request.Scope + "\x00" + operation + "\x00" + request.ID))
	var status Status
	err := withRepositoryLock(repo.Root, func() error {
		location := run(ctx, repo.Root, metadataLimitBytes, "rev-parse", "--git-path", "pudding/requests/"+hex.EncodeToString(identity[:])+".json")
		if location.err != nil {
			return commandError(ctx, CodeReceiptFailed, location)
		}
		receiptPath := strings.TrimSpace(location.stdout)
		if !filepath.IsAbs(receiptPath) {
			receiptPath = filepath.Join(repo.Root, receiptPath)
		}
		// These two directories are Pudding-owned metadata, not project paths.
		// Refuse links so a repository cannot redirect receipt writes elsewhere.
		for _, dir := range []string{filepath.Dir(filepath.Dir(receiptPath)), filepath.Dir(receiptPath)} {
			if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return newError(CodeReceiptFailed, "Unable to create Git receipt directory", err)
			}
			info, err := os.Lstat(dir)
			if err != nil || !info.IsDir() {
				return newError(CodeReceiptFailed, "Git receipt directory is not a directory", err)
			}
		}
		existing, err := os.ReadFile(receiptPath)
		if err == nil {
			if string(existing) != hex.EncodeToString(fingerprint[:]) {
				return newError(CodeRequestConflict, "Request identity was already used with different parameters", nil)
			}
			status, err = ReadStatus(ctx, repo)
			return err
		}
		if !errors.Is(err, os.ErrNotExist) {
			return newError(CodeReceiptFailed, "Unable to read Git operation receipt", err)
		}
		current, err := ReadStatus(ctx, repo)
		if err != nil {
			return err
		}
		if current.Revision != request.ExpectedRevision {
			return newError(CodeStateConflict, "Git state changed; review it before starting another operation", nil)
		}
		status, err = execute()
		if err != nil {
			return err
		}
		f, err := os.CreateTemp(filepath.Dir(receiptPath), ".receipt-*")
		if err != nil {
			return newError(CodeReceiptFailed, "Git changed but its receipt could not be saved; review repository state", err)
		}
		defer os.Remove(f.Name())
		_, err = f.WriteString(hex.EncodeToString(fingerprint[:]))
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(f.Name(), receiptPath)
		}
		if err != nil {
			return newError(CodeReceiptFailed, "Git changed but its receipt could not be saved; review repository state", err)
		}
		return nil
	})
	return status, err
}
