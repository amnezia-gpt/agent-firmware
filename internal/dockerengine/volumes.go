package dockerengine

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/amnezia-gpt/agent-firmware/internal/backendprofile"
	"github.com/amnezia-gpt/agent-firmware/internal/configuration"
	"github.com/amnezia-gpt/agent-firmware/internal/workload"
	"github.com/amnezia-gpt/agent-firmware/internal/workloadrelease"
	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

const (
	volumeRetentionLabel = workload.LabelPrefix + "retention"
	volumeIssuerLabel    = workload.LabelPrefix + "issuer"
	installationLabel    = workload.LabelPrefix + "installation"
	managerLabel         = workload.LabelPrefix + "managed-by"
)

type VolumeInfo struct {
	Name         string                    `json:"name"`
	Issuer       string                    `json:"issuer,omitempty"`
	CreatedAt    string                    `json:"created_at,omitempty"`
	Retention    workloadrelease.Retention `json:"retention,omitempty"`
	installation string
}

func ownedVolume(v volume.Volume, installation string) bool {
	name, ok := strings.CutPrefix(v.Name, "af-"+installation+"-")
	return enrollmentID.MatchString(installation) && ok && resourceName.MatchString(name) &&
		owned(v.Labels, installation, "volume") && v.Driver == "local" && len(v.Options) == 0
}

func volumeInfo(v volume.Volume) (VolumeInfo, error) {
	installation := v.Labels[installationLabel]
	if !ownedVolume(v, installation) {
		return VolumeInfo{}, workload.ErrConflict
	}
	info := VolumeInfo{Name: v.Name, Retention: workloadrelease.Retention(v.Labels[volumeRetentionLabel]), installation: installation}
	if created, err := time.Parse(time.RFC3339Nano, v.CreatedAt); err == nil {
		info.CreatedAt = created.Format(time.RFC3339Nano)
	}
	switch info.Retention {
	case "":
		return info, nil
	case workloadrelease.Enrollment, workloadrelease.UserData:
		info.Issuer = v.Labels[volumeIssuerLabel]
		if !backendprofile.ValidIssuer(info.Issuer) || workload.EnrollmentID(info.Issuer) != installation {
			return VolumeInfo{}, workload.ErrConflict
		}
		return info, nil
	default:
		return VolumeInfo{}, workload.ErrVolumeClass
	}
}

func (e *Engine) listVolumes(ctx context.Context, filters client.Filters) ([]volume.Volume, error) {
	call, cancel := context.WithTimeout(ctx, e.callTimeout)
	defer cancel()
	result, err := e.api.VolumeList(call, client.VolumeListOptions{Filters: filters})
	if err != nil {
		return nil, classify(err)
	}
	if len(result.Warnings) != 0 {
		return nil, workload.ErrEngine
	}
	return result.Items, nil
}

// CheckEnrollmentVolumes performs the complete read-only volume gate at CLI
// entry points. Reconciliation checks mounts independently so the previous
// release can recover even if a failed update created an additional volume.
func (e *Engine) CheckEnrollmentVolumes(ctx context.Context, stack workload.Stack, fresh bool) (bool, error) {
	volumes, err := e.listVolumes(ctx, nil)
	if err != nil {
		return false, err
	}
	attached := false
	prefix := "af-" + stack.Installation + "-"
	for _, v := range volumes {
		if v.Labels[installationLabel] != stack.Installation && !strings.HasPrefix(v.Name, prefix) {
			continue
		}
		info, err := volumeInfo(v)
		if err != nil || v.Labels[installationLabel] != stack.Installation {
			if err != nil {
				return false, err
			}
			return false, workload.ErrConflict
		}
		if info.Retention == "" {
			return false, workload.ErrVolumeUnclassified
		}
		if info.Issuer != stack.Issuer {
			return false, workload.ErrConflict
		}
		expected, declared := stack.Volumes[v.Name]
		if declared && expected != info.Retention {
			return false, workload.ErrVolumeClass
		}
		if info.Retention == workloadrelease.UserData {
			if !declared {
				return false, workload.ErrUserVolumeMissing
			}
			attached = true
		} else if fresh {
			// An enrollment-owned volume can hold credentials sealed to
			// a previous set of operational keys.
			return false, configuration.ErrRetainedVolumes
		}
	}
	return attached, nil
}

func (e *Engine) volumes(ctx context.Context, stack workload.Stack, create bool) error {
	for _, name := range slices.Sorted(maps.Keys(stack.Volumes)) {
		call, cancel := context.WithTimeout(ctx, e.callTimeout)
		result, err := e.api.VolumeInspect(call, name, client.VolumeInspectOptions{})
		if errdefs.IsNotFound(err) && create {
			tags := labels(stack.Installation, "volume")
			tags[volumeRetentionLabel] = string(stack.Volumes[name])
			tags[volumeIssuerLabel] = stack.Issuer
			created, createErr := e.api.VolumeCreate(call, client.VolumeCreateOptions{Name: name, Driver: "local", Labels: tags})
			result.Volume, err = created.Volume, createErr
		} else if errdefs.IsNotFound(err) {
			cancel()
			continue
		}
		cancel()
		if err != nil {
			return classify(err)
		}
		if !ownedVolume(result.Volume, stack.Installation) || result.Volume.Name != name {
			return workload.ErrConflict
		}
		info, err := volumeInfo(result.Volume)
		if err != nil {
			return err
		}
		if info.Retention == "" {
			return workload.ErrVolumeUnclassified
		}
		if info.Issuer != stack.Issuer || info.Retention != stack.Volumes[name] {
			return workload.ErrVolumeClass
		}
	}
	return nil
}

func (e *Engine) managedVolumeInfo(ctx context.Context, installation string) ([]VolumeInfo, error) {
	volumes, err := e.listVolumes(ctx, client.Filters{}.Add("label", managerLabel+"="+workload.Manager))
	if err != nil {
		return nil, err
	}
	infos := make([]VolumeInfo, 0, len(volumes))
	for _, v := range volumes {
		if v.Labels[managerLabel] != workload.Manager ||
			installation != "" && v.Labels[installationLabel] != installation {
			continue
		}
		info, err := volumeInfo(v)
		if err != nil {
			return nil, err
		}
		infos = append(infos, info)
	}
	slices.SortFunc(infos, func(a, b VolumeInfo) int {
		if order := strings.Compare(a.Issuer, b.Issuer); order != 0 {
			return order
		}
		return strings.Compare(a.Name, b.Name)
	})
	return infos, nil
}

func (e *Engine) CheckManufactureVolumes(ctx context.Context) error {
	volumes, err := e.managedVolumeInfo(ctx, "")
	if err != nil {
		return err
	}
	for _, v := range volumes {
		if v.Retention != workloadrelease.UserData {
			return configuration.ErrRetainedVolumes
		}
	}
	return nil
}

// RetainedData includes older firmware volumes with no declared class.
func (e *Engine) RetainedData(ctx context.Context, installation string) ([]VolumeInfo, error) {
	volumes, err := e.managedVolumeInfo(ctx, installation)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(volumes, func(v VolumeInfo) bool { return v.Retention == workloadrelease.Enrollment }), nil
}
