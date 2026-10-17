// Package release decides which release versions are shipped and which are
// still in play, and assembles each version's release candidate and verdict,
// for the server and the sync loops alike.
package release

import (
	"context"
	"regexp"
	"strings"

	"github.com/quay/release-readiness/internal/catalog"
	"github.com/quay/release-readiness/internal/db"
)

// quayVersion is a concrete version, named after its ART assembly:
// quay-v3.18.1 is assembly 3.18.1.
var quayVersion = regexp.MustCompile(`^quay-v(\d+\.\d+\.\d+)$`)

type Selector struct {
	db      *db.DB
	shipped *catalog.Shipped
	// ArtBaseURL is the ART build history the build pages link to.
	ArtBaseURL string
	// StageReleasePlanPattern matches the ReleasePlan names whose Releases
	// are image STAGE; nil selects no staged build.
	StageReleasePlanPattern *regexp.Regexp
}

// NewSelector builds the selector. A nil shipped leaves JIRA's released flag
// as the only shipped signal.
func NewSelector(database *db.DB, shipped *catalog.Shipped) *Selector {
	return &Selector{db: database, shipped: shipped}
}

// CatalogShipped reports whether the catalog publishes the fixVersion, e.g.
// quay-v3.17.5 as quay 3.17.5.
func (s *Selector) CatalogShipped(name string) bool {
	product, version, _ := strings.Cut(name, "-v")
	return s.shipped.Has(product, version)
}

// ShipmentVersions returns the quay-vX.Y.Z versions whose shipment MRs are
// polled: those the overview shows as not shipped, and those whose stored MR
// is still open, since a version can ship before ART merges its MR.
func (s *Selector) ShipmentVersions(ctx context.Context) ([]string, error) {
	releases, err := s.db.ListAllReleaseVersions(ctx)
	if err != nil {
		return nil, err
	}
	mrs, err := s.db.ShipmentMRs(ctx)
	if err != nil {
		return nil, err
	}
	var versions []string
	for _, rel := range releases {
		open := mrs[rel.Name] != nil && mrs[rel.Name].State == "opened"
		if (open || (!rel.Released && !s.CatalogShipped(rel.Name))) && quayVersion.MatchString(rel.Name) {
			versions = append(versions, rel.Name)
		}
	}
	return versions, nil
}
