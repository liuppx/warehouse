package service

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	objectpath "github.com/yeying-community/warehouse/internal/domain/object"
	"github.com/yeying-community/warehouse/internal/domain/quota"
	"github.com/yeying-community/warehouse/internal/domain/user"
	"github.com/yeying-community/warehouse/internal/infrastructure/atomicfile"
	"github.com/yeying-community/warehouse/internal/infrastructure/config"
	"github.com/yeying-community/warehouse/internal/infrastructure/repository"
)

type ObjectInfo struct {
	Bucket      string
	Key         string
	Size        int64
	ETag        string
	ContentType string
	ModifiedAt  time.Time
	IsPrefix    bool
}

type ObjectWriteOptions struct {
	ExpectedMD5    string
	ExpectedSHA256 string
	ExpectedCRC32  string
	ExpectedETag   string
	CreateOnly     bool
	ETag           string
	ContentType    string
}

var (
	ErrObjectAlreadyExists      = errors.New("object already exists")
	ErrObjectPreconditionFailed = errors.New("object precondition failed")
)

type ObjectMetadata struct {
	ETag        string
	ContentType string
	UpdatedAt   time.Time
}

type ObjectList struct {
	Objects  []ObjectInfo
	Prefixes []string
}

// ObjectService contains filesystem operations shared by protocol adapters.
type ObjectService struct {
	webdavRoot       string
	quotaService     quota.Service
	userRepo         user.Repository
	mutationRecorder MutationRecorder
	shareConfig      *config.Config
	userShareRepo    repository.UserShareRepository
	publicShareRepo  repository.ShareRepository
	metadataRepo     objectMetadataRepository
	locks            sync.Map
}

func (s *ObjectService) SetShareReferences(cfg *config.Config, userShareRepo repository.UserShareRepository, publicShareRepo repository.ShareRepository) {
	s.shareConfig = cfg
	s.userShareRepo = userShareRepo
	s.publicShareRepo = publicShareRepo
}

type quotaReserveRepository interface {
	ReserveUsedSpaceDelta(context.Context, string, int64) (int64, error)
	ReleaseUsedSpaceDelta(context.Context, string, int64) error
}

type objectMetadataRepository interface {
	Upsert(context.Context, string, string, string, ObjectMetadata) error
	Find(context.Context, string, string, string) (*ObjectMetadata, error)
	Delete(context.Context, string, string, string) error
	ListByPrefix(context.Context, string, string, string) (map[string]ObjectMetadata, error)
}

