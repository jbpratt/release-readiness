package jira

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/quay/release-readiness/internal/model"
	"github.com/quay/release-readiness/internal/syncstatus"
)

// Store is the subset of the database layer needed by the JIRA syncer.
type Store interface {
	UpsertReleaseVersion(ctx context.Context, v *model.ReleaseVersion) error
	UpsertJiraIssue(ctx context.Context, issue *model.JiraIssueRecord) error
	DeleteJiraIssuesNotIn(ctx context.Context, fixVersion string, keys []string) error
	ListAllReleaseVersions(ctx context.Context) ([]model.ReleaseVersion, error)
}

// TxFunc wraps a function in a database transaction, passing a tx-scoped Store.
type TxFunc func(ctx context.Context, fn func(Store) error) error

// Syncer orchestrates periodic JIRA synchronisation into a Store.
type Syncer struct {
	client *Client
	store  Store
	withTx TxFunc
	logger *slog.Logger
	// Status receives each pass's outcome; nil reports nowhere.
	Status *syncstatus.Source
}

// NewSyncer creates a Syncer that uses client to fetch data and store to persist it.
func NewSyncer(client *Client, store Store, withTx TxFunc, logger *slog.Logger) *Syncer {
	return &Syncer{client: client, store: store, withTx: withTx, logger: logger}
}

// Run performs an immediate sync and then repeats every interval until ctx is cancelled.
func (s *Syncer) Run(ctx context.Context, interval time.Duration) {
	s.SyncOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("stopping")
			return
		case <-ticker.C:
			s.SyncOnce(ctx)
		}
	}
}

// SyncOnce discovers active releases and syncs their issues.
func (s *Syncer) SyncOnce(ctx context.Context) {
	var pass syncstatus.Pass
	defer func() { s.Status.Report(authError(pass.Err())) }()

	releases, err := s.client.DiscoverActiveReleases(ctx)
	if err != nil {
		s.logger.Error("discover releases", "error", err)
		pass.Add(err)
		return
	}

	s.logger.Info("discovered active releases", "count", len(releases))

	activeSet := make(map[string]bool, len(releases))
	streams := make(map[string]bool)
	// Stream candidates are stored under their own Target Version, with
	// no release_versions row: they are not a release.
	syncStream := func(version string) {
		if z := StreamVersion(version); z != "" && !streams[z] {
			streams[z] = true
			pass.Add(s.syncVersion(ctx, z))
		}
	}

	for _, rel := range releases {
		activeSet[rel.FixVersion] = true

		rv := &model.ReleaseVersion{
			Name:                  rel.FixVersion,
			ReleaseTicketKey:      rel.ReleaseTicketKey,
			ReleaseTicketAssignee: rel.Assignee,
			KonfluxApplication:    rel.KonfluxApplication,
			DueDate:               rel.DueDate,
		}

		versionInfo, err := s.client.GetVersion(ctx, rel.FixVersion)
		if err != nil {
			// A fixVersion missing from JIRA is a data problem on one ticket, not sync health.
			s.logger.Warn("get version metadata", "version", rel.FixVersion, "error", err)
		} else {
			rv.Released = versionInfo.Released
			rv.Archived = versionInfo.Archived
			if versionInfo.ReleaseDate != "" {
				t, err := time.Parse("2006-01-02", versionInfo.ReleaseDate)
				if err == nil {
					rv.ReleaseDate = &t
				}
			}
		}

		if err := s.store.UpsertReleaseVersion(ctx, rv); err != nil {
			s.logger.Error("upsert version", "version", rel.FixVersion, "error", err)
			pass.Add(fmt.Errorf("upsert version %s: %w", rel.FixVersion, err))
		}

		pass.Add(s.syncVersion(ctx, rel.FixVersion))
		syncStream(rel.FixVersion)
	}

	// Keep syncing versions whose tracking ticket closed (dropped from
	// DiscoverActiveReleases): a shipped release still lists its issues,
	// and JIRA often leaves a shipped version unreleased. Archived ones stay frozen.
	dbVersions, err := s.store.ListAllReleaseVersions(ctx)
	if err != nil {
		s.logger.Error("list db versions", "error", err)
		pass.Add(fmt.Errorf("list db versions: %w", err))
		return
	}
	for _, dbv := range dbVersions {
		if dbv.Archived || activeSet[dbv.Name] {
			continue
		}
		versionInfo, err := s.client.GetVersion(ctx, dbv.Name)
		if err != nil {
			continue
		}
		dbv.Released = versionInfo.Released
		dbv.Archived = versionInfo.Archived
		if versionInfo.ReleaseDate != "" {
			t, err := time.Parse("2006-01-02", versionInfo.ReleaseDate)
			if err == nil {
				dbv.ReleaseDate = &t
			}
		}
		if err := s.store.UpsertReleaseVersion(ctx, &dbv); err != nil {
			s.logger.Error("upsert version", "version", dbv.Name, "error", err)
			pass.Add(fmt.Errorf("upsert version %s: %w", dbv.Name, err))
		}
		pass.Add(s.syncVersion(ctx, dbv.Name))
		syncStream(dbv.Name)
	}
}

// authError marks a rejected token, which every later pass will hit too.
func authError(err error) error {
	if err != nil && (strings.Contains(err.Error(), "returned 401") || strings.Contains(err.Error(), "returned 403")) {
		return fmt.Errorf("JIRA authentication failed: %w", err)
	}
	return err
}

// syncVersion fetches all issues for a single fixVersion and upserts them.
func (s *Syncer) syncVersion(ctx context.Context, fixVersion string) error {
	issues, err := s.client.SearchIssues(ctx, fixVersion)
	if err != nil {
		s.logger.Error("search issues", "version", fixVersion, "error", err)
		return fmt.Errorf("search issues %s: %w", fixVersion, err)
	}

	if err := s.withTx(ctx, func(txStore Store) error {
		var keys []string
		for _, issue := range issues {
			keys = append(keys, issue.Key)

			labels := strings.Join(issue.Fields.Labels, ",")
			assignee := ""
			if issue.Fields.Assignee != nil {
				assignee = issue.Fields.Assignee.DisplayName
			}

			jiraURL := fmt.Sprintf("%s/browse/%s", s.client.SiteURL(), issue.Key)

			record := &model.JiraIssueRecord{
				Key:        issue.Key,
				Summary:    issue.Fields.Summary,
				Status:     issue.Fields.Status.Name,
				Priority:   issue.Fields.Priority.Name,
				Labels:     labels,
				FixVersion: fixVersion,
				Assignee:   assignee,
				IssueType:  issue.Fields.IssueType.Name,
				Link:       jiraURL,
				QAContact:  issue.QAContact,
			}

			if err := txStore.UpsertJiraIssue(ctx, record); err != nil {
				return fmt.Errorf("upsert issue %s: %w", issue.Key, err)
			}
		}

		if err := txStore.DeleteJiraIssuesNotIn(ctx, fixVersion, keys); err != nil {
			return fmt.Errorf("cleanup issues: %w", err)
		}
		return nil
	}); err != nil {
		s.logger.Error("sync version", "version", fixVersion, "error", err)
		return fmt.Errorf("sync version %s: %w", fixVersion, err)
	}

	s.logger.Info("synced issues", "count", len(issues), "version", fixVersion)
	return nil
}
