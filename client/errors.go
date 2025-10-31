package client

import (
	"errors"
	"fmt"

	goa "goa.design/goa/v3/pkg"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

const (
	errorNameClusterAlreadyInitialized  = "cluster_already_initialized"
	errorNameClusterNotInitialized      = "cluster_not_initialized"
	errorNameDatabaseAlreadyExists      = "database_already_exists"
	errorNameDatabaseNotModifiable      = "database_not_modifiable"
	errorNameInvalidInput               = "invalid_input"
	errorNameInvalidJoinToken           = "invalid_join_token"
	errorNameNotFound                   = "not_found"
	errorNameOperationAlreadyInProgress = "operation_already_in_progress"
	errorNameOperationNotSupported      = "operation_not_supported"
	errorNameRestartFailed              = "restart_failed"
	errorNameServerError                = "server_error"
)

var (
	ErrClusterAlreadyInitialized  = errors.New(errorNameClusterAlreadyInitialized)
	ErrClusterNotInitialized      = errors.New(errorNameClusterNotInitialized)
	ErrDatabaseAlreadyExists      = errors.New(errorNameDatabaseAlreadyExists)
	ErrDatabaseNotModifiable      = errors.New(errorNameDatabaseNotModifiable)
	ErrInvalidInput               = errors.New(errorNameInvalidInput)
	ErrInvalidJoinToken           = errors.New(errorNameInvalidJoinToken)
	ErrNotFound                   = errors.New(errorNameNotFound)
	ErrOperationAlreadyInProgress = errors.New(errorNameOperationAlreadyInProgress)
	ErrOperationNotSupported      = errors.New(errorNameOperationNotSupported)
	ErrRestartFailed              = errors.New(errorNameRestartFailed)
	ErrServerError                = errors.New(errorNameServerError)
)

func errorByName(name string) error {
	switch name {
	case errorNameClusterAlreadyInitialized:
		return ErrClusterAlreadyInitialized
	case errorNameClusterNotInitialized:
		return ErrClusterNotInitialized
	case errorNameDatabaseAlreadyExists:
		return ErrDatabaseAlreadyExists
	case errorNameDatabaseNotModifiable:
		return ErrDatabaseNotModifiable
	case errorNameInvalidInput:
		return ErrInvalidInput
	case errorNameInvalidJoinToken:
		return ErrInvalidJoinToken
	case errorNameNotFound:
		return ErrNotFound
	case errorNameOperationAlreadyInProgress:
		return ErrOperationAlreadyInProgress
	case errorNameOperationNotSupported:
		return ErrOperationNotSupported
	case errorNameRestartFailed:
		return ErrRestartFailed
	case errorNameServerError:
		return ErrServerError
	default:
		return nil
	}
}

func translateErr(err error) error {
	if err == nil {
		return nil
	}

	var apiErr *api.APIError
	var goaErr *goa.ServiceError
	switch {
	case errors.As(err, &apiErr):
		if known := errorByName(apiErr.Name); known != nil {
			return fmt.Errorf("%w: %s", known, apiErr.Message)
		} else {
			return fmt.Errorf("%s: %s", apiErr.Name, apiErr.Message)
		}
	case errors.As(err, &goaErr):
		if known := errorByName(goaErr.GoaErrorName()); known != nil {
			return fmt.Errorf("%w: %s", known, goaErr.Message)
		} else {
			return fmt.Errorf("%s: %s", goaErr.GoaErrorName(), goaErr.Message)
		}
	default:
		return err
	}
}
