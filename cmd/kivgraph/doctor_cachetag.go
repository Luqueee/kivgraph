package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

// doctorCacheDirectoriesShown bounds how many directories one repository lists:
// a monorepo can hold dozens of Cargo targets, and a line that long is not read.
const doctorCacheDirectoriesShown = 5

// reportDoctorCacheDirectories says which directories every walk skips because a
// tool tagged them as a cache (ADR 0122). The skip decides what the graph holds,
// and a directory of real sources that carries such a tag simply is not indexed:
// without this line nothing would ever say so. Finding none is reported too,
// because silence cannot be told from not having looked.
//
// A repository whose tree cannot be walked is a failure and not an empty list:
// "no tagged directory" would be a claim about a tree nobody read.
func reportDoctorCacheDirectories(ctx context.Context, report func(string, bool, string), repositories []workspace.Repository) {
	var listed []string
	for _, repository := range repositories {
		directories, err := workspace.CacheDirectories(ctx, repository)
		if err != nil {
			report("walk.cache_tag", false, fmt.Sprintf("%s: %v", repository.Name, err))
			continue
		}
		if len(directories) == 0 {
			continue
		}
		root := repository.RealPath
		if strings.TrimSpace(root) == "" {
			root = repository.Path
		}
		shown := make([]string, 0, doctorCacheDirectoriesShown)
		for _, directory := range directories[:min(len(directories), doctorCacheDirectoriesShown)] {
			relative, err := filepath.Rel(root, directory)
			if err != nil {
				relative = directory
			}
			shown = append(shown, filepath.ToSlash(relative))
		}
		entry := repository.Name + ": " + strings.Join(shown, ", ")
		if extra := len(directories) - len(shown); extra > 0 {
			entry += fmt.Sprintf(" and %d more", extra)
		}
		listed = append(listed, entry)
	}
	if len(listed) == 0 {
		report("walk.cache_tag", true, "no directory carries a cache tag")
		return
	}
	report("walk.cache_tag", true, "skipped by every walk and never indexed: "+strings.Join(listed, "; ")+
		" (remove a directory's CACHEDIR.TAG to index it)")
}
