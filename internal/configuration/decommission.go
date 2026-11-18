package configuration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/amnezia-gpt/agent-firmware/internal/host"
	"github.com/amnezia-gpt/agent-firmware/internal/privatefile"
	"github.com/amnezia-gpt/agent-firmware/internal/strictjson"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
)

const (
	decommissionFile             = "decommission.json"
	ErrDecommissionPending Error = "decommission_pending"
	NoDeregistration             = "not_requested"
	LegacyDeregistration         = "not_attempted"
)

var (
	legacyProfileID = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	legacyKeyTemp   = regexp.MustCompile(`^\.device-api-[a-f0-9]{24}$`)
	legacyStateTemp = regexp.MustCompile(`^\.state-[a-f0-9]{24}$`)
)

// DecommissionRecord survives removal of the service and the installed binary.
type DecommissionRecord struct {
	Version               int      `json:"schema_version"`
	Legacy                bool     `json:"legacy"`
	Installation          string   `json:"installation"`
	Images                []string `json:"images"`
	Deregistration        string   `json:"deregistration"`
	LocalResourcesRemoved bool     `json:"local_resources_removed"`
	DeleteData            bool     `json:"delete_data"`
	Discover              bool     `json:"discover_installations"`
}

func (s Store) ExclusiveForDecommission(fn func(*Session) error) error {
	return s.exclusive(true, fn)
}

func checkDecommission(root *os.Root) error {
	if _, err := root.Lstat(decommissionFile); err == nil {
		return ErrDecommissionPending
	} else if !errors.Is(err, os.ErrNotExist) {
		return ErrRead
	}
	return nil
}

