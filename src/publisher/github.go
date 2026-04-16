// Package publisher handles publishing built APG packages to GitHub.
// Authentication uses GitHub App JWT → installation token (no PAT required).
package publisher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"

	"github.com/NurOS-Linux/apger/src/credentials"
)

// Publisher publishes packages to a GitHub organisation.
// Supports both GitHub App (JWT) and PAT authentication via Credentials.AuthToken.
type Publisher struct {
	creds credentials.Credentials
	org   string
}

// New creates a Publisher from stored credentials.
func New(creds credentials.Credentials, org string) *Publisher {
	return &Publisher{creds: creds, org: org}
}

// client returns a GitHub client with a fresh token (App or PAT).
func (p *Publisher) client(ctx context.Context) (*github.Client, error) {
	token, err := p.creds.AuthToken(ctx, p.org)
	if err != nil {
		return nil, fmt.Errorf("get auth token: %w", err)
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	return github.NewClient(oauth2.NewClient(ctx, ts)), nil
}

// EnsureRepo creates the repository for pkgName in the org if it doesn't exist.
func (p *Publisher) EnsureRepo(ctx context.Context, pkgName string) error {
	c, err := p.client(ctx)
	if err != nil {
		return err
	}
	_, resp, err := c.Repositories.Get(ctx, p.org, pkgName)
	if err == nil {
		return nil
	}
	if resp == nil || resp.StatusCode != 404 {
		return fmt.Errorf("check repo %s/%s: %w", p.org, pkgName, err)
	}
	_, _, err = c.Repositories.CreateInOrg(ctx, p.org, &github.Repository{
		Name:        github.Ptr(pkgName),
		Description: github.Ptr("NurOS APGv2 package: " + pkgName),
		Private:     github.Ptr(false),
		AutoInit:    github.Ptr(true),
	})
	return err
}

// UploadRelease creates or updates a GitHub Release and uploads assets.
func (p *Publisher) UploadRelease(ctx context.Context, pkgName, version string, assetPaths []string) error {
	c, err := p.client(ctx)
	if err != nil {
		return err
	}
	tag := "v" + strings.TrimPrefix(version, "v")
	rel, err := findOrCreateRelease(ctx, c, p.org, pkgName, tag)
	if err != nil {
		return err
	}
	for _, path := range assetPaths {
		if err := uploadAsset(ctx, c, p.org, pkgName, rel, path); err != nil {
			return fmt.Errorf("upload %s: %w", filepath.Base(path), err)
		}
	}
	return nil
}

// UploadRevocationCert pushes a PGP revocation cert to <org>/.pgp-revocations/.
func (p *Publisher) UploadRevocationCert(ctx context.Context, pkgName string, cert []byte) error {
	c, err := p.client(ctx)
	if err != nil {
		return err
	}
	repo := ".pgp-revocations"
	path := pkgName + "-revocation.asc"
	existing, _, _ := c.Repositories.GetContents(ctx, p.org, repo, path, nil)
	var sha *string
	if existing != nil {
		sha = existing.SHA
	}
	_, _, err = c.Repositories.CreateFile(ctx, p.org, repo, path, &github.RepositoryContentFileOptions{
		Message: github.Ptr("revoke: " + pkgName + " PGP key"),
		Content: cert,
		SHA:     sha,
	})
	return err
}

// TriggerWorkflow dispatches build-packages.yml using the best available auth method.
func TriggerWorkflow(ctx context.Context, creds credentials.Credentials, repoOwner, repoName string, packages []string) error {
	token, err := creds.AuthToken(ctx, repoOwner)
	if err != nil {
		return fmt.Errorf("get auth token: %w", err)
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	c := github.NewClient(oauth2.NewClient(ctx, ts))

	_, err = c.Actions.CreateWorkflowDispatchEventByFileName(
		ctx, repoOwner, repoName, "build-packages.yml",
		github.CreateWorkflowDispatchEventRequest{
			Ref: "main",
			Inputs: map[string]interface{}{
				"packages": strings.Join(packages, ","),
			},
		},
	)
	return err
}

func findOrCreateRelease(ctx context.Context, c *github.Client, org, repo, tag string) (*github.RepositoryRelease, error) {
	rel, resp, err := c.Repositories.GetReleaseByTag(ctx, org, repo, tag)
	if err == nil {
		return rel, nil
	}
	if resp == nil || resp.StatusCode != 404 {
		return nil, fmt.Errorf("get release %s: %w", tag, err)
	}
	rel, _, err = c.Repositories.CreateRelease(ctx, org, repo, &github.RepositoryRelease{
		TagName: github.Ptr(tag),
		Name:    github.Ptr(repo + " " + tag),
		Body:    github.Ptr("Built by APGer — NurOS package builder"),
	})
	return rel, err
}

func uploadAsset(ctx context.Context, c *github.Client, org, repo string, rel *github.RepositoryRelease, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	name := filepath.Base(path)
	assets, _, _ := c.Repositories.ListReleaseAssets(ctx, org, repo, rel.GetID(), nil)
	for _, a := range assets {
		if a.GetName() == name {
			c.Repositories.DeleteReleaseAsset(ctx, org, repo, a.GetID()) //nolint:errcheck
			break
		}
	}
	_, _, err = c.Repositories.UploadReleaseAsset(ctx, org, repo, rel.GetID(),
		&github.UploadOptions{Name: name}, f)
	return err
}
