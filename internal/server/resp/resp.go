package resp

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/gin-gonic/gin"
)

type ResponseStruct struct {
	Code      int         `json:"code" example:"200"`
	ErrorCode string      `json:"error_code,omitempty" example:"site.sub2api.api_key_required"`
	Message   string      `json:"message" example:"success"`
	Data      interface{} `json:"data,omitempty"`
}

func Success(c *gin.Context, data any) {
	c.JSON(http.StatusOK, ResponseStruct{
		Code:    http.StatusOK,
		Message: "success",
		Data:    data,
	})
}

func Error(c *gin.Context, code int, err string) {
	ErrorWithCode(c, code, "", err)
}

func ErrorWithAppError(c *gin.Context, status int, err error) {
	ErrorWithCode(c, status, apperror.Code(err), apperror.Message(err))
}

func ErrorWithCode(c *gin.Context, status int, errorCode string, message string) {
	c.AbortWithStatusJSON(status, ResponseStruct{
		Code:      status,
		ErrorCode: errorCode,
		Message:   message,
	})
}
