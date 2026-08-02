package op

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/apperror"
)

const CodeCatalogProvisionBadRequest = "catalog.provision.bad_request"

func newCatalogProvisionBadRequestError(message string) *apperror.Error {
	return apperror.New(CodeCatalogProvisionBadRequest, message).WithStatus(http.StatusBadRequest)
}
