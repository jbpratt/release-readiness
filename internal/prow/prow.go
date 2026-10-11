// Package prow ingests Quay periodic CI runs from the public
// OpenShift CI GCS bucket.
package prow

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	ArtifactPresent = "present"
	ArtifactMissing = "missing"
	ArtifactInvalid = "invalid"
)

// Run is one Prow job execution and the images its tested-images.json names.
type Run struct {
	JobName, BuildID, Application, State string
	StartedAt, CompletedAt               *time.Time
	ProwURL, ArtifactState, CatalogRef   string
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
	Catalog       struct {
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

// applyArtifact records tested-images.json on r. The catalog and operator
// bundle are stored as images too so a digest lookup finds them; of the rest,
// only images that ran in a pod count, not those the CSV merely references.
func (r *Run) applyArtifact(data []byte) {
	var ti testedImages
	if err := json.Unmarshal(data, &ti); err != nil || ti.SchemaVersion != 1 {
		r.ArtifactState = ArtifactInvalid
		return
	}
	r.ArtifactState = ArtifactPresent
	r.CatalogRef = ti.Catalog.ResolvedRef
	add := func(role, ref string) {
		r.Images = append(r.Images, Image{Role: role, Digest: digest(ref)})
	}
	if ti.Catalog.ResolvedRef != "" {
		add("catalog", ti.Catalog.ResolvedRef)
	}
	if ti.Operator.BundleRef != "" {
		add("quay-operator-bundle", ti.Operator.BundleRef)
	}
	for _, img := range ti.Images {
		if img.Source == "pod" {
			add(img.Role, img.RequestedRef)
		}
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
