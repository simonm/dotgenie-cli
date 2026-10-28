package cli

import (
	"fmt"
	"strings"

	"github.com/simonm/dotgenie/internal/config"
	"github.com/simonm/dotgenie/internal/dotfiles"
)

// layerSummary is one line naming the layers in the order they apply, with
// what each one holds, e.g.
// "common (42 files, common.yml) -> server (no files, server.yml) -> hosts/boron (3 files)".
func layerSummary(layers []dotfiles.Layer) string {
	var parts []string
	for _, l := range layers {
		var what []string
		switch l.Files {
		case 0:
			what = append(what, "no files")
		case 1:
			what = append(what, "1 file")
		default:
			what = append(what, fmt.Sprintf("%d files", l.Files))
		}
		if l.Packages != "" {
			what = append(what, l.Packages)
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", l.Name, strings.Join(what, ", ")))
	}
	return strings.Join(parts, " -> ")
}

// layerWarnings says what's wrong with this machine's system type: unset,
// not a known type, or a known type with nothing in its layer. Each of these
// used to be skipped without a word.
func layerWarnings(systemType string, layers []dotfiles.Layer) []string {
	types := strings.Join(config.SystemTypes, ", ")
	switch {
	case systemType == "":
		return []string{fmt.Sprintf("no system_type in config.local.yml, so only common and the host layer apply (types: %s)", types)}
	case !config.IsSystemType(systemType):
		return []string{fmt.Sprintf("system_type %q isn't a known type (%s); set one in config.local.yml", systemType, types)}
	}
	for _, l := range layers {
		if l.Name == systemType && l.Files == 0 && l.Packages == "" {
			return []string{fmt.Sprintf("the %s layer is empty: no files in dotfiles/%s/ and no packages/%s.yml", systemType, systemType, systemType)}
		}
	}
	return nil
}

// printLayers shows the layers and any warnings about them.
func printLayers(mgr *dotfiles.Manager) {
	layers := mgr.Layers()
	fmt.Printf("Layers:      %s\n", layerSummary(layers))
	for _, w := range layerWarnings(mgr.SystemType, layers) {
		fmt.Printf("Warning:     %s\n", w)
	}
}
