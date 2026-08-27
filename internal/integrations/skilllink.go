package integrations

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Luqueee/kivgraph/internal/durable"
)

// statusBroken is a link of ours with nothing on the other end.
const statusBroken = "broken"

// canonicalSkillPath is the one copy of the skill a user edits.
//
// It sits beside `config.yaml` rather than under `~/.local/share`, and the
// reason is what it is for: this file exists to be changed. `~/.config/kivgraph`
// is where someone looks for the things they are allowed to edit, and a skill
// nobody can find is a skill nobody adapts.
func (manager Manager) canonicalSkillPath() string {
	return filepath.Join(manager.homeDir, ".config", "kivgraph", "skills", "kivgraph", "SKILL.md")
}

// linksSkills reports whether a client path should point at the canonical file
// rather than hold a copy of it.
//
// Project scope always copies, and that is not a limitation. A project-scoped
// path lives inside the repository and is committed as a matter of course; a
// symlink to an absolute path under this machine's home directory would arrive
// broken on every other clone and in CI.
//
// There is no Windows case to answer, which is the only reason this reads as
// simply as it does: New refuses every GOOS but darwin and linux, so the
// question of whether creating a symlink needs an elevated process never
// reaches here. A build that widened that would have to answer it.
func (manager Manager) linksSkills(scope Scope) bool {
	return scope == ScopeUser
}

// skillPlacement is what a client's skill path currently holds.
type skillPlacement uint8

const (
	// skillAbsent is nothing at the path.
	skillAbsent skillPlacement = iota
	// skillLinked is a link to the canonical file: what install produces.
	skillLinked
	// skillDangling is a link to the canonical file with nothing on the
	// other end. It reads as ours because it is, but no client can load a
	// skill through it, so reporting it as managed would be a status that
	// lies. Install restores the file and needs no forcing to do it.
	skillDangling
	// skillShippedCopy is a copy of the skill this build ships, which is
	// what an install before this change left behind. Install upgrades it
	// to a link, and does not need forcing to do so: nothing is lost,
	// because the bytes are the ones we would have written.
	skillShippedCopy
	// skillForeign is anything else -- an edited copy, a link somewhere
	// else, another tool's file. Replacing it loses something, so it is
	// refused without --force.
	skillForeign
)

// skillState is one inspected client path.
type skillState struct {
	placement skillPlacement
	// data is the file's content when it holds one, for the backup a
	// replacement takes.
	data []byte
	// target is where a link points, when it is one.
	target string
}

// inspectSkillPath reads a client path without following what it finds.
//
// readDestination refuses a symlink outright, and it is right to: it is used to
// write MCP entries and hook files, and writing through a link would put them
// somewhere nobody asked for. Here a link is the expected state, so this
// inspects with Lstat and reports what is there rather than failing on it.
func (manager Manager) inspectSkillPath(path string) (skillState, error) {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return skillState{placement: skillAbsent}, nil
	case err != nil:
		return skillState{}, fmt.Errorf("inspect skill path %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return skillState{}, fmt.Errorf("read skill link %q: %w", path, err)
		}
		placement := skillForeign
		if target == manager.canonicalSkillPath() {
			placement = skillLinked
			if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
				placement = skillDangling
			} else if err != nil {
				return skillState{}, fmt.Errorf("inspect canonical skill %q: %w", target, err)
			}
		}
		return skillState{placement: placement, target: target}, nil
	}
	if !info.Mode().IsRegular() {
		return skillState{}, fmt.Errorf("skill path %q is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return skillState{}, fmt.Errorf("read skill path %q: %w", path, err)
	}
	placement := skillForeign
	if bytes.Equal(data, embeddedSkill) {
		placement = skillShippedCopy
	}
	return skillState{placement: placement, data: data}, nil
}

// ErrCanonicalSkillEdited reports a canonical file this build did not write.
var ErrCanonicalSkillEdited = errors.New("the canonical skill differs from the one this build ships")

// ensureCanonicalSkill materialises the file every linked client points at.
//
// It writes only when there is nothing there or when what is there is the skill
// this build ships. A canonical file that differs is left exactly as it is, and
// that is the whole point of having one: an upgrade must not silently discard
// the edit that made the skill worth editing. `--force` takes the shipped
// version back.
func (manager Manager) ensureCanonicalSkill(force bool) (bool, error) {
	path := manager.canonicalSkillPath()
	data, exists, err := readDestination(path)
	if err != nil {
		return false, err
	}
	switch {
	case exists && bytes.Equal(data, embeddedSkill):
		return false, nil
	case exists && !force:
		// Not an error: the links still have to be made, and the caller
		// reports that the canonical was left alone.
		return false, nil
	}
	if err := writeDestination(path, embeddedSkill, exists, data); err != nil {
		return false, err
	}
	return true, nil
}

// canonicalIsEdited reports whether the canonical file carries changes.
func (manager Manager) canonicalIsEdited() (bool, error) {
	data, exists, err := readDestination(manager.canonicalSkillPath())
	if err != nil {
		return false, err
	}
	return exists && !bytes.Equal(data, embeddedSkill), nil
}

