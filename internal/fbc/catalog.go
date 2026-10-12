// Package fbc renders ART file-based catalog (FBC) images from their registry
// and keeps the release channel's head bundle of each, by image digest.
package fbc

import (
	"errors"
	"fmt"
	"io"
	"slices"

	"k8s.io/apimachinery/pkg/util/yaml"
)

// Channel is a channel's head entry, the bundle a subscription to it installs.
type Channel struct {
	Name, HeadCSV, HeadBundlePullspec, Replaces, SkipRange string
}

type entry struct {
	Name      string   `json:"name"`
	Replaces  string   `json:"replaces"`
	Skips     []string `json:"skips"`
	SkipRange string   `json:"skipRange"`
}

// doc holds the fields of the olm.package, olm.channel and olm.bundle
// documents of a catalog that a channel head needs.
type doc struct {
	Schema  string  `json:"schema"`
	Name    string  `json:"name"`
	Image   string  `json:"image"`
	Entries []entry `json:"entries"`
}

// ParseChannel reads catalog YAML one document at a time and returns the
// head of channel: its one entry no other entry replaces or skips. The
// package's defaultChannel plays no part, as an older stream's catalog
// defaults to the newest stream's channel.
func ParseChannel(r io.Reader, channel string) (Channel, error) {
	var entries []entry
	found := false
	images := map[string]string{}
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
			images[d.Name] = d.Image
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
	if images[h.Name] == "" {
		return Channel{}, fmt.Errorf("channel %s head %s has no bundle image", channel, h.Name)
	}
	return Channel{Name: channel, HeadCSV: h.Name, HeadBundlePullspec: images[h.Name], Replaces: h.Replaces, SkipRange: h.SkipRange}, nil
}
