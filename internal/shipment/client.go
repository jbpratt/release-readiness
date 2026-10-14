// Package shipment tracks the GitLab merge request through which ART ships
// each Quay assembly, read from ART's ocp-shipment-data project.
package shipment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

// Client reads shipment MR pointers from ocp-build-data and the MRs from
// GitLab, with a token when one is set and anonymously otherwise.
type Client struct {
	gitlabURL    string
	buildDataURL string
	project      int
	token        string
	http         *http.Client
}

func NewClient(gitlabURL, buildDataURL string, project int, token string, hc *http.Client) *Client {
	return &Client{
		gitlabURL:    strings.TrimSuffix(gitlabURL, "/"),
		buildDataURL: strings.TrimSuffix(buildDataURL, "/"),
		project:      project,
		token:        token,
		http:         hc,
	}
}

// MR is a GitLab merge request. Changes is set only by changes.
type MR struct {
	IID          int      `json:"iid"`
	WebURL       string   `json:"web_url"`
	State        string   `json:"state"`
	Draft        bool     `json:"draft"`
	Labels       []string `json:"labels"`
	SourceBranch string   `json:"source_branch"`
	HeadPipeline *struct {
		Status string `json:"status"`
	} `json:"head_pipeline"`
	DetailedMergeStatus string     `json:"detailed_merge_status"`
	MergedAt            *time.Time `json:"merged_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	Changes             []Change   `json:"changes"`
}

// Change is a file an MR changes.
type Change struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
}

// pointer returns the iid of the shipment MR that the stream's releases.yml
// names for the assembly, or 0 when the branch, file or key is missing.
func (c *Client) pointer(ctx context.Context, assembly string) (int, error) {
	stream := assembly[:strings.LastIndex(assembly, ".")]
	data, err := c.get(ctx, c.buildDataURL+"/quay-"+stream+"/releases.yml", false)
	if err != nil || data == nil {
		return 0, err
	}
	var doc struct {
		Releases map[string]json.RawMessage `json:"releases"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return 0, fmt.Errorf("releases.yml of quay-%s: %w", stream, err)
	}
	raw, ok := doc.Releases[assembly]
	if !ok {
		return 0, nil
	}
	var rel struct {
		Assembly struct {
			Group struct {
				Shipment struct {
					MR string `json:"mr"`
				} `json:"shipment"`
			} `json:"group"`
		} `json:"assembly"`
	}
	if err := json.Unmarshal(raw, &rel); err != nil {
		return 0, fmt.Errorf("releases.yml of quay-%s, %s: %w", stream, assembly, err)
	}
	mr := rel.Assembly.Group.Shipment.MR
	if mr == "" {
		return 0, nil
	}
	iid, err := strconv.Atoi(path.Base(mr))
	if err != nil {
		return 0, fmt.Errorf("shipment MR %q of %s: %w", mr, assembly, err)
	}
	return iid, nil
}

// mr returns the merge request, or nil if it does not exist.
func (c *Client) mr(ctx context.Context, iid int) (*MR, error) {
	return c.getMR(ctx, fmt.Sprintf("/merge_requests/%d", iid))
}

// changes returns the merge request with the files it changes.
func (c *Client) changes(ctx context.Context, iid int) (*MR, error) {
	return c.getMR(ctx, fmt.Sprintf("/merge_requests/%d/changes", iid))
}

// search returns one page of the project's merge requests that mention the
// assembly, most recently updated first. They carry no head pipeline.
func (c *Client) search(ctx context.Context, assembly string) ([]MR, error) {
	q := url.Values{"search": {assembly}, "state": {"all"}, "order_by": {"updated_at"}, "per_page": {"100"}}
	data, err := c.get(ctx, c.projectURL()+"/merge_requests?"+q.Encode(), true)
	if err != nil {
		return nil, err
	}
	var mrs []MR
	if err := json.Unmarshal(data, &mrs); err != nil {
		return nil, fmt.Errorf("search %s: %w", assembly, err)
	}
	return mrs, nil
}

func (c *Client) getMR(ctx context.Context, p string) (*MR, error) {
	data, err := c.get(ctx, c.projectURL()+p, true)
	if err != nil || data == nil {
		return nil, err
	}
	var mr MR
	if err := json.Unmarshal(data, &mr); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	return &mr, nil
}

func (c *Client) projectURL() string {
	return c.gitlabURL + "/api/v4/projects/" + strconv.Itoa(c.project)
}

// get returns the body, or nil on 404. gitlab sends the token, when set.
func (c *Client) get(ctx context.Context, u string, gitlab bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if gitlab && c.token != "" {
		req.Header.Set("PRIVATE-TOKEN", c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
