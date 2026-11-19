package main

import (
	"context"
	"encoding/base64"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yixiaozi/imgmesh/server/internal/auth"
	"github.com/yixiaozi/imgmesh/server/internal/blob"
	"github.com/yixiaozi/imgmesh/server/internal/httpapi"
	"github.com/yixiaozi/imgmesh/server/internal/store"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	cfg, err := load()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	st, err := store.Open(ctx, cfg.databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if cfg.quota > 0 {
		st.Quota = cfg.quota
	}

	api := &httpapi.Server{
		Store:          st,
		Secret:         cfg.secret,
		PublicBaseURL:  cfg.publicBase,
		Apple:          &auth.Verifier{Audience: cfg.audience},
		TeamID:         cfg.teamID,
		AllowedOrigins: cfg.origins,
	}
	if cfg.s3.Bucket != "" {
		s3, err := blob.NewS3(cfg.s3)
		if err != nil {
			log.Fatal(err)
		}
		api.S3 = s3
	} else {
		disk, err := blob.NewDisk(cfg.blobDir)
		if err != nil {
			log.Fatal(err)
		}
		api.Disk = disk
	}

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("imgmesh api listening on %s", cfg.addr)
	log.Fatal(srv.ListenAndServe())
}

type config struct {
	databaseURL string
	addr        string
	publicBase  string
	blobDir     string
	secret      []byte
	audience    string
	teamID      string
	quota       int64
	origins     []string
	s3          blob.S3Config
}

func load() (config, error) {
	secret, err := decodeSecret(os.Getenv("BLOB_SECRET"))
	if err != nil {
		return config{}, err
	}
	quota, _ := strconv.ParseInt(os.Getenv("QUOTA_BYTES"), 10, 64)
	origins := []string{"https://imgmesh.com", "https://www.imgmesh.com", "http://localhost:5173"}
	if extra := os.Getenv("ALLOWED_ORIGINS"); extra != "" {
		origins = append(origins, strings.Split(extra, ",")...)
	}
	addr := env("ADDR", "127.0.0.1:8097")
	return config{
		databaseURL: os.Getenv("DATABASE_URL"),
		addr:        addr,
		publicBase:  env("PUBLIC_BASE_URL", "https://imgmesh.com"),
		blobDir:     env("BLOB_DIR", "/var/lib/imgmesh/blobs"),
		secret:      secret,
		audience:    env("APPLE_AUDIENCE", "com.imgmesh.app"),
		teamID:      os.Getenv("APPLE_TEAM_ID"),
		quota:       quota,
		origins:     origins,
		s3: blob.S3Config{
			Bucket:    os.Getenv("S3_BUCKET"),
			Region:    env("S3_REGION", "auto"),
			Endpoint:  os.Getenv("S3_ENDPOINT"),
			AccessKey: os.Getenv("S3_ACCESS_KEY_ID"),
			SecretKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
			PathStyle: os.Getenv("S3_ENDPOINT") != "",
		},
	}, require(os.Getenv("DATABASE_URL"), "DATABASE_URL")
}

func require(value, name string) error {
	if strings.TrimSpace(value) == "" {
		return errMissing(name)
	}
	return nil
}

type errMissing string

func (e errMissing) Error() string { return "missing " + string(e) }

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func decodeSecret(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errMissing("BLOB_SECRET")
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil && len(b) >= 32 {
		return b, nil
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) >= 32 {
		return b, nil
	}
	return nil, errMissing("BLOB_SECRET (want 32+ bytes, base64)")
}
