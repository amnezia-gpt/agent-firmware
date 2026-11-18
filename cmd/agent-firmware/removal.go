package main

import "github.com/amnezia-gpt/agent-firmware/internal/dockerengine"

func removalFlags(args []string) (confirmed, deleteData, valid bool) {
	for _, arg := range args {
		switch {
		case arg == "--confirm" && !confirmed:
			confirmed = true
		case arg == "--delete-data" && !deleteData:
			deleteData = true
		default:
			return false, false, false
		}
	}
	return confirmed, deleteData, true
}

type removalResult struct {
	State             string                    `json:"state"`
	Deregistration    string                    `json:"deregistration"`
	KeptVolumes       []dockerengine.VolumeInfo `json:"kept_volumes"`
	DeleteDataCommand string                    `json:"delete_data_command,omitempty"`
}

func removalResponse(state, deregistration string, kept []dockerengine.VolumeInfo, bootstrap bool) removalResult {
	if kept == nil {
		kept = []dockerengine.VolumeInfo{}
	}
	result := removalResult{State: state, Deregistration: deregistration, KeptVolumes: kept}
	if len(kept) > 0 {
		result.DeleteDataCommand = "sudo agent-firmware decommission --confirm --delete-data"
		if bootstrap {
			result.DeleteDataCommand = "sudo sh agent-firmware-install.sh --decommission --confirm --delete-data"
		}
	}
	return result
}
