package github

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"testing"

	"github.com/google/go-github/github"
	"github.com/stretchr/testify/require"
)

type listCall struct {
	owner string
	repo  string
}

type fakeIssuesService struct {
	calls  []listCall
	labels []string
}

func (f *fakeIssuesService) ListLabels(ctx context.Context, owner string, repo string, opt *github.ListOptions) ([]*github.Label, *github.Response, error) {
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

func TestBulkGetLabel(t *testing.T) {
	// non-blocking channel to take the label result
	noop := func() chan getLabelResult { return make(chan getLabelResult, 1) }

	for i, c := range []struct {
		expectedListCalls []listCall
		params            []getLabelParams
	}{
		{nil, nil},
		{[]listCall{
			{"org1", "repo1"},
		}, []getLabelParams{
			{"org1", "repo1", "label1", noop()},
			{"org1", "repo1", "label2", noop()},
		}},
		{[]listCall{
			{"org1", "repo1"},
			{"org1", "repo2"},
		}, []getLabelParams{
			{"org1", "repo1", "label1", noop()},
			{"org1", "repo2", "label2", noop()},
		}},
		{[]listCall{
			{"org1", "repo1"},
			{"org2", "repo1"},
		}, []getLabelParams{
			{"org1", "repo1", "label1", noop()},
			{"org2", "repo1", "label2", noop()},
		}},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			issues := &fakeIssuesService{
				labels: []string{"label1", "label2"},
			}

			bulkGetLabels(issues, c.params)

			sorter := func(calls []listCall) func(i, j int) bool {
				return func(i, j int) bool {
					return calls[i].owner < calls[j].owner ||
						calls[i].owner == calls[j].owner && calls[i].repo < calls[j].repo
				}
			}

			sort.Slice(c.expectedListCalls, sorter(c.expectedListCalls))
			sort.Slice(issues.calls, sorter(issues.calls))
			require.Equal(t, c.expectedListCalls, issues.calls)
		})
	}
}
