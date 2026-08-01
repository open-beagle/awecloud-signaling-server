package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/open-beagle/awecloud-signaling-server/internal/server/db"
	"github.com/open-beagle/awecloud-signaling-server/internal/server/service"
)

const ErrorCodePlatformSupplyQueryFailed = "PLATFORM_SUPPLY_QUERY_FAILED"

type PlatformSupplyAPI struct{}

func NewPlatformSupplyAPI() *PlatformSupplyAPI { return &PlatformSupplyAPI{} }

func (a *PlatformSupplyAPI) ListConflicts(c *gin.Context) {
	authorization, ok := currentManagementAuthorization(c)
	if !ok {
		writeManagementRequestError(c, service.ErrManagementPermissionDenied)
		return
	}
	input, ok := platformSupplyConflictListInput(c)
	if !ok {
		return
	}
	result, err := service.NewPlatformSupplyGovernanceService(db.DB).ListSupplyConflicts(c.Request.Context(), authorization, input)
	if err != nil {
		writePlatformSupplyError(c, err)
		return
	}
	c.JSON(http.StatusOK, NewPagedResponse(result.Items, result.Total, input.Page, input.PageSize))
}

func platformSupplyConflictListInput(c *gin.Context) (service.PlatformSupplyConflictListInput, bool) {
	input := service.PlatformSupplyConflictListInput{
		Search: strings.TrimSpace(c.Query("search")), Type: strings.TrimSpace(c.Query("type")), Page: 1, PageSize: 20,
	}
	for key := range c.Request.URL.Query() {
		switch key {
		case "search", "type", "page", "size":
		default:
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Platform 冲突查询参数无效")
			return input, false
		}
	}
	var err error
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		input.Page, err = strconv.Atoi(raw)
		if err != nil {
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "分页参数无效")
			return input, false
		}
	}
	if raw := strings.TrimSpace(c.Query("size")); raw != "" {
		input.PageSize, err = strconv.Atoi(raw)
		if err != nil {
			codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "分页参数无效")
			return input, false
		}
	}
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > 100 || len(input.Search) > 200 || len(input.Type) > 32 {
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Platform 冲突查询参数无效")
		return input, false
	}
	return input, true
}

func writePlatformSupplyError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrProviderSupplyInvalidInput):
		codedError(c, http.StatusBadRequest, ErrorCodeInvalidArgument, "Platform 冲突查询参数无效")
	case errors.Is(err, service.ErrManagementPermissionDenied):
		codedError(c, http.StatusForbidden, ErrorCodeManagementPermission, "Platform 资源权限已失效")
	default:
		codedError(c, http.StatusInternalServerError, ErrorCodePlatformSupplyQueryFailed, "查询 Platform 供给冲突失败")
	}
}
