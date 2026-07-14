package agent

import (
	"os"

	"github.com/open-beagle/awecloud-signaling-server/internal/updater"
	pb "github.com/open-beagle/awecloud-signaling-server/pkg/proto"
)

func newAgentUpdateManager(version string) (*updater.Manager, error) {
	return updater.NewManager(updater.Config{
		Component:       "agent",
		CurrentVersion:  version,
		StateDir:        "/etc/kubernetes/data/signaling/updater/agent",
		CurrentLink:     "/opt/bin/signal_agent",
		ServiceName:     "k8s-signaling",
		PublicKeyBase64: os.Getenv("SIGNAL_UPDATER_PUBLIC_KEY"),
	})
}

func updateDirectiveFromProto(directive *pb.UpdateDirective) updater.Directive {
	return updater.Directive{
		TaskID:        directive.TaskId,
		Component:     directive.Component,
		Version:       directive.Version,
		DownloadURL:   directive.DownloadUrl,
		Filename:      directive.Filename,
		Size:          directive.Size,
		SHA256:        directive.Sha256,
		Signature:     directive.Signature,
		KeyID:         directive.KeyId,
		NotBeforeUnix: directive.NotBeforeUnix,
		DeadlineUnix:  directive.DeadlineUnix,
	}
}

func updateStatusesToProto(statuses []updater.Status) []*pb.UpdateStatus {
	result := make([]*pb.UpdateStatus, 0, len(statuses))
	for _, status := range statuses {
		result = append(result, &pb.UpdateStatus{
			TaskId:         status.TaskID,
			Phase:          status.Phase,
			Progress:       int32(status.Progress),
			CurrentVersion: status.CurrentVersion,
			Sequence:       status.Sequence,
			ErrorCode:      status.ErrorCode,
			ErrorMessage:   status.ErrorMessage,
		})
	}
	return result
}
