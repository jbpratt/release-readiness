// Package prow ingests Quay periodic and rehearsal CI runs from the public
// OpenShift CI GCS bucket.
package prow

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"
)

const (
	KindPeriodic  = "periodic"
	KindRehearsal = "rehearsal"

	ArtifactPresent = "present"
	ArtifactMissing = "missing"
	ArtifactInvalid = "invalid"
)

// Run is one Prow job execution and the images its tested-images.json names.
type Run struct {
	JobName       string     `json:"job_name"`
	BuildID       string     `json:"build_id"`
	Kind          string     `json:"kind"`
	Application   string     `json:"application"`
	State         string     `json:"state"`
	StartedAt     *time.Time `json:"started_at"`
	CompletedAt   *time.Time `json:"completed_at"`
	ProwURL       string     `json:"prow_url"`
	ArtifactState string     `json:"artifact_state"`
	CatalogRef    string     `json:"catalog_ref"`
	FetchedAt     time.Time  `json:"fetched_at"`
	Images        []Image    `json:"images"`
}

// Image is a requested image a run tested. Digest is the join key against
// Snapshot components; ImageID is the runtime image and diagnostic only.
type Image struct {
	Role         string `json:"role"`
	Source       string `json:"source"`
	RequestedRef string `json:"requested_ref"`
	Digest       string `json:"digest"`
	ImageID      string `json:"image_id"`
}

// SyncState records when a job's runs were last listed without error.
type SyncState struct {
	JobName            string
	Application        string
	Interval           time.Duration
	LastSuccessfulSync time.Time
}

// Job is a configured GCS run prefix mapped to the Konflux application whose
// images it tests, so every z-stream of a minor shares its runs.
type Job struct {
	Name        string
	Prefix      string
	Application string
	Kind        string
}

// ParseJobs parses "key=application[,key=application...]". For
// periodics the key is a job name under logs/; for rehearsals it is a full
// GCS prefix.
func ParseJobs(spec, kind string) ([]Job, error) {
	var jobs []Job
	for entry := range strings.SplitSeq(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key, app, ok := strings.Cut(entry, "=")
		key, app = strings.Trim(strings.TrimSpace(key), "/"), strings.TrimSpace(app)
		if !ok || key == "" || app == "" {
			return nil, fmt.Errorf("invalid %s entry %q, want key=application", kind, entry)
		}
		prefix := key + "/"
		if kind == KindPeriodic {
			prefix = "logs/" + prefix
		}
		jobs = append(jobs, Job{Name: path.Base(key), Prefix: prefix, Application: app, Kind: kind})
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
		Source       string `json:"source"`
		RequestedRef string `json:"requested_ref"`
		ImageID      string `json:"image_id"`
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
		Kind:          job.Kind,
		Application:   job.Application,
		State:         pj.Status.State,
		StartedAt:     pj.Status.StartTime,
		ProwURL:       pj.Status.URL,
		ArtifactState: ArtifactMissing,
		Images:        []Image{},
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
// bundle are stored as images too so a digest lookup finds them.
func (r *Run) applyArtifact(data []byte) {
	var ti testedImages
	if err := json.Unmarshal(data, &ti); err != nil || ti.SchemaVersion != 1 {
		r.ArtifactState = ArtifactInvalid
		return
	}
	r.ArtifactState = ArtifactPresent
	r.CatalogRef = ti.Catalog.ResolvedRef
	add := func(role, source, ref, imageID string) {
		r.Images = append(r.Images, Image{Role: role, Source: source, RequestedRef: ref, Digest: digest(ref), ImageID: imageID})
	}
	if ti.Catalog.ResolvedRef != "" {
		add("catalog", "catalog", ti.Catalog.ResolvedRef, "")
	}
	if ti.Operator.BundleRef != "" {
		add("quay-operator-bundle", "bundle", ti.Operator.BundleRef, "")
	}
	for _, img := range ti.Images {
		add(img.Role, img.Source, img.RequestedRef, img.ImageID)
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

// componentRoles maps a quay-X-Y component, less its application prefix, to
// the tested-images.json role that deploys it.
var componentRoles = map[string]string{
	"quay-quay":            "quay",
	"quay-clair":           "clair",
	"quay-builder":         "builder",
	"quay-builder-qemu":    "builder-qemu",
	"quay-operator":        "quay-operator",
	"quay-operator-bundle": "quay-operator-bundle",
}

// ComponentKey is the exact (role, digest) identity a run must have tested for
// it to count as testing a Snapshot component's image, or "" when no run can.
// fbc-quay-X-Y-quay-operator is the catalog the run installs from.
func ComponentKey(application, component, image string) string {
	suffix, ok := strings.CutPrefix(component, application+"-")
	d := digest(image)
	if !ok || d == "" {
		return ""
	}
	role := componentRoles[suffix]
	if strings.HasPrefix(application, "fbc-") {
		role = ""
		if suffix == "quay-operator" {
			role = "catalog"
		}
	}
	if role == "" {
		return ""
	}
	return role + "@" + d
}

// Tested reports whether r tested an image with any of keys.
func (r *Run) Tested(keys map[string]bool) bool {
	for _, img := range r.Images {
		if img.Digest != "" && keys[img.Role+"@"+img.Digest] {
			return true
		}
	}
	return false
}
