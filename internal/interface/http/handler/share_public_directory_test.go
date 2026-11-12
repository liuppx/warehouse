package handler

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/share"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"go.uber.org/zap"
)

func TestHandleDirectoryAccessListsAndDownloadsWithoutAuthentication(t *testing.T) {
	handler, root, item := newPublicDirectoryHandler(t)

	listRequest := httptest.NewRequest("GET", "/api/v1/public/share/directory/token/entries", nil)
	listResponse := httptest.NewRecorder()
	handler.HandleDirectoryAccess(listResponse, listRequest)
	if listResponse.Code != 200 {
		t.Fatalf("directory listing status = %d, body=%s", listResponse.Code, listResponse.Body.String())
	}
	if listResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected no-store cache policy, got %q", listResponse.Header().Get("Cache-Control"))
	}
	if !strings.Contains(listResponse.Body.String(), `"name":"nested"`) {
		t.Fatalf("directory listing omitted nested folder: %s", listResponse.Body.String())
	}
	if !strings.Contains(listResponse.Body.String(), `"mode":"download"`) {
		t.Fatalf("directory listing omitted share mode: %s", listResponse.Body.String())
	}

	downloadRequest := httptest.NewRequest("GET", "/api/v1/public/share/directory/token/download?path=nested%2Freadme.txt", nil)
	downloadResponse := httptest.NewRecorder()
	handler.HandleDirectoryAccess(downloadResponse, downloadRequest)
	if downloadResponse.Code != 200 || downloadResponse.Body.String() != "public contents" {
		t.Fatalf("directory download status=%d body=%q", downloadResponse.Code, downloadResponse.Body.String())
	}
	if !strings.Contains(downloadResponse.Header().Get("Content-Disposition"), "readme.txt") {
		t.Fatalf("download filename missing: %q", downloadResponse.Header().Get("Content-Disposition"))
	}

	item.Mode = share.ModePreview
	previewList := httptest.NewRecorder()
	handler.HandleDirectoryAccess(previewList, listRequest)
	if !strings.Contains(previewList.Body.String(), `"mode":"preview"`) {
		t.Fatalf("directory listing omitted preview mode: %s", previewList.Body.String())
	}
	previewRequest := httptest.NewRequest("GET", "/api/v1/public/share/directory/token/download?path=nested%2Freadme.txt", nil)
	previewResponse := httptest.NewRecorder()
	handler.HandleDirectoryAccess(previewResponse, previewRequest)
	if !strings.HasPrefix(previewResponse.Header().Get("Content-Disposition"), "inline") {
		t.Fatalf("preview mode should open supported files inline, got %q", previewResponse.Header().Get("Content-Disposition"))
	}
	if !item.IsDir || root == "" {
		t.Fatal("invalid test fixture")
	}
}

func TestHandleDirectoryAccessRejectsTraversalAndUnsupportedMethods(t *testing.T) {
	handler, _, _ := newPublicDirectoryHandler(t)
	request := httptest.NewRequest("GET", "/api/v1/public/share/directory/token/download?path=nested%2F..%2F..%2Fsecret.txt", nil)
	response := httptest.NewRecorder()
	handler.HandleDirectoryAccess(response, request)
	if response.Code != 404 {
		t.Fatalf("path traversal status = %d, want 404", response.Code)
	}

	request = httptest.NewRequest("POST", "/api/v1/public/share/directory/token/entries", nil)
	response = httptest.NewRecorder()
	handler.HandleDirectoryAccess(response, request)
	if response.Code != 405 {
		t.Fatalf("unsupported method status = %d, want 405", response.Code)
	}
}

func TestHandleAccessRedirectsDirectoryShareToGuestPage(t *testing.T) {
	handler, _, _ := newPublicDirectoryHandler(t)
	request := httptest.NewRequest("GET", "/api/v1/public/share/token", nil)
	response := httptest.NewRecorder()
	handler.HandleAccess(response, request)
	if response.Code != 302 || response.Header().Get("Location") != "/share/token" {
		t.Fatalf("directory share redirect status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
}

func newPublicDirectoryHandler(t *testing.T) (*ShareHandler, string, *share.ShareItem) {
	t.Helper()
	root := t.TempDir()
	sharedRoot := filepath.Join(root, "shared")
	if err := os.MkdirAll(filepath.Join(sharedRoot, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sharedRoot, "nested", "readme.txt"), []byte("public contents"), 0644); err != nil {
		t.Fatal(err)
	}
	item := &share.ShareItem{Token: "token", UserID: "owner", CreatorUserID: "owner", Name: "shared", Path: "/shared", IsDir: true, Mode: share.ModeDownload}
	repo := &publicDirectoryHandlerShareRepo{item: item}
	users := &publicDirectoryHandlerUserRepo{u: &user.User{ID: "owner", Username: "owner", Directory: root}}
	svc := service.NewShareService(repo, users, config.DefaultConfig(), zap.NewNop())
	return NewShareHandler(svc, zap.NewNop()), root, item
}

type publicDirectoryHandlerShareRepo struct{ item *share.ShareItem }

func (r *publicDirectoryHandlerShareRepo) Create(context.Context, *share.ShareItem) error { return nil }
func (r *publicDirectoryHandlerShareRepo) GetByToken(context.Context, string) (*share.ShareItem, error) {
	return r.item, nil
}
func (r *publicDirectoryHandlerShareRepo) GetByUserID(context.Context, string) ([]*share.ShareItem, error) {
	return []*share.ShareItem{r.item}, nil
}
func (r *publicDirectoryHandlerShareRepo) DeleteByToken(context.Context, string) error { return nil }
func (r *publicDirectoryHandlerShareRepo) IncrementView(context.Context, string) error { return nil }
func (r *publicDirectoryHandlerShareRepo) IncrementDownload(context.Context, string) error {
	return nil
}

type publicDirectoryHandlerUserRepo struct{ u *user.User }

func (r *publicDirectoryHandlerUserRepo) FindByUsername(context.Context, string) (*user.User, error) {
	return r.u, nil
}
func (r *publicDirectoryHandlerUserRepo) FindByWalletAddress(context.Context, string) (*user.User, error) {
	return r.u, nil
}
func (r *publicDirectoryHandlerUserRepo) FindByEmail(context.Context, string) (*user.User, error) {
	return r.u, nil
}
func (r *publicDirectoryHandlerUserRepo) FindByID(context.Context, string) (*user.User, error) {
	return r.u, nil
}
func (r *publicDirectoryHandlerUserRepo) Save(context.Context, *user.User) error { return nil }
func (r *publicDirectoryHandlerUserRepo) Delete(context.Context, string) error   { return nil }
func (r *publicDirectoryHandlerUserRepo) List(context.Context) ([]*user.User, error) {
	return []*user.User{r.u}, nil
}
func (r *publicDirectoryHandlerUserRepo) UpdateUsedSpace(context.Context, string, int64) error {
	return nil
}
func (r *publicDirectoryHandlerUserRepo) UpdateUsedSpaceDelta(context.Context, string, int64) (int64, error) {
	return 0, nil
}
func (r *publicDirectoryHandlerUserRepo) UpdateQuota(context.Context, string, int64) error {
	return nil
}
