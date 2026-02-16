package git

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

// Syncer handles debounced git commit and push operations.
type Syncer struct {
	dir      string
	repo     *gogit.Repository
	remote   string
	branch   string
	user     string
	email    string
	token    string
	debounce time.Duration
	mu       sync.Mutex
	timer    *time.Timer
}

// Config holds the parameters needed to create a Syncer.
type Config struct {
	Dir          string
	Repo         string
	Branch       string
	User         string
	Email        string
	Token        string
	Debounce     time.Duration
	PullInterval time.Duration
}

// InitRepo ensures the vault directory exists and initializes git if needed.
func InitRepo(cfg Config) *gogit.Repository {
	os.MkdirAll(cfg.Dir, 0755)

	repo, err := initRepo(cfg)
	if err != nil {
		log.Printf("[git] init failed: %v", err)
		return nil
	}
	return repo
}

func initRepo(cfg Config) (*gogit.Repository, error) {
	// Try to open an existing repo
	repo, err := gogit.PlainOpen(cfg.Dir)
	if err == nil {
		log.Println("[git] repo already initialized")
		return repo, nil
	}

	// Try to clone if remote is configured
	if cfg.Repo != "" {
		log.Printf("[git] cloning %s ...", cfg.Repo)
		cloneOpts := &gogit.CloneOptions{
			URL:           cfg.Repo,
			ReferenceName: plumbing.NewBranchReferenceName(cfg.Branch),
			SingleBranch:  true,
		}
		if cfg.Token != "" {
			cloneOpts.Auth = &http.BasicAuth{
				Username: "token",
				Password: cfg.Token,
			}
		}
		repo, err = gogit.PlainClone(cfg.Dir, false, cloneOpts)
		if err == nil {
			log.Println("[git] cloned successfully")
			return repo, nil
		}
		log.Printf("[git] clone failed, initializing fresh: %v", err)
	}

	// Fall back to plain init
	repo, err = gogit.PlainInit(cfg.Dir, false)
	if err != nil {
		return nil, fmt.Errorf("git init: %w", err)
	}

	// Set HEAD to the configured branch so the first commit lands there
	// (PlainInit defaults to "master", which may differ from cfg.Branch)
	ref := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(cfg.Branch))
	if err := repo.Storer.SetReference(ref); err != nil {
		log.Printf("[git] set HEAD to %s failed: %v", cfg.Branch, err)
	}

	// Add remote if configured
	if cfg.Repo != "" {
		_, err = repo.CreateRemote(&config.RemoteConfig{
			Name: "origin",
			URLs: []string{cfg.Repo},
		})
		if err != nil {
			log.Printf("[git] create remote failed: %v", err)
		}
	}

	log.Println("[git] initialized new repo")
	return repo, nil
}

// New creates a Syncer. If repo is nil (no git configured), the syncer
// will still accept Trigger() calls but skip actual sync operations.
func New(cfg Config, repo *gogit.Repository) *Syncer {
	return &Syncer{
		dir:      cfg.Dir,
		repo:     repo,
		remote:   cfg.Repo,
		branch:   cfg.Branch,
		user:     cfg.User,
		email:    cfg.Email,
		token:    cfg.Token,
		debounce: cfg.Debounce,
	}
}

// StartPuller launches a background goroutine that periodically pulls
// from the remote. Does nothing if no remote is configured or interval is 0.
func (gs *Syncer) StartPuller(interval time.Duration) {
	if gs.repo == nil || gs.remote == "" || interval <= 0 {
		return
	}
	log.Printf("[git] starting periodic pull every %s", interval)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			gs.doPull()
		}
	}()
}

func (gs *Syncer) doPull() {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.pullLocked()
}

// pullLocked performs git pull. Caller must hold gs.mu.
func (gs *Syncer) pullLocked() {
	wt, err := gs.repo.Worktree()
	if err != nil {
		log.Printf("[git] pull: worktree failed: %v", err)
		return
	}

	pullOpts := &gogit.PullOptions{
		RemoteName:    "origin",
		ReferenceName: plumbing.NewBranchReferenceName(gs.branch),
		SingleBranch:  true,
	}
	if gs.token != "" {
		pullOpts.Auth = &http.BasicAuth{
			Username: "token",
			Password: gs.token,
		}
	}

	err = wt.Pull(pullOpts)
	switch err {
	case nil:
		log.Println("[git] pulled new changes")
	case gogit.NoErrAlreadyUpToDate:
		// nothing to do
	default:
		log.Printf("[git] pull failed: %v", err)
	}
}

func (gs *Syncer) Trigger() {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	if gs.timer != nil {
		gs.timer.Stop()
	}
	gs.timer = time.AfterFunc(gs.debounce, gs.doSync)
}

func (gs *Syncer) doSync() {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	log.Println("[git] syncing...")

	if gs.repo == nil {
		log.Println("[git] no repo configured, skipping sync")
		return
	}

	wt, err := gs.repo.Worktree()
	if err != nil {
		log.Printf("[git] worktree failed: %v", err)
		return
	}

	if err := wt.AddGlob("."); err != nil {
		log.Printf("[git] add failed: %v", err)
		return
	}

	status, err := wt.Status()
	if err != nil {
		log.Printf("[git] status failed: %v", err)
		return
	}

	if status.IsClean() {
		log.Println("[git] no changes")
		return
	}

	msg := fmt.Sprintf("sync: %s", time.Now().Format("2006-01-02 15:04"))
	_, err = wt.Commit(msg, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  gs.user,
			Email: gs.email,
			When:  time.Now(),
		},
	})
	if err != nil {
		log.Printf("[git] commit failed: %v", err)
		return
	}

	if gs.remote != "" {
		gs.pullLocked()

		pushOpts := &gogit.PushOptions{}
		if gs.token != "" {
			pushOpts.Auth = &http.BasicAuth{
				Username: "token",
				Password: gs.token,
			}
		}
		if err := gs.repo.Push(pushOpts); err != nil {
			log.Printf("[git] push failed: %v", err)
			return
		}
		log.Println("[git] pushed")
	}
}
