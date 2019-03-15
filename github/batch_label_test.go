package github

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/google/go-github/github"
	"github.com/stretchr/testify/require"
)

type batchGetLabelParams struct {
	owner string
	repo  string
	label string
}

type listCall struct {
	owner string
	repo  string
}

type fakeIssuesService struct {
	sync.Mutex

	calls  []listCall
	labels []string
}

func (f *fakeIssuesService) ListLabels(ctx context.Context, owner string, repo string, opt *github.ListOptions) ([]*github.Label, *github.Response, error) {
	f.Lock()
	defer f.Unlock()

	f.calls = append(f.calls, listCall{
		owner: owner,
		repo:  repo,
	})

	var labels []*github.Label
	for _, l := range f.labels {
		l := l
		labels = append(labels, &github.Label{
			Name: &l,
		})
	}
	return labels, &github.Response{Response: &http.Response{StatusCode: 200}}, nil
}

func TestBatchGetLabel(t *testing.T) {
	ctx := context.Background()

	for i, c := range []struct {
		expectedListCalls []listCall
		params            []batchGetLabelParams
	}{
		{nil, nil},
		{[]listCall{
			{"org1", "repo1"},
		}, []batchGetLabelParams{
			{"org1", "repo1", "label1"},
			{"org1", "repo1", "label2"},
		}},
		{[]listCall{
			{"org1", "repo1"},
			{"org1", "repo2"},
		}, []batchGetLabelParams{
			{"org1", "repo1", "label1"},
			{"org1", "repo2", "label2"},
		}},
		{[]listCall{
			{"org1", "repo1"},
			{"org2", "repo1"},
		}, []batchGetLabelParams{
			{"org1", "repo1", "label1"},
			{"org2", "repo1", "label2"},
		}},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			issues := &fakeIssuesService{
				labels: []string{"label1", "label2"},
			}
			wg := sync.WaitGroup{}
			for _, p := range c.params {
				wg.Add(1)
				p := p
				go func() {
					defer wg.Done()

					l, err := batchGetLabel(ctx, issues, p.owner, p.repo, p.label)
					require.NoError(t, err)
					require.Equal(t, p.label, l.GetName())
				}()
			}
			wg.Wait()

			require.Equal(t, c.expectedListCalls, issues.calls)
		})
	}
}
