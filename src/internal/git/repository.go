// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/zarf-dev/zarf/src/pkg/logger"
	"github.com/zarf-dev/zarf/src/pkg/message"
	"github.com/zarf-dev/zarf/src/pkg/transform"
	"github.com/zarf-dev/zarf/src/pkg/utils"
)

// Open opens an existing local repository at the given path.
func Open(rootPath, address string) (*Repository, error) {
	repoFolder, err := transform.GitURLtoFolderName(address)
	if err != nil {
		return nil, fmt.Errorf("unable to parse git url %s: %w", address, err)
	}
	repoPath := filepath.Join(rootPath, repoFolder)

	// Check that this package is using the new repo format (if not fallback to the format from <= 0.24.x)
	_, err = os.Stat(repoPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if os.IsNotExist(err) {
		repoFolder, err = transform.GitURLtoRepoName(address)
		if err != nil {
			return nil, fmt.Errorf("unable to parse git url %s: %w", address, err)
		}
		repoPath = filepath.Join(rootPath, repoFolder)
	}

	return &Repository{
		path: repoPath,
	}, nil
}

// Clone clones a git repository to the given local path.
func Clone(ctx context.Context, rootPath, address string, shallow bool) (*Repository, error) {
	l := logger.From(ctx)
	// Split the remote url and the zarf reference
	gitURLNoRef, refPlain, err := transform.GitURLSplitRef(address)
	if err != nil {
		return nil, err
	}

	// Parse the ref from the git URL.
	var ref plumbing.ReferenceName
	if refPlain != emptyRef {
		ref = ParseRef(refPlain)
	}

	// Construct a path unique to this git repo
	repoFolder, err := transform.GitURLtoFolderName(address)
	if err != nil {
		return nil, err
	}

	r := &Repository{
		path: filepath.Join(rootPath, repoFolder),
	}

	// Clone the repository
	cloneOpts := &git.CloneOptions{
		URL:        gitURLNoRef,
		RemoteName: onlineRemoteName,
	}
	if ref.IsTag() || ref.IsBranch() {
		cloneOpts.Tags = git.NoTags
		cloneOpts.ReferenceName = ref
		cloneOpts.SingleBranch = true
	}
	if shallow {
		cloneOpts.Depth = 1
	}
	gitCred, err := utils.FindAuthForHost(gitURLNoRef)
	if err != nil {
		return nil, err
	}
	if gitCred != nil {
		cloneOpts.Auth = &gitCred.Auth
	}
	repo, err := git.PlainCloneContext(ctx, r.path, false, cloneOpts)
	if err != nil {
		message.Notef("Falling back to host 'git', failed to clone the repo %q with Zarf: %s", gitURLNoRef, err.Error())
		l.Info("falling back to host 'git', failed to clone the repo with Zarf", "url", gitURLNoRef, "error", err)
		err := r.gitCloneFallback(ctx, gitURLNoRef, ref, shallow)
		if err != nil {
			return nil, err
		}
		return r, nil
	}

	// If we're cloning the whole repo, we need to also fetch the other branches besides the default.
	if ref == emptyRef {
		fetchOpts := &git.FetchOptions{
			RemoteName: onlineRemoteName,
			RefSpecs:   []config.RefSpec{"refs/*:refs/*"},
			Tags:       git.AllTags,
		}
		if gitCred != nil {
			fetchOpts.Auth = &gitCred.Auth
		}
		if err := repo.FetchContext(ctx, fetchOpts); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return nil, err
		}
	}

	// Optionally checkout ref
	if ref != emptyRef && !ref.IsBranch() {
		// Remove the "refs/tags/" prefix from the ref.
		stripped := strings.TrimPrefix(refPlain, "refs/tags/")
		// Use the plain ref as part of the branch name so it is unique and doesn't conflict with other refs.
		alias := fmt.Sprintf("zarf-ref-%s", stripped)
		trunkBranchName := plumbing.NewBranchReferenceName(alias)
		// Checkout the ref as a branch.
		err := r.checkoutRefAsBranch(stripped, trunkBranchName)
		if err != nil {
			return nil, err
		}
	}

	return r, nil
}

// Repository manages a local git repository.
type Repository struct {
	path string
}

// Path returns the local path the repository is stored at.
func (r *Repository) Path() string {
	return r.path
}