func (s *ObjectService) lockPath(path string) func() {
	value, _ := s.locks.LoadOrStore(path, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func NewObjectService(webdavRoot string) *ObjectService {
	return &ObjectService{webdavRoot: filepath.Clean(webdavRoot)}
}

func (s *ObjectService) SetGuards(quotaService quota.Service, userRepo user.Repository, mutationRecorder MutationRecorder) {
	s.quotaService = quotaService
	s.userRepo = userRepo
	s.mutationRecorder = mutationRecorder
}

func (s *ObjectService) SetMetadataRepository(repo objectMetadataRepository) {
	s.metadataRepo = repo
}

func (s *ObjectService) PutForUser(ctx context.Context, owner *user.User, bucket, key string, src io.Reader) (ObjectInfo, error) {
	return s.putForUserWithOptions(ctx, owner, bucket, key, src, ObjectWriteOptions{})
}

func (s *ObjectService) PutForUserChecked(ctx context.Context, owner *user.User, bucket, key string, src io.Reader, expectedMD5, expectedSHA256, expectedCRC32 string) (ObjectInfo, error) {
	return s.putForUserWithOptions(ctx, owner, bucket, key, src, ObjectWriteOptions{
		ExpectedMD5:    expectedMD5,
		ExpectedSHA256: expectedSHA256,
		ExpectedCRC32:  expectedCRC32,
	})
}

func (s *ObjectService) PutForUserWithOptions(ctx context.Context, owner *user.User, bucket, key string, src io.Reader, options ObjectWriteOptions) (ObjectInfo, error) {
	return s.putForUserWithOptions(ctx, owner, bucket, key, src, options)
}

func (s *ObjectService) putForUserWithOptions(ctx context.Context, owner *user.User, bucket, key string, src io.Reader, options ObjectWriteOptions) (ObjectInfo, error) {
	if owner == nil {
		return ObjectInfo{}, fmt.Errorf("user is nil")
	}
	fullPath, err := objectpath.ResolvePath(s.webdavRoot, owner.Directory, bucket, key)
	if err != nil {
		return ObjectInfo{}, err
	}
	unlock := s.lockPath(fullPath)
	defer unlock()
	var oldSize int64
	if stat, statErr := os.Stat(fullPath); statErr == nil && !stat.IsDir() {
		oldSize = stat.Size()
		if options.CreateOnly {
			matches, matchErr := fileMatchesSHA256(fullPath, options.ExpectedSHA256)
			if matchErr != nil {
				return ObjectInfo{}, matchErr
			}
			if matches {
				return s.statObject(ctx, owner.Directory, bucket, key, fullPath, nil)
			}
			return ObjectInfo{}, ErrObjectAlreadyExists
		}
		if expected := normalizeETag(options.ExpectedETag); expected != "" {
			current, infoErr := s.statObject(ctx, owner.Directory, bucket, key, fullPath, nil)
			if infoErr != nil {
				return ObjectInfo{}, infoErr
			}
			if normalizeETag(current.ETag) != expected {
				return ObjectInfo{}, ErrObjectPreconditionFailed
			}
		}
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return ObjectInfo{}, statErr
	} else if normalizeETag(options.ExpectedETag) != "" {
		return ObjectInfo{}, ErrObjectPreconditionFailed
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return ObjectInfo{}, err
	}
	tmp, err := atomicfile.Open(fullPath, 0o644)
	if err != nil {
		return ObjectInfo{}, err
	}
	md5Hash := md5.New()
	sha256Hash := sha256.New()
	crc32Hash := crc32.NewIEEE()
	writer := io.MultiWriter(tmp, md5Hash, sha256Hash, crc32Hash)
	size, err := io.Copy(writer, src)
	if err != nil {
		tmp.Abort()
		return ObjectInfo{}, err
	}
	if err := validateChecksum(options.ExpectedMD5, md5Hash, options.ExpectedSHA256, sha256Hash, options.ExpectedCRC32, crc32Hash); err != nil {
		tmp.Abort()
		return ObjectInfo{}, err
	}
	delta := size - oldSize
	reserved := false
	var reservedUsed int64
	if reserveRepo, ok := s.userRepo.(quotaReserveRepository); ok && delta != 0 {
		reservedUsed, err = reserveRepo.ReserveUsedSpaceDelta(ctx, owner.Username, delta)
		if err != nil {
			tmp.Abort()
			return ObjectInfo{}, err
		}
		reserved = true
	} else if s.quotaService != nil {
		if err := s.quotaService.CheckQuota(ctx, owner, delta); err != nil {
			tmp.Abort()
			return ObjectInfo{}, err
		}
	}
	if err := tmp.Close(); err != nil {
		if reserved {
			_ = s.userRepo.(quotaReserveRepository).ReleaseUsedSpaceDelta(ctx, owner.Username, delta)
		}
		return ObjectInfo{}, err
	}
	if reserved {
		owner.UpdateUsedSpace(reservedUsed)
	} else if s.userRepo != nil && delta != 0 {
		used, err := s.userRepo.UpdateUsedSpaceDelta(ctx, owner.Username, delta)
		if err != nil {
			return ObjectInfo{}, err
		}
		owner.UpdateUsedSpace(used)
	}
	if s.mutationRecorder != nil {
		if err := s.mutationRecorder.UpsertFile(ctx, fullPath); err != nil {
			return ObjectInfo{}, err
		}
	}
	metadata := ObjectMetadata{
		ETag:        strings.TrimSpace(options.ETag),
		ContentType: strings.TrimSpace(options.ContentType),
		UpdatedAt:   time.Now(),
	}
	if metadata.ETag == "" {
		metadata.ETag = hex.EncodeToString(md5Hash.Sum(nil))
	}
	if metadata.ContentType == "" {
		metadata.ContentType = detectContentType(fullPath)
	}
	if err := s.upsertMetadata(ctx, owner.Directory, bucket, key, metadata); err != nil {
		return ObjectInfo{}, err
	}
	return s.statObject(ctx, owner.Directory, bucket, key, fullPath, map[string]ObjectMetadata{key: metadata})
}

func normalizeETag(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"`)
}

func fileMatchesSHA256(fullPath, expected string) (bool, error) {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false, nil
	}
	file, err := os.Open(fullPath)
	if err != nil {
		return false, err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return false, err
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil)) == expected, nil
}

