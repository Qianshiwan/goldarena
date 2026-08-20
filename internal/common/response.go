package common

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/goldarena/goldarena/pkg/errs"
)

type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type PageResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
	Total   int64       `json:"total"`
	Page    int         `json:"page"`
	Size    int         `json:"size"`
}

func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{Code: 0, Message: "success", Data: data})
}

func SuccessPage(c *gin.Context, data interface{}, total int64, page, size int) {
	c.JSON(http.StatusOK, PageResponse{
		Code: 0, Message: "success",
		Data: data, Total: total, Page: page, Size: size,
	})
}

func Error(c *gin.Context, code errs.Code, detail string) {
	e := errs.New(code, detail)
	c.JSON(statusForCode(code), Response{Code: int(e.Code), Message: e.Message + ": " + detail})
}

// statusForCode maps a business error code to the proper HTTP status so that
// clients (and axios error interceptors) can react to failures correctly.
func statusForCode(code errs.Code) int {
	switch code {
	case errs.TooManyRequests:
		return http.StatusTooManyRequests
	case errs.Unauthorized, errs.InvalidToken, errs.TokenExpired:
		return http.StatusUnauthorized
	case errs.Forbidden:
		return http.StatusForbidden
	case errs.NotFound:
		return http.StatusNotFound
	case errs.Internal, errs.Unknown:
		return http.StatusInternalServerError
	default:
		// InvalidParam and all domain-specific 4xx-class errors
		return http.StatusBadRequest
	}
}

func ErrorRaw(c *gin.Context, e *errs.Error) {
	c.JSON(http.StatusOK, Response{Code: int(e.Code), Message: e.Message})
}

// BindJSON helper
func BindJSON(c *gin.Context, obj interface{}) error {
	if err := c.ShouldBindJSON(obj); err != nil {
		Error(c, errs.InvalidParam, err.Error())
		return err
	}
	return nil
}
