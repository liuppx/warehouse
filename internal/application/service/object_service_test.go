package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yeying-community/warehouse/internal/domain/user"
)

func TestObjectServiceConditionalPut(t *testing.T) {
	root := t.TempDir()
	svc := NewObjectService(root)
	ctx := context.Background()
	owner := &user.User{ID: "u1", Username: "alice", Directory: "alice"}

	initial, err := svc.Put(ctx, owner.Directory, "personal", "review.md", strings.NewReader("initial"))
	if err != nil {
		t.Fatalf("seed object: %v", err)
	}
	digest := sha256.Sum256([]byte("initial"))
	expectedSHA := base64.StdEncoding.EncodeToString(digest[:])
	retry, err := svc.PutForUserWithOptions(ctx, owner, "personal", "review.md", strings.NewReader("initial"), ObjectWriteOptions{
		ExpectedSHA256: expectedSHA,
		CreateOnly:     true,
	})
	if err != nil || retry.ETag != initial.ETag {
		t.Fatalf("idempotent create retry: info=%+v err=%v", retry, err)
	}

	_, err = svc.PutForUserWithOptions(ctx, owner, "personal", "review.md", strings.NewReader("different"), ObjectWriteOptions{
		CreateOnly: true,
	})
	if !errors.Is(err, ErrObjectAlreadyExists) {
		t.Fatalf("expected object exists, got %v", err)
	}

	_, err = svc.PutForUserWithOptions(ctx, owner, "personal", "review.md", strings.NewReader("updated"), ObjectWriteOptions{
		ExpectedETag: initial.ETag,
	})
	if err != nil {
		t.Fatalf("conditional overwrite: %v", err)
	}
	_, err = svc.PutForUserWithOptions(ctx, owner, "personal", "review.md", strings.NewReader("stale"), ObjectWriteOptions{
		ExpectedETag: initial.ETag,
	})
	if !errors.Is(err, ErrObjectPreconditionFailed) {
		t.Fatalf("expected precondition failure, got %v", err)
	}
}

func TestObjectServicePutListOpenDelete(t *testing.T) {
	root := t.TempDir()
	svc := NewObjectService(root)
	ctx := context.Background()

	info, err := svc.Put(ctx, "alice", "personal", "docs/note.txt", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("put object: %v", err)
	}
	if info.Size != 5 || info.ContentType != "text/plain; charset=utf-8" || info.ETag == "" {
		t.Fatalf("unexpected object info: %+v", info)
	}

	listed, err := svc.List(ctx, "alice", "personal", "docs/", '/')
	if err != nil {
		t.Fatalf("list objects: %v", err)
	}
	if len(listed.Objects) != 1 || listed.Objects[0].Key != "docs/note.txt" {
		t.Fatalf("unexpected list result: %+v", listed)
	}

	file, _, err := svc.Open(ctx, "alice", "personal", "docs/note.txt")
	if err != nil {
		t.Fatalf("open object: %v", err)
	}
	content, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil || string(content) != "hello" {
		t.Fatalf("unexpected content: content=%q err=%v", content, err)
	}

	if err := svc.Delete(ctx, "alice", "personal", "docs/note.txt"); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "alice", "personal", "docs", "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected object to be deleted, got err=%v", err)
	}
}

func TestObjectServiceListReturnsPrefixes(t *testing.T) {
	root := t.TempDir()
	svc := NewObjectService(root)
	ctx := context.Background()
	if _, err := svc.Put(ctx, "alice", "personal", "docs/note.txt", strings.NewReader("hello")); err != nil {
		t.Fatalf("put object: %v", err)
	}
	listed, err := svc.List(ctx, "alice", "personal", "", '/')
	if err != nil {
		t.Fatalf("list objects: %v", err)
	}
	if len(listed.Prefixes) != 1 || listed.Prefixes[0] != "docs/" {
		t.Fatalf("unexpected prefixes: %+v", listed.Prefixes)
	}
}

func TestObjectServiceListDoesNotInventObjectsForMissingPrefix(t *testing.T) {
	root := t.TempDir()
	svc := NewObjectService(root)
	ctx := context.Background()
	if _, err := svc.Put(ctx, "alice", "personal", "other/file.txt", strings.NewReader("hello")); err != nil {
		t.Fatalf("put object: %v", err)
	}
	listed, err := svc.List(ctx, "alice", "personal", "missing/", '/')
	if err != nil {
		t.Fatalf("list objects: %v", err)
	}
	if len(listed.Objects) != 0 || len(listed.Prefixes) != 0 {
		t.Fatalf("unexpected list result for missing prefix: %+v", listed)
	}
}

func TestObjectServicePersistsAndReadsMetadata(t *testing.T) {
	root := t.TempDir()
	svc := NewObjectService(root)
	repo := &testObjectMetadataRepo{items: make(map[string]ObjectMetadata)}
	svc.SetMetadataRepository(repo)
	owner := &user.User{Username: "alice", Directory: "alice"}
	ctx := context.Background()

	info, err := svc.PutForUserWithOptions(ctx, owner, "personal", "docs/report.bin", strings.NewReader("hello"), ObjectWriteOptions{
		ETag:        "custom-etag",
		ContentType: "application/custom",
	})
	if err != nil {
		t.Fatalf("put object with metadata: %v", err)
	}
	if info.ETag != "custom-etag" || info.ContentType != "application/custom" {
		t.Fatalf("unexpected info: %+v", info)
	}

	stat, err := svc.Stat(ctx, "alice", "personal", "docs/report.bin")
	if err != nil {
		t.Fatalf("stat object: %v", err)
	}
	if stat.ETag != "custom-etag" || stat.ContentType != "application/custom" {
		t.Fatalf("unexpected stat: %+v", stat)
	}

	if err := svc.DeleteForUser(ctx, owner, "personal", "docs/report.bin"); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	if _, ok := repo.items["alice|personal|docs/report.bin"]; ok {
		t.Fatal("expected metadata to be deleted")
	}
}

type testObjectMetadataRepo struct {
	items map[string]ObjectMetadata
}

func (r *testObjectMetadataRepo) Upsert(_ context.Context, userDirectory, bucket, key string, metadata ObjectMetadata) error {
	r.items[r.key(userDirectory, bucket, key)] = metadata
	return nil
}

func (r *testObjectMetadataRepo) Find(_ context.Context, userDirectory, bucket, key string) (*ObjectMetadata, error) {
	item, ok := r.items[r.key(userDirectory, bucket, key)]
	if !ok {
		return nil, nil
	}
	copy := item
	return &copy, nil
}

func (r *testObjectMetadataRepo) Delete(_ context.Context, userDirectory, bucket, key string) error {
	delete(r.items, r.key(userDirectory, bucket, key))
	return nil
}

func (r *testObjectMetadataRepo) ListByPrefix(_ context.Context, userDirectory, bucket, prefix string) (map[string]ObjectMetadata, error) {
	result := make(map[string]ObjectMetadata)
	for composite, item := range r.items {
		parts := strings.SplitN(composite, "|", 4)
		if len(parts) != 4 || parts[0] != userDirectory || parts[1] != bucket {
			continue
		}
		if prefix != "" && !strings.HasPrefix(parts[3], prefix) {
			continue
		}
		result[parts[3]] = item
	}
	return result, nil
}

func (r *testObjectMetadataRepo) key(userDirectory, bucket, key string) string {
	return userDirectory + "|" + bucket + "|" + key
}
