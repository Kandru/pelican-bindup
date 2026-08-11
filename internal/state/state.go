// Package state persists per-group update phase in a YAML sidecar.
package state

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Phase string

const (
	PhaseIdle           Phase = "idle"
	PhaseAwaitMainEmpty Phase = "await_main_empty"
	PhaseAwaitChildren  Phase = "await_children"
)

type GroupState struct {
	Phase               Phase             `yaml:"phase"`
	TargetBuildID       string            `yaml:"target_buildid,omitempty"`
	CachedRemoteBuildID string            `yaml:"cached_remote_buildid,omitempty"`
	SyncedBuildID       string            `yaml:"synced_buildid,omitempty"` // group done marker (= main+all children)
	ChildSynced         map[string]string `yaml:"child_synced,omitempty"`  // child UUID → last synced buildid
	UpdateDetectedAt    time.Time         `yaml:"update_detected_at,omitempty"`
	LastUpdateCheck     time.Time         `yaml:"last_update_check,omitempty"`
	PendingChildren     []string          `yaml:"pending_children,omitempty"`
	LastReboot          map[string]string `yaml:"last_reboot,omitempty"`
}

type file struct {
	Groups map[string]*GroupState `yaml:"groups"`
}

type Store struct {
	path string
}

func NewStore(stateFile string) *Store {
	return &Store{path: stateFile}
}

func (s *Store) Load(group string) (*GroupState, error) {
	all, err := s.readFile()
	if err != nil {
		return nil, err
	}
	gs, ok := all.Groups[group]
	if !ok || gs == nil {
		return newGroupState(), nil
	}
	normalizeGroupState(gs)
	return gs, nil
}

func (s *Store) Save(group string, gs *GroupState) error {
	all, err := s.readFile()
	if err != nil {
		return err
	}
	if all.Groups == nil {
		all.Groups = map[string]*GroupState{}
	}
	normalizeGroupState(gs)
	all.Groups[group] = gs
	return s.writeFile(all)
}

func (s *Store) readFile() (*file, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &file{Groups: map[string]*GroupState{}}, nil
		}
		return nil, err
	}
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse state %s: %w", s.path, err)
	}
	if f.Groups == nil {
		f.Groups = map[string]*GroupState{}
	}
	for name, gs := range f.Groups {
		if gs == nil {
			f.Groups[name] = newGroupState()
			continue
		}
		normalizeGroupState(gs)
	}
	return &f, nil
}

func (s *Store) writeFile(f *file) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func newGroupState() *GroupState {
	return &GroupState{
		Phase:           PhaseIdle,
		ChildSynced:     map[string]string{},
		LastReboot:    map[string]string{},
	}
}

func normalizeGroupState(gs *GroupState) {
	if gs.LastReboot == nil {
		gs.LastReboot = map[string]string{}
	}
	if gs.ChildSynced == nil {
		gs.ChildSynced = map[string]string{}
	}
	if gs.Phase == "" {
		gs.Phase = PhaseIdle
	}
}

func (gs *GroupState) RecordRestart(serverUUID string) {
	gs.LastReboot[serverUUID] = time.Now().UTC().Format(time.RFC3339)
}

func (gs *GroupState) MarkChildSynced(childUUID, buildID string) {
	if gs.ChildSynced == nil {
		gs.ChildSynced = map[string]string{}
	}
	gs.ChildSynced[childUUID] = buildID
}

// ChildNeedsSync is true until this child was recorded as synced to target.
// Bind-mounted volumes share main's manifest, so disk buildid cannot be used.
func (gs *GroupState) ChildNeedsSync(childUUID, target string) bool {
	if target == "" {
		return true
	}
	return gs.ChildSynced[childUUID] != target
}

func (gs *GroupState) ClearUpdatePending() {
	gs.TargetBuildID = ""
	gs.UpdateDetectedAt = time.Time{}
}
