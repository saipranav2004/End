package awsconnect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/codecommit"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/smithy-go"
)

// Exposed-credential checks: what the secret scanner does in a client's
// account, tried once with the same role and External ID.

var (
	// ErrSecretNotFound: no secret with that ARN (or the ARN's region is wrong).
	ErrSecretNotFound = errors.New("secret not found")
	// ErrSecretEmpty: the secret holds no token.
	ErrSecretEmpty = errors.New("secret holds no token")
	// ErrKMSDenied: the secret is encrypted with a KMS key the role may not use.
	ErrKMSDenied = errors.New("kms key denied")
)

// CodeCommitResult is what ListCodeCommit found.
type CodeCommitResult struct {
	Repositories int
	// Regions where CodeCommit answered; Unavailable where it could not be
	// reached (not offered there, or the region is not enabled).
	Regions     []string
	Unavailable []string
	// A repository to test reading, when any exist.
	SampleRepository string
	SampleRegion     string
	// Repos are the repositories found, in region order (at most
	// maxReposPerRegion per region): what the read check samples from.
	Repos []RepoRef
}

// RepoRef names one repository.
type RepoRef struct{ Region, Name string }

const maxReposPerRegion = 50

// codeCommitConcurrency bounds parallel region calls.
const codeCommitConcurrency = 8

// ListCodeCommit lists repositories in each region (codecommit:ListRepositories).
// AccessDenied in any region fails the whole check: the policy is account-wide.
func (c *Client) ListCodeCommit(ctx context.Context, s *Session, regions []string) (CodeCommitResult, error) {
	var res CodeCommitResult
	if s == nil {
		return res, errors.New("no session")
	}
	type outcome struct {
		region string
		count  int
		first  string
		names  []string
		err    error
	}
	outcomes := make([]outcome, len(regions))
	sem := make(chan struct{}, codeCommitConcurrency)
	var wg sync.WaitGroup
	for i, region := range regions {
		wg.Add(1)
		go func(i int, region string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			cfg := s.config.Copy()
			cfg.Region = region
			client := codecommit.NewFromConfig(cfg)
			o := outcome{region: region}
			var token *string
			for page := 0; page < 10; page++ { // at most 10 pages per region
				out, err := client.ListRepositories(ctx, &codecommit.ListRepositoriesInput{NextToken: token})
				if err != nil {
					o.err = classify(err)
					break
				}
				for _, r := range out.Repositories {
					if r.RepositoryName == nil {
						continue
					}
					if o.first == "" {
						o.first = *r.RepositoryName
					}
					if len(o.names) < maxReposPerRegion {
						o.names = append(o.names, *r.RepositoryName)
					}
				}
				o.count += len(out.Repositories)
				if out.NextToken == nil || *out.NextToken == "" {
					break
				}
				token = out.NextToken
			}
			outcomes[i] = o
		}(i, region)
	}
	wg.Wait()
	for _, o := range outcomes {
		switch {
		case o.err == nil:
			res.Regions = append(res.Regions, o.region)
			res.Repositories += o.count
			for _, name := range o.names {
				res.Repos = append(res.Repos, RepoRef{Region: o.region, Name: name})
			}
			if res.SampleRepository == "" && o.first != "" {
				res.SampleRepository, res.SampleRegion = o.first, o.region
			}
		case errors.Is(o.err, ErrAccessDenied):
			return res, fmt.Errorf("%w in %s", o.err, o.region)
		case ctx.Err() != nil:
			return res, ctx.Err()
		default:
			res.Unavailable = append(res.Unavailable, o.region)
		}
	}
	sort.Strings(res.Unavailable)
	if len(res.Regions) == 0 && len(regions) > 0 {
		return res, fmt.Errorf("codecommit could not be reached in any selected region")
	}
	return res, nil
}

// CodeCommitActions are the four permissions the secret scanner reads code with.
var CodeCommitActions = []string{"codecommit:GetBranch", "codecommit:GetCommit", "codecommit:GetDifferences", "codecommit:GetBlob"}

// RepoProbe is what reading one repository the way the scanner does found.
type RepoProbe struct {
	Region, Repository string
	Branch             string
	// Denied lists the permissions AWS refused, in the order the scanner uses them.
	Denied []string
	// Files is how many changed files' contents (blobs) could be fetched.
	Files int
	// Empty: the repository has no commits to read yet.
	Empty bool
	// Err is a failure that is not a missing permission (repository gone, AWS down).
	Err error
}

// OK reports that every permission worked (or there was nothing to read).
func (p RepoProbe) OK() bool { return len(p.Denied) == 0 && p.Err == nil }

