package proxy

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/open-beagle/awecloud-signaling-server/internal/common/logger"
)

// HeadscaleProxy Headscale 反向代理
type HeadscaleProxy struct {
	target *url.URL
	proxy  *httputil.ReverseProxy
}

// NewHeadscaleProxy 创建 Headscale 反向代理
func NewHeadscaleProxy(targetURL string) (*HeadscaleProxy, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	// 自定义 Director 处理路径重写
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// 移除 /headscale 前缀
		req.URL.Path = strings.TrimPrefix(req.URL.Path, "/headscale")
		if req.URL.Path == "" {
			req.URL.Path = "/"
		}
		req.Host = target.Host
		logger.Debugf("[headscale-proxy] %s %s -> %s", req.Method, req.URL.Path, target.String()+req.URL.Path)
	}

	// 错误处理
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Errorf("[headscale-proxy] 代理错误: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}

	return &HeadscaleProxy{
		target: target,
		proxy:  proxy,
	}, nil
}

// Handler 返回 Gin 处理函数
func (p *HeadscaleProxy) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		p.proxy.ServeHTTP(c.Writer, c.Request)
	}
}