func (s *Session) DecommissionRecord() (*DecommissionRecord, error) {
	raw, err := readRecord(s.root, decommissionFile)
	if errors.Is(err, ErrMissing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fields, ok := strictjson.Object(raw, "schema_version", "legacy", "installation", "images", "deregistration", "local_resources_removed", "delete_data", "discover_installations")
	var record DecommissionRecord
	intent, discovery := string(fields["delete_data"]), string(fields["discover_installations"])
	if !ok || string(fields["schema_version"]) != "2" || intent != "true" && intent != "false" ||
		discovery != "true" && discovery != "false" || json.Unmarshal(raw, &record) != nil || !record.valid() {
		return nil, ErrCorrupt
	}
	return &record, nil
}

func (r DecommissionRecord) valid() bool {
	if r.Installation != "" && !enrollmentIDPattern.MatchString(r.Installation) ||
		len(r.Images) > workloadrelease.MaxServices ||
		r.LocalResourcesRemoved && r.Deregistration == DeregistrationPending {
		return false
	}
	for _, image := range r.Images {
		if !workloadrelease.IsPinnedReference(image) {
			return false
		}
	}
	if r.Legacy || r.Discover {
		return r.Deregistration == LegacyDeregistration && (r.Installation != "" || r.Discover)
	}
	if r.Installation == "" {
		return r.Deregistration == NoDeregistration
	}
	return validDeregistration(r.Deregistration)
}

func (s *Session) SaveDecommission(record *DecommissionRecord) error {
	if !record.valid() {
		return ErrInvalid
	}
	record.Version = 2
	raw, err := json.Marshal(record)
	if err != nil {
		return ErrWrite
	}
	return s.store.atomic(s.root, decommissionFile, raw)
}

// DecommissionSnapshot reads identity for a pending domain deregistration.
func (s *Session) DecommissionSnapshot() (Snapshot, error) { return loadSnapshot(s.root) }

// LegacyDecommission extracts just the coordinates needed for offline removal
// from the v0.1.5 accepted record. Old inputs remain unusable for deployment.
func (s *Session) LegacyDecommission() (*DecommissionRecord, error) {
	raw, err := readRecord(s.root, host.LegacyState)
	if errors.Is(err, ErrMissing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fields, ok := strictjson.Object(raw, "schema_version", "generation", "backend_profile", "workload_release")
	record := &DecommissionRecord{Legacy: true, Discover: true, Deregistration: LegacyDeregistration}
	var profile, release []byte
	if !ok || string(bytes.TrimSpace(fields["schema_version"])) != "1" ||
		json.Unmarshal(fields["backend_profile"], &profile) != nil || json.Unmarshal(fields["workload_release"], &release) != nil {
		return record, nil
	}
	p, ok := strictjson.Object(profile, "schema_version", "backend_profile", "auth_issuer", "router_base_url",
		"runtime_gateway_base_url", "ingress_trust_bundle", "ingress_trust_sha256")
	id, issuer := strictjson.StringField(p, "backend_profile"), strictjson.StringField(p, "auth_issuer")
	u, err := url.Parse(issuer)
	if !ok || string(bytes.TrimSpace(p["schema_version"])) != "3" || !legacyProfileID.MatchString(id) ||
		err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.ForceQuery ||
		u.RawQuery != "" || strings.ContainsAny(issuer, "#\\\r\n\t ") {
		return record, nil
	}
	u.Host = strings.ToLower(u.Host)
	if strings.HasSuffix(u.EscapedPath(), "/") {
		u.Path = strings.TrimSuffix(u.Path, "/")
		u.RawPath = strings.TrimSuffix(u.RawPath, "/")
	}
	sum := sha256.Sum256([]byte("urn:amnezia-gpt:workload-contract:device:v1\n" + id + "\n" + u.String()))
	record.Installation = hex.EncodeToString(sum[:16])
	record.Discover = false
	var manifest struct {
		Version int `json:"schema_version"`
		Images  map[string]struct {
			workloadrelease.Image
			Reference string `json:"reference"`
		} `json:"images"`
	}
	if json.Unmarshal(release, &manifest) != nil || manifest.Version != 2 || len(manifest.Images) != 2 {
		return record, nil
	}
	for _, image := range manifest.Images {
		if image.Reference != image.Image.Reference() || !workloadrelease.IsPinnedReference(image.Reference) {
			record.Images = nil
			return record, nil
		}
		record.Images = append(record.Images, image.Reference)
	}
	return record, nil
}

// CheckDecommissionFiles inventories the private state before any teardown.
// The same checks apply after a partial removal.
func (s *Session) CheckDecommissionFiles(legacy bool) error {
	_, err := s.decommissionFiles(legacy)
	return err
}

func (s *Session) decommissionFiles(legacy bool) ([]string, error) {
	entries, err := fsEntries(s.root)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		info, err := s.root.Lstat(name)
		if err != nil {
			return nil, ErrUnsafe
		}
		owner := privatefile.EffectiveOwner()
		switch {
		case name == decommissionFile, name == host.OperationLock, name == host.OperationJournal, name == host.EngineState,
			legacy && (name == host.LegacyState || name == "device-api-required" || name == "device-api-init.lock"),
			!legacy && (name == host.DeviceState || name == host.RootSecret || name == removalFile),
			pendingRecordName.MatchString(name) && !info.IsDir():
			if !owner.Regular(info, 0600, false) {
				return nil, ErrUnsafe
			}
		case legacy && (name == "device-api" || legacyKeyTemp.MatchString(name)),
			!legacy && (name == host.EnrollmentDir || name == detachedEnrollment || pendingRecordName.MatchString(name)):
			if !owner.Directory(info, 0700) || s.checkDecommissionSubdir(name, legacy) != nil {
				return nil, ErrUnsafe
			}
		default:
			return nil, ErrUnsafe
		}
		if name != decommissionFile && name != host.OperationLock {
			files = append(files, name)
		}
	}
	return files, nil
}

func (s *Session) checkDecommissionSubdir(name string, legacy bool) error {
	dir, err := s.root.OpenRoot(name)
	if err != nil {
		return ErrUnsafe
	}
	defer dir.Close()
	entries, err := fsEntries(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		file := entry.Name()
		known := file == enrollmentState || pendingRecordName.MatchString(file)
		if legacy {
			known = file == "state.json" || file == "key.cred" || legacyStateTemp.MatchString(file)
		}
		info, err := dir.Lstat(file)
		if !known || err != nil || !privatefile.EffectiveOwner().Regular(info, 0600, false) {
			return ErrUnsafe
		}
	}
	return nil
}

func fsEntries(root *os.Root) ([]os.DirEntry, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, ErrRead
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, ErrRead
	}
	return entries, nil
}

func (s *Session) FinishDecommission(record *DecommissionRecord) error {
	if !record.LocalResourcesRemoved {
		return ErrDecommissionPending
	}
	files, err := s.decommissionFiles(record.Legacy)
	if err != nil {
		return err
	}
	for _, name := range files {
		if err := s.root.RemoveAll(name); err != nil {
			return ErrWrite
		}
	}
	if err := s.store.synchronize(s.root); err != nil {
		return err
	}
	if err := s.root.Remove(host.OperationLock); err != nil {
		return ErrWrite
	}
	if err := s.root.Remove(decommissionFile); err != nil {
		return ErrWrite
	}
	if err := os.Remove(s.store.Path); err != nil {
		return ErrWrite
	}
	if privatefile.SyncDirectory(nil, filepath.Dir(s.store.Path), nil) != nil {
		return ErrUncertain
	}
	return nil
}
