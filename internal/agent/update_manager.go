package agent

import (
	"github.com/open-beagle/awecloud-signaling-server/internal/updater"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func newAgentUpdateManager(version, commitID, binarySHA256 string) (*updater.Manager, error) {
	return updater.NewManager(updater.Config{
		Component:       "agent",
		CurrentVersion:  version,
		CurrentCommitID: commitID,
		CurrentSHA256:   binarySHA256,
		StateDir:        "/etc/kubernetes/data/signaling/updater/agent",
		CurrentLink:     "/opt/bin/signal_agent",
		ServiceName:     "k8s-signaling",
	})
}

func updateDirectiveFromProto(directive *pb.UpdateDirective) updater.Directive {
	return updater.Directive{
		TaskID:        directive.TaskId,
		Component:     directive.Component,
		Version:       directive.Version,
		ArtifactID:    directive.ArtifactId,
		DownloadURL:   directive.DownloadUrl,
		Filename:      directive.Filename,
		Size:          directive.Size,
		SHA256:        directive.Sha256,
		Force:         directive.Force,
		NotBeforeUnix: directive.NotBeforeUnix,
		DeadlineUnix:  directive.DeadlineUnix,
		CommitID:      directive.CommitId,
	}
}

func updateStatusesToProto(statuses []updater.Status) []*pb.UpdateStatus {
	result := make([]*pb.UpdateStatus, 0, len(statuses))
	for _, status := range statuses {
		result = append(result, &pb.UpdateStatus{
			TaskId:          status.TaskID,
			Phase:           status.Phase,
			Progress:        int32(status.Progress),
			CurrentVersion:  status.CurrentVersion,
			Sequence:        status.Sequence,
			ErrorCode:       status.ErrorCode,
			ErrorMessage:    status.ErrorMessage,
			CurrentCommitId: status.CurrentCommitID,
			CurrentSha256:   status.CurrentSHA256,
		})
	}
	return result
}