// ProbeCodeCommit reads one repository the way the scanner does, with only
// the four permissions it needs: GetBranch (default branch -> commit id),
// GetCommit, GetDifferences (the files the commit changed) and GetBlob (one
// file's contents). A not-found answer still proves the permission: AWS
// checks the permission before it looks for the thing.
func (c *Client) ProbeCodeCommit(ctx context.Context, s *Session, region, repository string) RepoProbe {
	p := RepoProbe{Region: region, Repository: repository}
	if s == nil {
		p.Err = errors.New("no session")
		return p
	}
	cfg := s.config.Copy()
	cfg.Region = region
	client := codecommit.NewFromConfig(cfg)
	denied := func(err error) bool {
		if err != nil && errors.Is(classify(err), ErrAccessDenied) {
			return true
		}
		return false
	}

	// The default branch: GetRepository when the role may, else the usual names.
	branches := []string{"main", "master"}
	if repo, err := client.GetRepository(ctx, &codecommit.GetRepositoryInput{RepositoryName: aws.String(repository)}); err == nil &&
		repo.RepositoryMetadata != nil && repo.RepositoryMetadata.DefaultBranch != nil && *repo.RepositoryMetadata.DefaultBranch != "" {
		branches = []string{*repo.RepositoryMetadata.DefaultBranch}
	}
	var commitID string
	for _, branch := range branches {
		out, err := client.GetBranch(ctx, &codecommit.GetBranchInput{RepositoryName: aws.String(repository), BranchName: aws.String(branch)})
		if denied(err) {
			p.Denied = append(p.Denied, CodeCommitActions[0])
			return p
		}
		if err == nil && out.Branch != nil && out.Branch.CommitId != nil {
			p.Branch, commitID = branch, *out.Branch.CommitId
			break
		}
	}
	if commitID == "" { // no such branch: an empty repository, or an unusual default
		p.Empty = true
		return p
	}

	commit, err := client.GetCommit(ctx, &codecommit.GetCommitInput{RepositoryName: aws.String(repository), CommitId: aws.String(commitID)})
	if denied(err) {
		p.Denied = append(p.Denied, CodeCommitActions[1])
	} else if err != nil {
		p.Err = classify(err)
		return p
	}
	in := &codecommit.GetDifferencesInput{
		RepositoryName:        aws.String(repository),
		AfterCommitSpecifier:  aws.String(commitID),
		MaxResults:            aws.Int32(5),
		BeforeCommitSpecifier: nil,
	}
	if commit != nil && commit.Commit != nil && len(commit.Commit.Parents) > 0 {
		in.BeforeCommitSpecifier = aws.String(commit.Commit.Parents[0])
	}
	diff, err := client.GetDifferences(ctx, in)
	var blobs []string
	switch {
	case denied(err):
		p.Denied = append(p.Denied, CodeCommitActions[2])
	case err != nil:
		p.Err = classify(err)
		return p
	default:
		for _, d := range diff.Differences {
			if d.AfterBlob != nil && d.AfterBlob.BlobId != nil {
				blobs = append(blobs, *d.AfterBlob.BlobId)
			}
		}
	}
	for _, id := range blobs {
		_, err := client.GetBlob(ctx, &codecommit.GetBlobInput{RepositoryName: aws.String(repository), BlobId: aws.String(id)})
		if denied(err) {
			p.Denied = append(p.Denied, CodeCommitActions[3])
			break
		}
		if err == nil {
			p.Files++
		}
	}
	return p
}

var secretARNRE = regexp.MustCompile(`^arn:(aws[a-z-]*):secretsmanager:([a-z]{2}(-[a-z]+)+-\d):(\d{12}):secret:([A-Za-z0-9/_+=.@-]{1,512})$`)

// SecretARN is a parsed Secrets Manager secret ARN.
type SecretARN struct {
	ARN       string
	Region    string
	AccountID string
	Name      string // with the random 6-character suffix AWS appends
}

// ErrInvalidSecretARN carries a message for the person on the setup page.
type ErrInvalidSecretARN struct{ Message string }

func (e *ErrInvalidSecretARN) Error() string { return e.Message }

// ParseSecretARN checks the ARN shape, that it is in the given account, and
// that its name starts with prefix (the read policy is scoped to it).
func ParseSecretARN(raw, accountID, prefix string) (SecretARN, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return SecretARN{}, &ErrInvalidSecretARN{Message: "Paste the ARN of the secret that holds your GitHub token."}
	}
	m := secretARNRE.FindStringSubmatch(raw)
	if m == nil {
		return SecretARN{}, &ErrInvalidSecretARN{Message: "That is not a Secrets Manager secret ARN. It looks like arn:aws:secretsmanager:<region>:<account>:secret:<name>."}
	}
	arn := SecretARN{ARN: raw, Region: m[2], AccountID: m[4], Name: m[5]}
	if arn.AccountID != accountID {
		return SecretARN{}, &ErrInvalidSecretARN{Message: fmt.Sprintf("The secret is in account %s; store it in %s, the account this role belongs to.", arn.AccountID, accountID)}
	}
	if prefix != "" && !strings.HasPrefix(arn.Name, prefix) {
		return SecretARN{}, &ErrInvalidSecretARN{Message: fmt.Sprintf("Name the secret starting with %s - the read permission is limited to that name.", prefix)}
	}
	return arn, nil
}

// ReadSecretToken reads a GitHub token from the secret
// (secretsmanager:GetSecretValue). The secret may hold the token itself or a
// JSON object with a "token" (or "github_token") field.
func (c *Client) ReadSecretToken(ctx context.Context, s *Session, arn SecretARN) (string, error) {
	if s == nil {
		return "", errors.New("no session")
	}
	cfg := s.config.Copy()
	cfg.Region = arn.Region
	out, err := secretsmanager.NewFromConfig(cfg).GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String(arn.ARN)})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "ResourceNotFoundException":
				return "", ErrSecretNotFound
			case "DecryptionFailure", "KMSAccessDeniedException", "KMSInvalidStateException":
				return "", ErrKMSDenied
			}
		}
		return "", classify(err)
	}
	if out.SecretString == nil {
		return "", ErrSecretEmpty
	}
	return extractToken(*out.SecretString)
}

func extractToken(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "{") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(value), &obj); err != nil {
			return "", ErrSecretEmpty
		}
		for _, key := range []string{"token", "github_token", "GITHUB_TOKEN"} {
			if v, ok := obj[key].(string); ok && strings.TrimSpace(v) != "" {
				return strings.TrimSpace(v), nil
			}
		}
		return "", ErrSecretEmpty
	}
	if value == "" {
		return "", ErrSecretEmpty
	}
	return value, nil
}
