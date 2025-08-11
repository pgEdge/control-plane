package client

import (
	"errors"
	"fmt"

	goa "goa.design/goa/v3/pkg"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
)

var (
	ErrClusterAlreadyInitialized  = errors.New("cluster_already_initialized")
	ErrClusterNotInitialized      = errors.New("cluster_not_initialized")
	ErrDatabaseAlreadyExists      = errors.New("database_already_exists")
	ErrDatabaseNotModifiable      = errors.New("database_not_modifiable")
	ErrInvalidInput               = errors.New("invalid_input")
	ErrInvalidJoinToken           = errors.New("invalid_join_token")
	ErrNotFound                   = errors.New("not_found")
	ErrOperationAlreadyInProgress = errors.New("operation_already_in_progress")
	ErrRestartFailed              = errors.New("restart_failed")
	ErrServerError                = errors.New("server_error")
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
	errorNameRestartFailed              = "restart_failed"
	errorNameServerError                = "server_error"
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
