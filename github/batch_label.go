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

type getLabelResult struct {
	label *github.Label
	err   error
}

type getLabelParams struct {
	owner string
	repo  string
	name  string

	result chan getLabelResult
}
type issuesService interface {
	ListLabels(ctx context.Context, owner string, repo string, opt *github.ListOptions) ([]*github.Label, *github.Response, error)
}

type labelBatcher struct {
	// issues and after are configurable
	issues issuesService
	after  time.Duration

	// startOnce is used to ensure the start method is only invoked once
	startOnce sync.Once

	// the mutex covers only ch, timer, and params
	sync.Mutex
	ch     chan getLabelParams
	timer  *time.Timer
	params []getLabelParams
}

func (b *labelBatcher) start() {
	b.startOnce.Do(func() {
		func() {
			b.Lock()
			defer b.Unlock()

			b.ch = make(chan getLabelParams)
		}()

		go func() {
			for {
				log.Printf("[DEBUG] waiting for label batch")
				n := <-b.ch
				log.Printf("[DEBUG] processing queued label %s/%s %s", n.owner, n.repo, n.name)

				func() {
					b.Lock()
					defer b.Unlock()

					b.params = append(b.params, n)

					if b.timer != nil {
						b.timer.Stop()
					}
					b.timer = time.AfterFunc(b.after, func() {
						var params []getLabelParams
						func() {
							b.Lock()
							defer b.Unlock()

							log.Printf("[DEBUG] label batch timer elapsed, processing batch")

							params = b.params
							b.params = nil
						}()

						bulkGetLabels(b.issues, params)
					})
				}()
			}
		}()
	})
}

func (b *labelBatcher) GetLabel(ctx context.Context, owner, repo, name string) (*github.Label, error) {
	b.start()

	result := make(chan getLabelResult)

	log.Printf("[DEBUG] queuing label for batch %s/%s %s", owner, repo, name)
	b.ch <- getLabelParams{
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

func bulkGetLabels(issues issuesService, batch []getLabelParams) {
	type key struct {
		owner string
		repo  string
	}

	repos := map[key][]getLabelParams{}

	for _, p := range batch {
		k := key{p.owner, p.repo}
		repo := repos[k]
		repo = append(repo, p)
		repos[k] = repo
	}

	for k, params := range repos {
		labels := []*github.Label{}
		opt := github.ListOptions{
			PerPage: 100,
		}

		for {
			page, resp, err := issues.ListLabels(context.Background(), k.owner, k.repo, &opt)
			// if you get an error on any request, just return the error for all labels
			if err != nil {
				for _, p := range params {
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

		for _, p := range params {
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
}
