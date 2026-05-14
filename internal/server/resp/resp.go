package resp

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/gin-gonic/gin"
)

type ResponseStruct struct {
	Code      int            `json:"code" example:"200"`
	ErrorCode string         `json:"error_code,omitempty" example:"site.sub2api.api_key_required"`
	Message   string         `json:"message" example:"success"`
	Params    map[string]any `json:"params,omitempty"`
	Data      interface{}    `json:"data,omitempty"`
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

func ErrorWithAppError(c *gin.Context, fallbackStatus int, err error) {
	status := fallbackStatus
	if appStatus := apperror.Status(err); appStatus != 0 {
		status = appStatus
	}
	ErrorWithCodeAndParams(c, status, apperror.Code(err), apperror.Message(err), apperror.Params(err))
}

func ErrorWithCode(c *gin.Context, status int, errorCode string, message string) {
	ErrorWithCodeAndParams(c, status, errorCode, message, nil)
}

func ErrorWithCodeAndParams(c *gin.Context, status int, errorCode string, message string, params map[string]any) {
	c.AbortWithStatusJSON(status, ResponseStruct{
		Code:      status,
		ErrorCode: errorCode,
		Message:   message,
		Params:    params,
	})
}

func InvalidJSON(c *gin.Context) {
	ErrorWithAppError(c, http.StatusBadRequest, apperror.InvalidJSON(ErrInvalidJSON))
}

func InvalidParam(c *gin.Context) {
	ErrorWithAppError(c, http.StatusBadRequest, apperror.InvalidParam(ErrInvalidParam))
}

func InternalError(c *gin.Context) {
	ErrorWithAppError(c, http.StatusInternalServerError, apperror.New(apperror.CodeCommonInternalError, ErrInternalServer).WithStatus(http.StatusInternalServerError))
}

func DatabaseError(c *gin.Context) {
	ErrorWithAppError(c, http.StatusInternalServerError, apperror.New(apperror.CodeCommonDatabaseError, ErrDatabase).WithStatus(http.StatusInternalServerError))
}

func NotFound(c *gin.Context) {
	ErrorWithAppError(c, http.StatusNotFound, apperror.New(apperror.CodeCommonNotFound, ErrResourceNotFound).WithStatus(http.StatusNotFound))
}

func DuplicateResource(c *gin.Context) {
	ErrorWithAppError(c, http.StatusConflict, apperror.New(apperror.CodeCommonDuplicateResource, ErrDuplicateResource).WithStatus(http.StatusConflict))
}

func Unauthorized(c *gin.Context) {
	ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthUnauthorized, ErrUnauthorized).WithStatus(http.StatusUnauthorized))
}

func InvalidToken(c *gin.Context) {
	ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthInvalidToken, ErrUnauthorized).WithStatus(http.StatusUnauthorized))
}

func InvalidCredentials(c *gin.Context) {
	ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthInvalidCredentials, ErrUnauthorized).WithStatus(http.StatusUnauthorized))
}

func APIKeyMissing(c *gin.Context) {
	ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthAPIKeyMissing, "API key is missing").WithStatus(http.StatusUnauthorized))
}

func APIKeyExpired(c *gin.Context) {
	ErrorWithAppError(c, http.StatusUnauthorized, apperror.New(apperror.CodeAuthAPIKeyExpired, "API key has expired").WithStatus(http.StatusUnauthorized))
}
