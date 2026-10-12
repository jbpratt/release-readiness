// Package prow ingests Quay periodic CI runs from the public
// OpenShift CI GCS bucket.
package prow

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	ArtifactPresent = "present"
	// ArtifactMissing is a run not read yet; ArtifactNone one that published
	// neither tested-images.json nor olm-csvs.yaml.
	ArtifactMissing = "missing"
	ArtifactInvalid = "invalid"
	ArtifactOLMCSVs = "olm_csvs"
	ArtifactNone    = "none"
)

// Run is one Prow job execution and the images its tested-images.json, or
// else its olm-csvs.yaml, names. InstallFailed is a run whose Quay install
// step failed after quay-gather saw its quay-operator Subscription.
type Run struct {
	JobName, BuildID, Application, State string
	StartedAt, CompletedAt               *time.Time
	ProwURL, ArtifactState, CatalogRef   string
	InstallFailed                        bool
	FetchedAt                            time.Time
	Images                               []Image
}

// Image is a requested image a run tested. Digest is the join key against
// Snapshot components.
type Image struct {
	Role, Digest string
}

// Job is a configured GCS run prefix mapped to the Konflux application whose
// images it tests, so every z-stream of a minor shares its runs.
type Job struct {
	Name        string
	Prefix      string
	Application string
}

// ParseJobs parses "job_name=application[,job_name=application...]".
func ParseJobs(spec string) ([]Job, error) {
	var jobs []Job
	for entry := range strings.SplitSeq(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key, app, ok := strings.Cut(entry, "=")
		key, app = strings.TrimSpace(key), strings.TrimSpace(app)
		if !ok || key == "" || app == "" {
			return nil, fmt.Errorf("invalid entry %q, want job_name=application", entry)
		}
		jobs = append(jobs, Job{Name: key, Prefix: "logs/" + key + "/", Application: app})
	}
	return jobs, nil
}

type prowJob struct {
	Spec struct {
		PodSpec struct {
			Containers []struct {
				Args []string `json:"args"`
			} `json:"containers"`
		} `json:"pod_spec"`
	} `json:"spec"`
	Status struct {
		State     string     `json:"state"`
		StartTime *time.Time `json:"startTime"`
		URL       string     `json:"url"`
	} `json:"status"`
}

type finished struct {
	Timestamp int64  `json:"timestamp"`
	Result    string `json:"result"`
}

// testedImages is tested-images.json contract v1 (openshift/release
// quay-gather). Unknown fields are ignored.
type testedImages struct {
	SchemaVersion int `json:"schema_version"`
	// Missing lists "subscription" when the cluster has no quay-operator
	// Subscription.
	Missing []string `json:"missing"`
	Catalog struct {
		ResolvedRef string `json:"resolved_ref"`
	} `json:"catalog"`
	Operator struct {
		BundleRef string `json:"bundle_ref"`
	} `json:"operator"`
	Images []struct {
		Role         string `json:"role"`
		RequestedRef string `json:"requested_ref"`
		Source       string `json:"source"`
	} `json:"images"`
}

func (pj *prowJob) target() string {
	for _, c := range pj.Spec.PodSpec.Containers {
		for _, a := range c.Args {
			if t, ok := strings.CutPrefix(a, "--target="); ok {
				return t
			}
		}
	}
	return ""
}

// parseRun builds a run from prowjob.json and, once the run is over,
// finished.json, which is authoritative for the terminal state. It also
// returns the ci-operator target, the run's artifacts/ directory.
func parseRun(job Job, buildID string, pjData, finData []byte) (*Run, string, error) {
	var pj prowJob
	if err := json.Unmarshal(pjData, &pj); err != nil {
		return nil, "", fmt.Errorf("parse prowjob.json: %w", err)
	}
	r := &Run{
		JobName:       job.Name,
		BuildID:       buildID,
		Application:   job.Application,
		State:         pj.Status.State,
		StartedAt:     pj.Status.StartTime,
		ProwURL:       pj.Status.URL,
		ArtifactState: ArtifactMissing,
	}
	if finData != nil {
		var fin finished
		if err := json.Unmarshal(finData, &fin); err != nil {
			return nil, "", fmt.Errorf("parse finished.json: %w", err)
		}
		if fin.Result != "" {
			r.State = strings.ToLower(fin.Result)
		}
		t := time.Unix(fin.Timestamp, 0).UTC()
		r.CompletedAt = &t
	}
	return r, pj.target(), nil
}

// applyArtifact records tested-images.json on r and reports whether it saw a
// quay-operator Subscription. The operator bundle is stored as an image too;
// of the rest, only images that ran in a pod count, not those the CSV merely
// references.
func (r *Run) applyArtifact(data []byte) (subscribed bool) {
	var ti testedImages
	if err := json.Unmarshal(data, &ti); err != nil || ti.SchemaVersion != 1 {
		r.ArtifactState = ArtifactInvalid
		return false
	}
	r.ArtifactState = ArtifactPresent
	r.CatalogRef = ti.Catalog.ResolvedRef
	add := func(role, ref string) {
		r.Images = append(r.Images, Image{Role: role, Digest: digest(ref)})
	}
	if ti.Operator.BundleRef != "" {
		add("quay-operator-bundle", ti.Operator.BundleRef)
	}
	for _, img := range ti.Images {
		if img.Source == "pod" {
			add(img.Role, img.RequestedRef)
		}
	}
	return !slices.Contains(ti.Missing, "subscription")
}

// olmRoles is the role of the image of each repository a quay-operator CSV
// deploys. The operator's was quay-operator-rhel9 until about 2026-10-01.
var olmRoles = map[string]string{
	"quay-rhel9-operator":           "quay-operator",
	"quay-operator-rhel9":           "quay-operator",
	"quay-rhel9":                    "quay",
	"clair-rhel9":                   "clair",
	"quay-builder-rhel9":            "quay-builder",
	"quay-builder-qemu-rhcos-rhel8": "quay-builder-qemu",
}

var (
	quayCSV  = regexp.MustCompile(`(?m)^    name: quay-operator\.v`)
	olmImage = regexp.MustCompile(`/([\w.-]+)@(sha256:[0-9a-f]{64})`)
)

// applyOLMCSVs records on r the Quay images of the quay-operator CSV in
// olm-csvs.yaml, the CSVs installed on the test cluster, which runs published
// before tested-images.json. A role is recorded only when the CSV names one
// digest for it.
func (r *Run) applyOLMCSVs(data []byte) {
	r.ArtifactState = ArtifactOLMCSVs
	// The List's items start at "- " in column 0; no YAML module is needed.
	for item := range strings.SplitSeq(string(data), "\n- ") {
		if !quayCSV.MatchString(item) {
			continue
		}
		var roles []string
		digests := map[string][]string{}
		for _, m := range olmImage.FindAllStringSubmatch(item, -1) {
			role, ok := olmRoles[m[1]]
			if !ok || slices.Contains(digests[role], m[2]) {
				continue
			}
			if digests[role] == nil {
				roles = append(roles, role)
			}
			digests[role] = append(digests[role], m[2])
		}
		for _, role := range roles {
			if len(digests[role]) == 1 {
				r.Images = append(r.Images, Image{Role: role, Digest: digests[role][0]})
			}
		}
		return
	}
}

// digest returns the sha256 digest of a repo@sha256:... reference, or "" for
// a tag reference.
func digest(ref string) string {
	_, d, ok := strings.Cut(ref, "@")
	if !ok || !strings.HasPrefix(d, "sha256:") {
		return ""
	}
	return d
}