func validateChecksum(expectedMD5 string, md5Hash hash.Hash, expectedSHA256 string, sha256Hash hash.Hash, expectedCRC32 string, crc32Hash hash.Hash) error {
	checks := []struct {
		name, expected string
		actual         hash.Hash
	}{
		{"Content-MD5", expectedMD5, md5Hash},
		{"x-amz-checksum-sha256", expectedSHA256, sha256Hash},
		{"x-amz-checksum-crc32", expectedCRC32, crc32Hash},
	}
	for _, check := range checks {
		if strings.TrimSpace(check.expected) == "" {
			continue
		}
		actual := base64.StdEncoding.EncodeToString(check.actual.Sum(nil))
		if actual != strings.TrimSpace(check.expected) {
			return fmt.Errorf("%s mismatch", check.name)
		}
	}
	return nil
}

func (s *ObjectService) DeleteForUser(ctx context.Context, owner *user.User, bucket, key string) error {
	if owner == nil {
		return fmt.Errorf("user is nil")
	}
	fullPath, err := objectpath.ResolvePath(s.webdavRoot, owner.Directory, bucket, key)
	if err != nil {
		return err
	}
	unlock := s.lockPath(fullPath)
	defer unlock()
	info, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("cannot delete directory object")
	}
	if err := os.Remove(fullPath); err != nil {
		return err
	}
	if err := RemoveAllShareReferencesForOwnerPath(ctx, s.userShareRepo, s.publicShareRepo, s.shareConfig, owner, fullPath); err != nil {
		return err
	}
	if err := s.deleteMetadata(ctx, owner.Directory, bucket, key); err != nil {
		return err
	}
	if s.userRepo != nil && info.Size() != 0 {
		used, err := s.userRepo.UpdateUsedSpaceDelta(ctx, owner.Username, -info.Size())
		if err != nil {
			return err
		}
		owner.UpdateUsedSpace(used)
	}
	if s.mutationRecorder != nil {
		return s.mutationRecorder.RemovePath(ctx, fullPath, false)
	}
	return nil
}

func (s *ObjectService) List(ctx context.Context, userDirectory, bucket, prefix string, delimiter rune) (ObjectList, error) {
	if err := ctx.Err(); err != nil {
		return ObjectList{}, err
	}
	base, err := objectpath.ResolvePath(s.webdavRoot, userDirectory, bucket, "")
	if err != nil {
		return ObjectList{}, err
	}
	if _, statErr := os.Stat(base); os.IsNotExist(statErr) {
		return ObjectList{}, nil
	} else if statErr != nil {
		return ObjectList{}, statErr
	}
	prefix = normalizeObjectKeyPrefix(prefix)
	result := ObjectList{Objects: make([]ObjectInfo, 0), Prefixes: make([]string, 0)}
	prefixes := make(map[string]struct{})
	var metadataByKey map[string]ObjectMetadata
	if s.metadataRepo != nil {
		metadataByKey, err = s.metadataRepo.ListByPrefix(ctx, userDirectory, bucket, prefix)
		if err != nil {
			return ObjectList{}, err
		}
	}
	err = filepath.WalkDir(base, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if current == base {
			return nil
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "._upload-") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		relPath, err := filepath.Rel(base, current)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(relPath)
		if entry.IsDir() {
			if prefix == "" || strings.HasPrefix(prefix, key+"/") || strings.HasPrefix(key+"/", prefix) {
				return nil
			}
			return filepath.SkipDir
		}
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			return nil
		}
		if delimiter != 0 {
			remainder := strings.TrimPrefix(key, prefix)
			if index := strings.IndexRune(remainder, delimiter); index >= 0 {
				commonPrefix := prefix + remainder[:index+1]
				prefixes[commonPrefix] = struct{}{}
				return nil
			}
		}
		item, err := s.statObject(ctx, userDirectory, bucket, key, current, metadataByKey)
		if err != nil {
			return err
		}
		result.Objects = append(result.Objects, item)
		return nil
	})
	if err != nil {
		return ObjectList{}, err
	}
	for commonPrefix := range prefixes {
		result.Prefixes = append(result.Prefixes, commonPrefix)
	}
	sort.Slice(result.Objects, func(i, j int) bool { return result.Objects[i].Key < result.Objects[j].Key })
	sort.Strings(result.Prefixes)
	return result, nil
}

func normalizeObjectKeyPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return ""
	}
	prefix = strings.ReplaceAll(prefix, "\\", "/")
	prefix = strings.TrimPrefix(prefix, "/")
	if strings.HasSuffix(prefix, "/") {
		clean := path.Clean("/" + prefix)
		if clean == "/" {
			return ""
		}
		return strings.TrimPrefix(clean, "/") + "/"
	}
	clean := path.Clean("/" + prefix)
	if clean == "/" {
		return ""
	}
	return strings.TrimPrefix(clean, "/")
}