// installLinkedSkill points a client path at the canonical file.
func (manager Manager) installLinkedSkill(target Target, scope Scope, path string, dryRun, force bool) (Plan, error) {
	state, err := manager.inspectSkillPath(path)
	if err != nil {
		return Plan{}, err
	}
	if state.placement == skillForeign && !force {
		return Plan{}, incompatibleError(path)
	}
	edited, err := manager.canonicalIsEdited()
	if err != nil {
		return Plan{}, err
	}
	detail := "link the Kivgraph skill to " + manager.canonicalSkillPath()
	if edited && !force {
		detail = "link to the edited canonical skill at " + manager.canonicalSkillPath() + " (kept)"
	}
	if state.placement == skillLinked || state.placement == skillDangling {
		// The link is already ours. Only the file it points at may need
		// writing, which is exactly what a dangling one is missing.
		plan := Plan{Action: ActionInstall, Target: target, Scope: scope, Path: path,
			Status: "managed", Detail: "skill already points at " + manager.canonicalSkillPath()}
		if state.placement == skillDangling {
			plan.Status, plan.Changed = statusBroken, true
			plan.Detail = "restore " + manager.canonicalSkillPath() + ", which the link points at"
		}
		if dryRun {
			plan.DryRun = true
			if plan.Changed {
				plan.Status = "would-install"
			}
			return plan, nil
		}
		written, err := manager.ensureCanonicalSkill(force)
		if err != nil {
			return Plan{}, err
		}
		if written && state.placement == skillDangling {
			plan.Status = "installed"
		}
		return plan, nil
	}
	plan := Plan{Action: ActionInstall, Target: target, Scope: scope, Path: path,
		Status: string(placementName(state.placement)), Changed: true, DryRun: dryRun, Detail: detail}
	if dryRun {
		plan.Status = "would-install"
		return plan, nil
	}
	if _, err := manager.ensureCanonicalSkill(force); err != nil {
		return Plan{}, err
	}
	if err := manager.linkSkill(path, state); err != nil {
		return Plan{}, err
	}
	plan.Status = "installed"
	return plan, nil
}

// linkSkill replaces whatever is at a path with the link, keeping a backup of
// anything it displaces.
func (manager Manager) linkSkill(path string, state skillState) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create skill directory %q: %w", parent, err)
	}
	if state.placement == skillForeign && len(state.data) > 0 {
		// An edited copy is the one thing here worth keeping, so it is
		// kept before the link takes its place.
		if err := preserveBackup(path, state.data); err != nil {
			return err
		}
	}
	if state.placement != skillAbsent {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("replace skill path %q: %w", path, err)
		}
	}
	if err := os.Symlink(manager.canonicalSkillPath(), path); err != nil {
		return fmt.Errorf("link skill %q: %w", path, err)
	}
	return durable.Directory(parent)
}

// removeLinkedSkill takes away the link and leaves the canonical file.
//
// The canonical outliving a remove is deliberate. It is the file the user was
// invited to edit, and deleting an edit because one client was unregistered
// would be the opposite of the reason it exists.
func (manager Manager) removeLinkedSkill(target Target, scope Scope, path string, dryRun, force bool) (Plan, error) {
	state, err := manager.inspectSkillPath(path)
	if err != nil {
		return Plan{}, err
	}
	if state.placement == skillAbsent {
		return Plan{Action: ActionRemove, Target: target, Scope: scope, Path: path,
			Status: "absent", Detail: "skill is not installed"}, nil
	}
	if state.placement == skillForeign && !force {
		return Plan{}, incompatibleError(path)
	}
	plan := Plan{Action: ActionRemove, Target: target, Scope: scope, Path: path,
		Status: string(placementName(state.placement)), Changed: true, DryRun: dryRun,
		Detail: "remove the Kivgraph skill; " + manager.canonicalSkillPath() + " is kept"}
	if dryRun {
		plan.Status = "would-remove"
		return plan, nil
	}
	if state.placement == skillLinked || state.placement == skillDangling {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Plan{}, fmt.Errorf("remove skill link %q: %w", path, err)
		}
	} else if err := removeDestination(path, state.data); err != nil {
		return Plan{}, err
	}
	plan.Status = "removed"
	return plan, nil
}

// statusLinkedSkill reports what a client path holds.
func (manager Manager) statusLinkedSkill(target Target, scope Scope, path string) (Plan, error) {
	state, err := manager.inspectSkillPath(path)
	if err != nil {
		return Plan{}, err
	}
	edited, err := manager.canonicalIsEdited()
	if err != nil {
		return Plan{}, err
	}
	status := string(placementName(state.placement))
	detail := linkedStatusDetail(state, manager.canonicalSkillPath())
	if state.placement == skillLinked && edited {
		detail += "; that file carries local edits"
	}
	return Plan{Action: ActionStatus, Target: target, Scope: scope, Path: path,
		Status: status, Detail: detail}, nil
}

// placementName is the status vocabulary a placement reports as.
func placementName(placement skillPlacement) string {
	switch placement {
	case skillLinked:
		return "managed"
	case skillDangling:
		return statusBroken
	case skillShippedCopy:
		return statusSuperseded
	case skillForeign:
		return "incompatible"
	default:
		return "absent"
	}
}

// linkedStatusDetail explains a placement to a reader.
func linkedStatusDetail(state skillState, canonical string) string {
	switch state.placement {
	case skillLinked:
		return "skill points at " + canonical
	case skillDangling:
		return "skill points at " + canonical + ", which does not exist: install restores it"
	case skillShippedCopy:
		return "skill is a copy from an earlier install: install replaces it with a link"
	case skillForeign:
		if state.target != "" {
			return "skill points at " + state.target + ", which is not Kivgraph's"
		}
		return "skill exists and does not match the one Kivgraph ships"
	default:
		return "skill is not installed"
	}
}
