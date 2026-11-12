package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeying-community/warehouse/internal/domain/share"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"go.uber.org/zap"
)

func TestResolvePublicDirectoryListsOnlyDirectSafeEntries(t *testing.T) {
	root := t.TempDir()
	shareRoot := filepath.Join(root, "docs")
	if err := os.MkdirAll(filepath.Join(shareRoot, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shareRoot, "readme.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shareRoot, "nested", "inside.txt"), []byte("inside"), 0644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "private.txt")
	if err := os.WriteFile(outside, []byte("private"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(shareRoot, "leak.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	svc, repo := newPublicDirectoryShareService(root)
	item, entries, hasMore, err := svc.ResolvePublicDirectory(context.Background(), "token", "", 0, 1)
	if err != nil {
		t.Fatalf("ResolvePublicDirectory() error = %v", err)
	}
	if item.Name != "docs" || len(entries) != 1 || !hasMore {
		t.Fatalf("unexpected first page: item=%+v entries=%+v hasMore=%v", item, entries, hasMore)
	}
	if entries[0].Name != "nested" || !entries[0].IsDir {
		t.Fatalf("expected sorted first entry to be nested directory, got %+v", entries[0])
	}
	_, secondPage, hasMore, err := svc.ResolvePublicDirectory(context.Background(), "token", "", 1, 1)
	if err != nil {
		t.Fatalf("ResolvePublicDirectory() second page error = %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].Name != "readme.txt" || hasMore {
		t.Fatalf("unexpected second page: entries=%+v hasMore=%v", secondPage, hasMore)
	}
	if _, _, _, err := svc.ResolvePublicDirectory(context.Background(), "token", "../", 0, 10); !errors.Is(err, share.ErrInvalidShare) {
		t.Fatalf("expected path traversal to be rejected, got %v", err)
	}
	if _, _, _, err := svc.ResolvePublicFile(context.Background(), "token", "leak.txt"); !errors.Is(err, share.ErrInvalidShare) {
		t.Fatalf("expected symlink outside root to be rejected, got %v", err)
	}
	if repo.item.Token != "token" {
		t.Fatalf("unexpected repository item: %+v", repo.item)
	}
}

func TestResolvePublicFileKeepsDownloadsInsideDirectoryShare(t *testing.T) {
	root := t.TempDir()
	shareRoot := filepath.Join(root, "docs")
	if err := os.MkdirAll(filepath.Join(shareRoot, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shareRoot, "nested", "readme.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "private.txt")
	if err := os.WriteFile(outside, []byte("private"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(shareRoot, "leak.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	svc, _ := newPublicDirectoryShareService(root)
	_, file, info, err := svc.ResolvePublicFile(context.Background(), "token", "nested/readme.txt")
	if err != nil {
		t.Fatalf("ResolvePublicFile() error = %v", err)
	}
	defer file.Close()
	if info.Name() != "readme.txt" {
		t.Fatalf("unexpected file: %q", info.Name())
	}
	content, err := os.ReadFile(filepath.Join(shareRoot, "nested", "readme.txt"))
	if err != nil || string(content) != "hello" {
		t.Fatalf("unexpected fixture content %q, error %v", content, err)
	}
	if _, _, _, err := svc.ResolvePublicFile(context.Background(), "token", "../../private.txt"); !errors.Is(err, share.ErrInvalidShare) {
		t.Fatalf("expected path traversal to be rejected, got %v", err)
	}
	if _, _, _, err := svc.ResolvePublicFile(context.Background(), "token", "nested/../nested/readme.txt"); !errors.Is(err, share.ErrInvalidShare) {
		t.Fatalf("expected non-canonical traversal to be rejected, got %v", err)
	}
	if _, _, _, err := svc.ResolvePublicFile(context.Background(), "token", "nested"); !errors.Is(err, share.ErrInvalidShare) {
		t.Fatalf("expected directory download to be rejected, got %v", err)
	}
	if _, _, _, err := svc.ResolvePublicFile(context.Background(), "token", "leak.txt"); !errors.Is(err, share.ErrInvalidShare) {
		t.Fatalf("expected symlink outside root to be rejected, got %v", err)
	}
}

func TestResolvePublicDirectoryRejectsExpiredAndFileShares(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	svc, repo := newPublicDirectoryShareService(root)
	repo.item.ExpiresAt = timePtr(time.Now().Add(-time.Minute))
	if _, _, _, err := svc.ResolvePublicDirectory(context.Background(), "token", "", 0, 10); !errors.Is(err, share.ErrShareExpired) {
		t.Fatalf("expected expired share to be rejected, got %v", err)
	}
	repo.item.ExpiresAt = nil
	repo.item.IsDir = false
	if _, _, _, err := svc.ResolvePublicDirectory(context.Background(), "token", "", 0, 10); !errors.Is(err, share.ErrInvalidShare) {
		t.Fatalf("expected file share to reject directory listing, got %v", err)
	}
}

func TestCreateAllowsDirectoryPublicShare(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0755); err != nil {
		t.Fatal(err)
	}
	svc, repo := newPublicDirectoryShareService(root)
	owner := &user.User{ID: "user", Username: "alice", Directory: root}
	created, err := svc.Create(context.Background(), owner, "/docs", ShareCreateInput{Mode: share.ModeDownload})
	if err != nil {
		t.Fatalf("Create() directory error = %v", err)
	}
	if !created.IsDir || created.Name != "docs" || !repo.created {
		t.Fatalf("directory share was not persisted correctly: item=%+v persisted=%v", created, repo.created)
	}
}

func newPublicDirectoryShareService(root string) (*ShareService, *publicDirectoryShareRepo) {
	repo := &publicDirectoryShareRepo{item: &share.ShareItem{Token: "token", UserID: "user", CreatorUserID: "user", Name: "docs", Path: "/docs", IsDir: true, Mode: share.ModeDownload}}
	users := &publicDirectoryUserRepo{user: &user.User{ID: "user", Username: "alice", Directory: root}}
	return NewShareService(repo, users, config.DefaultConfig(), zap.NewNop()), repo
}

type publicDirectoryShareRepo struct {
	item    *share.ShareItem
	created bool
}

func (r *publicDirectoryShareRepo) Create(_ context.Context, item *share.ShareItem) error {
	r.item = item
	r.created = true
	return nil
}
func (r *publicDirectoryShareRepo) GetByToken(context.Context, string) (*share.ShareItem, error) {
	return r.item, nil
}
func (r *publicDirectoryShareRepo) GetByUserID(context.Context, string) ([]*share.ShareItem, error) {
	return []*share.ShareItem{r.item}, nil
}
func (r *publicDirectoryShareRepo) DeleteByToken(context.Context, string) error     { return nil }
func (r *publicDirectoryShareRepo) IncrementView(context.Context, string) error     { return nil }
func (r *publicDirectoryShareRepo) IncrementDownload(context.Context, string) error { return nil }

type publicDirectoryUserRepo struct{ user *user.User }

func (r *publicDirectoryUserRepo) FindByUsername(context.Context, string) (*user.User, error) {
	return r.user, nil
}
func (r *publicDirectoryUserRepo) FindByWalletAddress(context.Context, string) (*user.User, error) {
	return r.user, nil
}
func (r *publicDirectoryUserRepo) FindByEmail(context.Context, string) (*user.User, error) {
	return r.user, nil
}
func (r *publicDirectoryUserRepo) FindByID(context.Context, string) (*user.User, error) {
	return r.user, nil
}
func (r *publicDirectoryUserRepo) Save(context.Context, *user.User) error               { return nil }
func (r *publicDirectoryUserRepo) Delete(context.Context, string) error                 { return nil }
func (r *publicDirectoryUserRepo) List(context.Context) ([]*user.User, error)           { return nil, nil }
func (r *publicDirectoryUserRepo) UpdateUsedSpace(context.Context, string, int64) error { return nil }
func (r *publicDirectoryUserRepo) UpdateUsedSpaceDelta(context.Context, string, int64) (int64, error) {
	return 0, nil
}
func (r *publicDirectoryUserRepo) UpdateQuota(context.Context, string, int64) error { return nil }

func timePtr(value time.Time) *time.Time { return &value }