func (s *ObjectService) EnsureBucket(ctx context.Context, userDirectory, bucket string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := objectpath.ResolvePath(s.webdavRoot, userDirectory, bucket, "")
	if err != nil {
		return err
	}
	return os.MkdirAll(fullPath, 0o755)
}

func (s *ObjectService) Stat(ctx context.Context, userDirectory, bucket, key string) (ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	fullPath, err := objectpath.ResolvePath(s.webdavRoot, userDirectory, bucket, key)
	if err != nil {
		return ObjectInfo{}, err
	}
	return s.statObject(ctx, userDirectory, bucket, key, fullPath, nil)
}

func (s *ObjectService) Open(ctx context.Context, userDirectory, bucket, key string) (*os.File, ObjectInfo, error) {
	info, err := s.Stat(ctx, userDirectory, bucket, key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	fullPath, err := objectpath.ResolvePath(s.webdavRoot, userDirectory, bucket, key)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	file, err := os.Open(fullPath)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	return file, info, nil
}

func (s *ObjectService) Put(ctx context.Context, userDirectory, bucket, key string, src io.Reader) (ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	fullPath, err := objectpath.ResolvePath(s.webdavRoot, userDirectory, bucket, key)
	if err != nil {
		return ObjectInfo{}, err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return ObjectInfo{}, err
	}
	if err := atomicfile.WriteAll(fullPath, src, 0o644); err != nil {
		return ObjectInfo{}, err
	}
	_ = s.deleteMetadata(ctx, userDirectory, bucket, key)
	return s.statObject(ctx, userDirectory, bucket, key, fullPath, nil)
}

func (s *ObjectService) Delete(ctx context.Context, userDirectory, bucket, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := objectpath.ResolvePath(s.webdavRoot, userDirectory, bucket, key)
	if err != nil {
		return err
	}
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return s.deleteMetadata(ctx, userDirectory, bucket, key)
}

func (s *ObjectService) statObject(ctx context.Context, userDirectory, bucket, key, fullPath string, metadataByKey map[string]ObjectMetadata) (ObjectInfo, error) {
	stat, err := os.Stat(fullPath)
	if err != nil {
		return ObjectInfo{}, err
	}
	if stat.IsDir() {
		return ObjectInfo{Bucket: bucket, Key: strings.TrimSuffix(key, "/") + "/", IsPrefix: true, ModifiedAt: stat.ModTime()}, nil
	}
	contentType := detectContentType(fullPath)
	etag := ""
	if metadata, ok := metadataByKey[key]; ok {
		etag = strings.TrimSpace(metadata.ETag)
		if strings.TrimSpace(metadata.ContentType) != "" {
			contentType = strings.TrimSpace(metadata.ContentType)
		}
	} else if metadata, err := s.findMetadata(ctx, userDirectory, bucket, key); err != nil {
		return ObjectInfo{}, err
	} else if metadata != nil {
		etag = strings.TrimSpace(metadata.ETag)
		if strings.TrimSpace(metadata.ContentType) != "" {
			contentType = strings.TrimSpace(metadata.ContentType)
		}
	}
	if etag == "" {
		etag, err = fallbackETag(fullPath, stat)
		if err != nil {
			return ObjectInfo{}, err
		}
	}
	return ObjectInfo{Bucket: bucket, Key: key, Size: stat.Size(), ETag: etag, ContentType: contentType, ModifiedAt: stat.ModTime()}, nil
}

func detectContentType(fullPath string) string {
	contentType := mime.TypeByExtension(filepath.Ext(fullPath))
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}

func (s *ObjectService) findMetadata(ctx context.Context, userDirectory, bucket, key string) (*ObjectMetadata, error) {
	if s.metadataRepo == nil || ctx == nil {
		return nil, nil
	}
	return s.metadataRepo.Find(ctx, userDirectory, bucket, key)
}

func (s *ObjectService) upsertMetadata(ctx context.Context, userDirectory, bucket, key string, metadata ObjectMetadata) error {
	if s.metadataRepo == nil {
		return nil
	}
	return s.metadataRepo.Upsert(ctx, userDirectory, bucket, key, metadata)
}

func (s *ObjectService) deleteMetadata(ctx context.Context, userDirectory, bucket, key string) error {
	if s.metadataRepo == nil {
		return nil
	}
	return s.metadataRepo.Delete(ctx, userDirectory, bucket, key)
}

func fallbackETag(path string, stat os.FileInfo) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
