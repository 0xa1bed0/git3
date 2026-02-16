package git

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestInitRepoFresh(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Dir:    dir,
		Branch: "main",
	}

	repo := InitRepo(cfg)
	if repo == nil {
		t.Fatal("expected non-nil repo")
	}

	// Should have .git directory
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatalf("expected .git directory: %v", err)
	}
}

func TestInitRepoIdempotent(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Dir:    dir,
		Branch: "main",
	}

	repo1 := InitRepo(cfg)
	repo2 := InitRepo(cfg)

	if repo1 == nil || repo2 == nil {
		t.Fatal("expected non-nil repos")
	}
}

func TestInitRepoWithRemoteFallback(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Dir:    dir,
		Repo:   "https://example.com/nonexistent.git",
		Branch: "main",
	}

	// Clone will fail (bad URL), should fall back to init + create remote
	repo := InitRepo(cfg)
	if repo == nil {
		t.Fatal("expected non-nil repo")
	}

	// Check that remote was created
	remotes, err := repo.Remotes()
	if err != nil {
		t.Fatalf("listing remotes failed: %v", err)
	}
	if len(remotes) != 1 {
		t.Fatalf("expected 1 remote, got %d", len(remotes))
	}
	if remotes[0].Config().Name != "origin" {
		t.Fatalf("remote name = %q, want origin", remotes[0].Config().Name)
	}
}

func TestDoSyncCommitsChanges(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Dir:   dir,
		Branch: "main",
		User:  "Test",
		Email: "test@test.com",
	}

	repo := InitRepo(cfg)
	if repo == nil {
		t.Fatal("expected non-nil repo")
	}

	syncer := New(cfg, repo)

	// Create a file
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644)

	// Run sync directly
	syncer.doSync()

	// Check that a commit was created
	head, err := repo.Head()
	if err != nil {
		t.Fatalf("expected HEAD after sync: %v", err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		t.Fatalf("getting commit failed: %v", err)
	}
	if commit.Author.Name != "Test" {
		t.Fatalf("author = %q, want Test", commit.Author.Name)
	}
}

func TestDoSyncNoChanges(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Dir:   dir,
		Branch: "main",
		User:  "Test",
		Email: "test@test.com",
	}

	repo := InitRepo(cfg)
	if repo == nil {
		t.Fatal("expected non-nil repo")
	}

	syncer := New(cfg, repo)

	// Create a file and sync
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644)
	syncer.doSync()

	head1, _ := repo.Head()

	// Sync again without changes
	syncer.doSync()

	head2, _ := repo.Head()
	if head1.Hash() != head2.Hash() {
		t.Fatal("expected no new commit when nothing changed")
	}
}

func TestTriggerDebounce(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{
		Dir:   dir,
		Branch: "main",
		User:  "Test",
		Email: "test@test.com",
	}

	repo := InitRepo(cfg)
	if repo == nil {
		t.Fatal("expected non-nil repo")
	}

	// Track doSync calls using a wrapper
	var syncCount atomic.Int32
	syncer := New(cfg, repo)
	syncer.debounce = 50 * time.Millisecond

	// Create a file so there's something to commit
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644)

	// Override the timer function to count calls
	origSync := syncer.doSync
	countingSync := func() {
		syncCount.Add(1)
		origSync()
	}

	// Trigger multiple times rapidly — only the last should fire
	syncer.mu.Lock()
	for i := 0; i < 5; i++ {
		if syncer.timer != nil {
			syncer.timer.Stop()
		}
		syncer.timer = time.AfterFunc(syncer.debounce, countingSync)
	}
	syncer.mu.Unlock()

	// Wait for debounce to fire
	time.Sleep(200 * time.Millisecond)

	count := syncCount.Load()
	if count != 1 {
		t.Fatalf("doSync called %d times, want 1", count)
	}
}

func TestNewSyncerNilRepo(t *testing.T) {
	cfg := Config{
		Dir:      t.TempDir(),
		Branch:   "main",
		User:     "Test",
		Email:    "test@test.com",
		Debounce: time.Second,
	}

	syncer := New(cfg, nil)

	// Should not panic — doSync handles nil repo
	syncer.doSync()

	// Trigger should also not panic
	syncer.Trigger()
}
