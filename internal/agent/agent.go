package agent

import (
	"log"

	"github.com/open-beagle/awecloud-signaling-server/internal/common/config"
)

type Agent struct {
	config *config.AgentConfig
}

func NewAgent(cfg *config.AgentConfig) (*Agent, error) {
	return &Agent{
		config: cfg,
	}, nil
}

func (a *Agent) Run() error {
	log.Println("Agent运行中...")
	log.Println("TODO: 实现Agent功能")

	// TODO: 连接到Server
	// TODO: 实现心跳
	// TODO: 处理Server消息

	select {} // 阻塞
}
