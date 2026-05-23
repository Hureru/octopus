package op

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/apperror"
)

const (
	CodeGroupAutoGroupBadRequest = "group.auto_group.bad_request"
	CodeGroupAutoGroupNotFound   = "group.auto_group.not_found"
)

func newGroupAutoGroupBadRequestError(message string) *apperror.Error {
	return apperror.New(CodeGroupAutoGroupBadRequest, message).WithStatus(http.StatusBadRequest)
}

func newGroupAutoGroupNotFoundError(message string) *apperror.Error {
	return apperror.New(CodeGroupAutoGroupNotFound, message).WithStatus(http.StatusNotFound)
}
