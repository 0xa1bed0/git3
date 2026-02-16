package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"obsidian-sync/internal/git"
	"obsidian-sync/internal/s3"
)

type Config struct {
	Dir       string
	Bucket    string
	Addr      string
	AccessKey string
	SecretKey string
	Region    string
	GitRepo   string
	GitBranch string
	GitUser   string
	GitEmail  string
	GitToken  string
	Debounce  time.Duration
}

func main() {
	var cfg Config

	flag.StringVar(&cfg.Dir, "dir", envOr("VAULT_DIR", "/vault"), "vault directory")
	flag.StringVar(&cfg.Bucket, "bucket", envOr("BUCKET", "vault"), "S3 bucket name")
	flag.StringVar(&cfg.Addr, "addr", envOr("ADDR", ":80"), "listen address")
	flag.StringVar(&cfg.AccessKey, "access-key", envOr("ACCESS_KEY", ""), "S3 access key")
	flag.StringVar(&cfg.SecretKey, "secret-key", envOr("SECRET_KEY", ""), "S3 secret key")
	flag.StringVar(&cfg.Region, "region", envOr("REGION", "us-east-1"), "S3 region")
	flag.StringVar(&cfg.GitRepo, "git-repo", envOr("GIT_REPO", ""), "git remote URL")
	flag.StringVar(&cfg.GitBranch, "git-branch", envOr("GIT_BRANCH", "main"), "git branch")
	flag.StringVar(&cfg.GitUser, "git-user", envOr("GIT_USER", "Obsidian Sync"), "git commit user")
	flag.StringVar(&cfg.GitEmail, "git-email", envOr("GIT_EMAIL", "obsidian@sync"), "git commit email")
	flag.StringVar(&cfg.GitToken, "git-token", envOr("GIT_TOKEN", ""), "git PAT for HTTPS auth")
	debounce := flag.Int("debounce", envOrInt("DEBOUNCE", 10), "git sync debounce in seconds")
	pullInterval := flag.Int("pull-interval", envOrInt("PULL_INTERVAL", 60), "git pull interval in seconds (0 to disable)")
	flag.Parse()

	cfg.Debounce = time.Duration(*debounce) * time.Second

	gitCfg := git.Config{
		Dir:      cfg.Dir,
		Repo:     cfg.GitRepo,
		Branch:   cfg.GitBranch,
		User:     cfg.GitUser,
		Email:    cfg.GitEmail,
		Token:    cfg.GitToken,
		Debounce: cfg.Debounce,
	}

	pullDuration := time.Duration(*pullInterval) * time.Second

	repo := git.InitRepo(gitCfg)
	syncer := git.New(gitCfg, repo)
	syncer.StartPuller(pullDuration)
	handler := s3.NewHandler(cfg.Dir, cfg.Bucket, cfg.AccessKey, cfg.SecretKey, cfg.Region, syncer)

	log.Printf("[obsidian-sync] listening on %s", cfg.Addr)
	log.Printf("[obsidian-sync] bucket=%s dir=%s region=%s", cfg.Bucket, cfg.Dir, cfg.Region)
	if cfg.GitRepo != "" {
		log.Printf("[obsidian-sync] git=%s branch=%s debounce=%s pull=%s", cfg.GitRepo, cfg.GitBranch, cfg.Debounce, pullDuration)
	}

	if err := http.ListenAndServe(cfg.Addr, handler); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
