package telemetry

import (
	"bytes"
	"io"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// responseBodyWriter 用于捕获响应体
type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// GinMiddleware 返回 Gin 的 OpenTelemetry 中间件
func GinMiddleware(serviceName string) gin.HandlerFunc {
	if serviceName == "" {
		serviceName = "awecloud-signaling-server"
	}

	return func(c *gin.Context) {
		// 跳过健康检查路径
		path := c.Request.URL.Path
		if path == "/health" || path == "/health/ready" {
			c.Next()
			return
		}

		// 从请求头提取 trace context
		ctx := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		// 创建 span
		spanName := c.Request.Method + " " + c.FullPath()
		if c.FullPath() == "" {
			spanName = c.Request.Method + " " + path
		}

		// 构建完整请求 URL
		fullURL := path
		if c.Request.URL.RawQuery != "" {
			fullURL = path + "?" + c.Request.URL.RawQuery
		}

		ctx, span := Tracer().Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(c.Request.Method),
				semconv.URLPath(path),
				semconv.URLScheme(c.Request.URL.Scheme),
				semconv.ServerAddress(c.Request.Host),
				semconv.UserAgentOriginal(c.Request.UserAgent()),
				semconv.ClientAddress(c.ClientIP()),
				attribute.String("http.request.url", fullURL),
			),
		)
		defer span.End()

		// 读取并记录请求体
		contentType := c.ContentType()
		if c.Request.Body != nil {
			bodyBytes, _ := io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			// 记录请求体大小
			span.SetAttributes(attribute.Int("http.request.body_size", len(bodyBytes)))

			// 记录脱敏后的请求体
			if len(bodyBytes) > 0 {
				sanitizedBody := SanitizeBody(bodyBytes, contentType)
				span.SetAttributes(attribute.String("http.request.body", sanitizedBody))
			}
		}

		// 包装 ResponseWriter 以捕获响应体
		respBody := &bytes.Buffer{}
		writer := &responseBodyWriter{ResponseWriter: c.Writer, body: respBody}
		c.Writer = writer

		// 将 context 传递给后续处理
		c.Request = c.Request.WithContext(ctx)

		// 执行后续处理
		c.Next()

		// 记录响应状态
		status := c.Writer.Status()
		span.SetAttributes(semconv.HTTPResponseStatusCode(status))

		// 记录响应体大小
		respBodyBytes := respBody.Bytes()
		span.SetAttributes(attribute.Int("http.response.body_size", len(respBodyBytes)))

		// 记录脱敏后的响应体
		if len(respBodyBytes) > 0 {
			respContentType := c.Writer.Header().Get("Content-Type")
			sanitizedResp := SanitizeBody(respBodyBytes, respContentType)
			span.SetAttributes(attribute.String("http.response.body", sanitizedResp))
		}

		// 记录错误
		if len(c.Errors) > 0 {
			span.SetAttributes(attribute.String("gin.errors", c.Errors.String()))
		}

		// 根据状态码设置 span 状态
		if status >= 400 {
			span.SetAttributes(attribute.Bool("error", true))
		}
	}
}
