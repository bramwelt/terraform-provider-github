package github

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/github"
)

const (
	batchWaitTime = 2 * time.Second
)

type getLabelResult struct {
	label *github.Label
	err   error
}

type getLabelParams struct {
	issues issuesService
	owner  string
	repo   string
	name   string

	result chan getLabelResult
}
type issuesService interface {
	ListLabels(ctx context.Context, owner string, repo string, opt *github.ListOptions) ([]*github.Label, *github.Response, error)
}

var (
	labelBatchChan = make(chan getLabelParams)
	startBatches   sync.Once
)

type labelBatchRepo struct {
	sync.Mutex
	timer *time.Timer

	params []getLabelParams
}

func (r *labelBatchRepo) run() {
	p := r.params[0]

	owner := p.owner
	repo := p.repo
	issues := p.issues

	labels := []*github.Label{}
	opt := github.ListOptions{
		PerPage: 100,
	}

	for {
		page, resp, err := issues.ListLabels(context.Background(), owner, repo, &opt)
		// if you get an error on any request, just return the error for all labels
		if err != nil {
			for _, p := range r.params {
				p.result <- getLabelResult{
					err: err,
				}
			}
			return
		}
		labels = append(labels, page...)

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	for _, p := range r.params {
		found := false
		for _, l := range labels {
			if strings.ToLower(l.GetName()) == strings.ToLower(p.name) {
				p.result <- getLabelResult{
					label: l,
				}

				found = true
				break
			}
		}
		if !found {
			p.result <- getLabelResult{}
		}
	}
}

func processLabelBatch() {
	go func() {
		var reposMu sync.Mutex
		repos := map[string]*labelBatchRepo{}

		for {
			log.Printf("[DEBUG] waiting for label batch")
			n := <-labelBatchChan
			log.Printf("[DEBUG] processing queued label %s/%s %s", n.owner, n.repo, n.name)

			key := fmt.Sprintf("%s/%s", n.owner, n.repo)
			var repo *labelBatchRepo

			func() {
				reposMu.Lock()
				defer reposMu.Unlock()

				repo = repos[key]
				if repo == nil {
					repo = &labelBatchRepo{}
					repos[key] = repo
				}

				repo.Lock()
				defer repo.Unlock()

				repo.params = append(repo.params, n)

				if repo.timer != nil {
					repo.timer.Stop()
				}
				repo.timer = time.AfterFunc(batchWaitTime, func() {
					log.Printf("[DEBUG] label batch timer elapsed, processing batch")

					var repo *labelBatchRepo
					func() {
						reposMu.Lock()
						defer reposMu.Unlock()

						repo = repos[key]
						delete(repos, key)
					}()
					repo.run()
				})
			}()
		}
	}()
}

func batchGetLabel(ctx context.Context, issues issuesService, owner, repo, name string) (*github.Label, error) {
	startBatches.Do(processLabelBatch)

	result := make(chan getLabelResult)

	log.Printf("[DEBUG] queuing label for batch %s/%s %s", owner, repo, name)
	labelBatchChan <- getLabelParams{
		issues: issues,
		owner:  owner,
		repo:   repo,
		name:   name,
		result: result,
	}

	select {
	case r := <-result:
		log.Printf("[DEBUG] label batch result received %s/%s %s", owner, repo, name)
		return r.label, r.err
	case <-ctx.Done():
		err := ctx.Err()
		if err == nil {
			err = fmt.Errorf("context terminated unexpectedly fetching label %s/%s %s", owner, repo, name)
		}
		return nil, err
	}
}