// Push pushes the repository to the remote git server.
func (r *Repository) Push(ctx context.Context, address, username, password string) error {
	l := logger.From(ctx)
	repo, err := git.PlainOpen(r.path)
	if err != nil {
		return fmt.Errorf("not a valid git repo or unable to open: %w", err)
	}

	// Configure new remote
	remote, err := repo.Remote(onlineRemoteName)
	if err != nil {
		return fmt.Errorf("unable to find the git remote: %w", err)
	}
	if len(remote.Config().URLs) == 0 {
		return fmt.Errorf("repository has zero remotes configured")
	}
	targetURL, err := transform.GitURL(address, remote.Config().URLs[0], username)
	if err != nil {
		return fmt.Errorf("unable to transform the git url: %w", err)
	}
	// Remove any preexisting offlineRemotes (happens when a retry is triggered)
	err = repo.DeleteRemote(offlineRemoteName)
	if err != nil && !errors.Is(err, git.ErrRemoteNotFound) {
		return err
	}
	_, err = repo.CreateRemote(&config.RemoteConfig{
		Name: offlineRemoteName,
		URLs: []string{targetURL.String()},
	})
	if err != nil {
		return fmt.Errorf("failed to create offline remote: %w", err)
	}

	// Push to new remote
	gitCred := http.BasicAuth{
		Username: username,
		Password: password,
	}

	// Fetch remote offline refs in case of old update or if multiple refs are specified in one package
	// Attempt the fetch, if it fails, log a warning and continue trying to push (might as well try..)
	fetchOptions := &git.FetchOptions{
		RemoteName: offlineRemoteName,
		Auth:       &gitCred,
		RefSpecs: []config.RefSpec{
			"refs/heads/*:refs/heads/*",
			"refs/tags/*:refs/tags/*",
		},
	}
	err = repo.FetchContext(ctx, fetchOptions)
	if errors.Is(err, transport.ErrRepositoryNotFound) {
		message.Debugf("Repo not yet available offline, skipping fetch...")
		l.Debug("repo not yet available offline, skipping fetch")
	} else if errors.Is(err, git.ErrForceNeeded) {
		message.Debugf("Repo fetch requires force, skipping fetch...")
		l.Debug("repo fetch requires force, skipping fetch")
	} else if errors.Is(err, git.NoErrAlreadyUpToDate) {
		message.Debugf("Repo already up-to-date, skipping fetch...")
		l.Debug("repo already up-to-date, skipping fetch")
	} else if err != nil {
		return fmt.Errorf("unable to fetch the git repo prior to push: %w", err)
	}

	// Push all heads and tags to the offline remote
	err = repo.PushContext(ctx, &git.PushOptions{
		RemoteName: offlineRemoteName,
		Auth:       &gitCred,
		// TODO: (@JEFFMCCOY) add the parsing for the `+` force prefix (see https://github.com/zarf-dev/zarf/issues/1410)
		//Force: isForce,
		// If a provided refspec doesn't push anything, it is just ignored
		RefSpecs: []config.RefSpec{
			"refs/heads/*:refs/heads/*",
			"refs/tags/*:refs/tags/*",
		},
	})
	if errors.Is(err, git.NoErrAlreadyUpToDate) {
		message.Debug("Repo already up-to-date")
		l.Debug("repo already up-to-date")
	} else if errors.Is(err, plumbing.ErrObjectNotFound) {
		return fmt.Errorf("unable to push repo due to likely shallow clone: %s", err.Error())
	} else if err != nil {
		return fmt.Errorf("unable to push repo to the gitops service: %s", err.Error())
	}

	return nil
}
func (r *Repository) checkoutRefAsBranch(ref string, branch plumbing.ReferenceName) error {
	repo, err := git.PlainOpen(r.path)
	if err != nil {
		return fmt.Errorf("not a valid git repo or unable to open: %w", err)
	}

	var hash plumbing.Hash
	if plumbing.IsHash(ref) {
		hash = plumbing.NewHash(ref)
	} else {
		tagRef, err := repo.Tag(ref)
		if err != nil {
			return fmt.Errorf("failed to locate tag %s in repository: %w", ref, err)
		}
		hash = tagRef.Hash()
	}

	objRef, err := repo.Object(plumbing.AnyObject, hash)
	if err != nil {
		return fmt.Errorf("an error occurred when getting the repo's object reference: %w", err)
	}

	var commitHash plumbing.Hash
	switch objRef := objRef.(type) {
	case *object.Tag:
		commitHash = objRef.Target
	case *object.Commit:
		commitHash = objRef.Hash
	default:
		return fmt.Errorf("hash type %s not supported", objRef.Type().String())
	}

	checkoutOpts := &git.CheckoutOptions{
		Hash:   commitHash,
		Branch: branch,
		Create: true,
		Force:  true,
	}
	tree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("unable to load the git repo: %w", err)
	}
	return tree.Checkout(checkoutOpts)
}
