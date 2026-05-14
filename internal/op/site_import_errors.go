package op

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/apperror"
)

const (
	CodeSiteImportInvalidJSON           = "site.import.invalid_json"
	CodeSiteImportEmptyPayload          = "site.import.empty_payload"
	CodeSiteImportUnrecognizedAllAPIHub = "site.import.unrecognized_all_api_hub"
	CodeSiteImportUnrecognizedMetapi    = "site.import.unrecognized_metapi"
	CodeSiteImportNoImportableAllAPIHub = "site.import.no_importable_all_api_hub"
	CodeSiteImportNoImportableMetapi    = "site.import.no_importable_metapi"
	CodeSiteImportUnsupportedPayload    = "site.import.unsupported_payload"
	CodeSiteImportPersistFailed         = "site.import.persist_failed"
)

func newSiteImportInvalidJSONError() *apperror.Error {
	return apperror.New(CodeSiteImportInvalidJSON, "site import invalid json").WithStatus(http.StatusBadRequest)
}

func newSiteImportEmptyPayloadError() *apperror.Error {
	return apperror.New(CodeSiteImportEmptyPayload, "site import empty payload").WithStatus(http.StatusBadRequest)
}

func newSiteImportUnrecognizedAllAPIHubError() *apperror.Error {
	return apperror.New(CodeSiteImportUnrecognizedAllAPIHub, "site import no recognizable all api hub payload sections").WithStatus(http.StatusBadRequest)
}

func newSiteImportUnrecognizedMetapiError() *apperror.Error {
	return apperror.New(CodeSiteImportUnrecognizedMetapi, "site import no recognizable metapi accounts section").WithStatus(http.StatusBadRequest)
}

func newSiteImportNoImportableAllAPIHubError() *apperror.Error {
	return apperror.New(CodeSiteImportNoImportableAllAPIHub, "site import no importable all api hub site account data").WithStatus(http.StatusBadRequest)
}

func newSiteImportNoImportableMetapiError() *apperror.Error {
	return apperror.New(CodeSiteImportNoImportableMetapi, "site import no importable metapi site account data").WithStatus(http.StatusBadRequest)
}

func newSiteImportUnsupportedPayloadError(message string) *apperror.Error {
	if message == "" {
		message = "site import unsupported payload"
	}
	return apperror.New(CodeSiteImportUnsupportedPayload, message).WithStatus(http.StatusBadRequest)
}

func wrapSiteImportPersistFailedError(err error) *apperror.Error {
	return apperror.Wrap(CodeSiteImportPersistFailed, "site import persist failed", err).WithStatus(http.StatusInternalServerError)
}
