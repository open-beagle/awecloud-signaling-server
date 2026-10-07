package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/model"
)

var errTunnelNodeBinding = errors.New("ambiguous or unsupported tunnel node binding")

// A Headscale ID is an external identifier, never a local Node primary key.
// Names (including provider-prefixed names) are not a fallback identity key.
func linkedTunnelNode(ctx context.Context, headscaleID uint64) (*model.Node, error) {
	if headscaleID == 0 {
		return nil, errTunnelNodeBinding
	}
	var nodes []model.Node
	if err := db.DB.WithContext(ctx).Where("headscale_node_id = ?", headscaleID).Limit(2).Find(&nodes).Error; err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, nil
	}
	if len(nodes) != 1 || (nodes[0].Type != model.NodeTypeAgent && nodes[0].Type != model.NodeTypeDesktop) {
		return nil, errTunnelNodeBinding
	}
	return &nodes[0], nil
}

func tunnelNodeLookupError(c *gin.Context, err error) {
	if status.Code(err) == codes.NotFound {
		c.JSON(http.StatusNotFound, NewErrorResponse("Node 不存在"))
		return
	}
	c.JSON(http.StatusInternalServerError, NewErrorResponse("获取 Node 失败"))
}

func tunnelNodeBindingError(c *gin.Context, err error) {
	if errors.Is(err, errTunnelNodeBinding) {
		c.JSON(http.StatusConflict, NewErrorResponse("本地 Node 绑定不唯一或类型异常，操作未执行"))
		return
	}
	c.JSON(http.StatusInternalServerError, NewErrorResponse("读取本地 Node 绑定失败，操作未执行"))
}

func tunnelNodeAuditState(node *model.Node) interface{} {
	if node == nil {
		return nil
	}
	return gin.H{"id": node.ID, "user_id": node.UserID, "type": node.Type,
		"headscale_node_id": node.HeadscaleNodeID, "ip": node.IP}
}
