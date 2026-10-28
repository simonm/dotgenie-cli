package cli

import (
	"strings"
	"testing"

	"github.com/simonm/dotgenie/internal/dotfiles"
)

var boronLayers = []dotfiles.Layer{
	{Name: "common", Files: 42, Packages: "common.yml"},
	{Name: "server", Files: 0, Packages: "server.yml"},
	{Name: "hosts/boron", Files: 1},
}

func TestLayerSummary(t *testing.T) {
	want := "common (42 files, common.yml) -> server (no files, server.yml) -> hosts/boron (1 file)"
	if got := layerSummary(boronLayers); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestLayerWarnings(t *testing.T) {
	if w := layerWarnings("server", boronLayers); len(w) != 0 {
		t.Errorf("server with a package file: %v", w)
	}
	empty := []dotfiles.Layer{{Name: "common", Files: 3}, {Name: "server"}, {Name: "hosts/boron"}}
	cases := map[string]struct {
		layers []dotfiles.Layer
		want   string
	}{
		"":        {boronLayers, "no system_type"},
		"minimal": {boronLayers, `"minimal" isn't a known type`},
		"server":  {empty, "the server layer is empty"},
	}
	for typ, c := range cases {
		w := layerWarnings(typ, c.layers)
		if len(w) != 1 || !strings.Contains(w[0], c.want) {
			t.Errorf("%q: got %v, want one warning containing %q", typ, w, c.want)
		}
	}
}
