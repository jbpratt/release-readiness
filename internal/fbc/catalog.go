// Package fbc renders ART file-based catalog (FBC) images from their registry
// and keeps the release channel's head bundle of each, by image digest.
package fbc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/yaml"
)

// Channel is a channel's head entry, the bundle a subscription to it installs,
// and Previous, the entry below it by version.
type Channel struct {
	Name, HeadCSV, HeadBundlePullspec string
	HeadRelatedImages                 []RelatedImage
	Previous                          Bundle
}

// Bundle is a channel entry's bundle; zero for none.
type Bundle struct {
	CSV, Version, BundlePullspec string
	RelatedImages                []RelatedImage
}

// RelatedImage is an image a bundle lists for OLM to pull.
type RelatedImage struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

type entry struct {
	Name     string   `json:"name"`
	Replaces string   `json:"replaces"`
	Skips    []string `json:"skips"`
}

type property struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// doc holds the fields of the olm.package, olm.channel and olm.bundle
// documents of a catalog that a channel head needs.
type doc struct {
	Schema        string         `json:"schema"`
	Name          string         `json:"name"`
	Image         string         `json:"image"`
	Entries       []entry        `json:"entries"`
	Properties    []property     `json:"properties"`
	RelatedImages []RelatedImage `json:"relatedImages"`
}

// ParseChannel reads catalog YAML one document at a time and returns the
// head of channel: its one entry no other entry replaces or skips. The
// package's defaultChannel plays no part, as an older stream's catalog
// defaults to the newest stream's channel.
func ParseChannel(r io.Reader, channel string) (Channel, error) {
	var entries []entry
	found := false
	bundles := map[string]Bundle{}
	dec := yaml.NewYAMLOrJSONDecoder(r, 4096)
	for {
		var d doc
		if err := dec.Decode(&d); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return Channel{}, fmt.Errorf("parse catalog: %w", err)
		}
		switch {
		case d.Schema == "olm.channel" && d.Name == channel:
			entries = append(entries, d.Entries...)
			found = true
		case d.Schema == "olm.bundle":
			b := Bundle{CSV: d.Name, BundlePullspec: d.Image, RelatedImages: d.RelatedImages}
			for _, p := range d.Properties {
				if p.Type != "olm.package" {
					continue
				}
				var v struct {
					Version string `json:"version"`
				}
				if err := json.Unmarshal(p.Value, &v); err != nil {
					return Channel{}, fmt.Errorf("bundle %s: olm.package: %w", d.Name, err)
				}
				b.Version = v.Version
			}
			bundles[d.Name] = b
		}
	}
	if !found {
		return Channel{}, fmt.Errorf("no channel %s", channel)
	}
	var superseded []string
	for _, e := range entries {
		superseded = append(append(superseded, e.Replaces), e.Skips...)
	}
	var heads []entry
	for _, e := range entries {
		if !slices.Contains(superseded, e.Name) {
			heads = append(heads, e)
		}
	}
	if len(heads) != 1 {
		return Channel{}, fmt.Errorf("channel %s has %d heads, want 1", channel, len(heads))
	}
	h := heads[0]
	hb := bundles[h.Name]
	if hb.BundlePullspec == "" {
		return Channel{}, fmt.Errorf("channel %s head %s has no bundle image", channel, h.Name)
	}
	ch := Channel{
		Name: channel, HeadCSV: h.Name, HeadBundlePullspec: hb.BundlePullspec, HeadRelatedImages: hb.RelatedImages,
	}
	if hv, ok := parseVersion(hb.Version); ok {
		var prev version
		for _, e := range entries {
			if v, ok := parseVersion(bundles[e.Name].Version); ok && v.compare(hv) < 0 && (ch.Previous.CSV == "" || v.compare(prev) > 0) {
				ch.Previous, prev = bundles[e.Name], v
			}
		}
	}
	return ch, nil
}

// version is a semver's major, minor and patch, and its prerelease.
type version struct {
	n   [3]int
	pre string
}

func parseVersion(s string) (version, bool) {
	core, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	v := version{pre: pre}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return version{}, false
		}
		v.n[i] = n
	}
	return v, true
}

func (a version) compare(b version) int {
	if c := slices.Compare(a.n[:], b.n[:]); c != 0 {
		return c
	}
	// A prerelease sorts below its release.
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	}
	return strings.Compare(a.pre, b.pre)
}
