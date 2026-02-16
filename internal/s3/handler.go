package s3

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Syncer is called after PUT/DELETE to trigger a background sync (e.g. git commit+push).
type Syncer interface {
	Trigger()
}

type Handler struct {
	dir       string
	bucket    string
	accessKey string
	secretKey string
	region    string
	syncer    Syncer
}

// NewHandler creates an S3-compatible HTTP handler.
func NewHandler(dir, bucket, accessKey, secretKey, region string, syncer Syncer) *Handler {
	return &Handler{
		dir:       dir,
		bucket:    bucket,
		accessKey: accessKey,
		secretKey: secretKey,
		region:    region,
		syncer:    syncer,
	}
}

func (s *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, DELETE, HEAD, POST")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Expose-Headers", "ETag, x-amz-request-id, x-amz-id-2")
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Auth
	if s.accessKey != "" {
		if !sigV4Verify(r, s.accessKey, s.secretKey, s.region) {
			s.xmlError(w, http.StatusForbidden, "AccessDenied", "Invalid signature")
			return
		}
	}

	// Route: /{bucket} or /{bucket}/{key...}
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	bucket := parts[0]
	key := ""
	if len(parts) > 1 {
		key = parts[1]
	}

	// Bucket-level operations
	if key == "" {
		switch r.Method {
		case "GET":
			s.listObjectsV2(w, r, bucket)
		case "HEAD":
			if bucket == s.bucket {
				w.WriteHeader(http.StatusOK)
			} else {
				s.xmlError(w, http.StatusNotFound, "NoSuchBucket", "Bucket not found")
			}
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}

	// Object-level operations
	switch r.Method {
	case "PUT":
		s.putObject(w, r, key)
	case "GET":
		s.getObject(w, r, key)
	case "HEAD":
		s.headObject(w, r, key)
	case "DELETE":
		s.deleteObject(w, r, key)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Handler) listObjectsV2(w http.ResponseWriter, r *http.Request, bucket string) {
	prefix := r.URL.Query().Get("prefix")
	maxKeys := 1000
	if v := r.URL.Query().Get("max-keys"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxKeys = n
		}
	}

	var objects []ObjectInfo
	root := s.dir

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, _ := filepath.Rel(root, path)
		relPath = filepath.ToSlash(relPath)

		if prefix != "" && !strings.HasPrefix(relPath, prefix) {
			return nil
		}

		if len(objects) >= maxKeys {
			return filepath.SkipAll
		}

		etag := fmt.Sprintf("\"%s\"", hashSHA256([]byte(relPath+info.ModTime().String())))
		objects = append(objects, ObjectInfo{
			Key:          relPath,
			LastModified: info.ModTime().UTC().Format(time.RFC3339),
			ETag:         etag,
			Size:         info.Size(),
			StorageClass: "STANDARD",
		})
		return nil
	})

	result := ListBucketResult{
		Xmlns:       "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:        bucket,
		Prefix:      prefix,
		KeyCount:    len(objects),
		MaxKeys:     maxKeys,
		IsTruncated: false,
		Contents:    objects,
	}

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(result)
}

func (s *Handler) putObject(w http.ResponseWriter, r *http.Request, key string) {
	fullPath := filepath.Join(s.dir, filepath.FromSlash(key))

	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		s.xmlError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	f, err := os.Create(fullPath)
	if err != nil {
		s.xmlError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	defer f.Close()

	if _, err := io.Copy(f, r.Body); err != nil {
		s.xmlError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	f.Seek(0, 0)
	h := sha256.New()
	io.Copy(h, f)
	etag := fmt.Sprintf("\"%s\"", hex.EncodeToString(h.Sum(nil))[:32])

	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)

	log.Printf("[s3] PUT %s", key)
	s.syncer.Trigger()
}

func (s *Handler) getObject(w http.ResponseWriter, r *http.Request, key string) {
	fullPath := filepath.Join(s.dir, filepath.FromSlash(key))

	info, err := os.Stat(fullPath)
	if err != nil {
		s.xmlError(w, http.StatusNotFound, "NoSuchKey", "Object not found")
		return
	}

	f, err := os.Open(fullPath)
	if err != nil {
		s.xmlError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}
	defer f.Close()

	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
	io.Copy(w, f)

	log.Printf("[s3] GET %s", key)
}

func (s *Handler) headObject(w http.ResponseWriter, r *http.Request, key string) {
	fullPath := filepath.Join(s.dir, filepath.FromSlash(key))

	info, err := os.Stat(fullPath)
	if err != nil {
		s.xmlError(w, http.StatusNotFound, "NoSuchKey", "Object not found")
		return
	}

	etag := fmt.Sprintf("\"%s\"", hashSHA256([]byte(key+info.ModTime().String())))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
}

func (s *Handler) deleteObject(w http.ResponseWriter, r *http.Request, key string) {
	fullPath := filepath.Join(s.dir, filepath.FromSlash(key))

	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		s.xmlError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	// Clean up empty parent directories
	dir := filepath.Dir(fullPath)
	for dir != s.dir {
		entries, _ := os.ReadDir(dir)
		if len(entries) > 0 {
			break
		}
		os.Remove(dir)
		dir = filepath.Dir(dir)
	}

	w.WriteHeader(http.StatusNoContent)
	log.Printf("[s3] DELETE %s", key)
	s.syncer.Trigger()
}

func (s *Handler) xmlError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	xml.NewEncoder(w).Encode(ErrorResponse{Code: code, Message: message})
}
