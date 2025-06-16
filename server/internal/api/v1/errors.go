package v1

import (
	"errors"

	goa "goa.design/goa/v3/pkg"

	api "github.com/pgEdge/control-plane/api/v1/gen/control_plane"
	"github.com/pgEdge/control-plane/server/internal/database"
	"github.com/pgEdge/control-plane/server/internal/etcd"
	"github.com/pgEdge/control-plane/server/internal/task"
	"github.com/pgEdge/control-plane/server/internal/workflows"
)

const (
	errClusterAlreadyInitialized  = "cluster_already_initialized"
	errClusterNotInitialized      = "cluster_not_initialized"
	errDatabaseNotModifiable      = "database_not_modifiable"
	errInvalidInput               = "invalid_input"
	errInvalidJoinToken           = "invalid_join_token"
	errNotFound                   = "not_found"
	errOperationAlreadyInProgress = "operation_already_in_progress"
	errServerError                = "server_error"
	errDatabaseAlreadyExists      = "database_already_exists"
)

var (
	ErrNotImplemented             = newAPIError(errServerError, "This endpoint not yet implemented.")
	ErrAlreadyInitialized         = newAPIError(errClusterAlreadyInitialized, "This operation is invalid on an initialized cluster.")
	ErrUninitialized              = newAPIError(errClusterNotInitialized, "This operation is invalid on an uninitialized cluster.")
	ErrInvalidHostID              = newAPIError(errInvalidInput, "The given host ID is invalid.")
	ErrInvalidDatabaseID          = newAPIError(errInvalidInput, "The given database ID is invalid.")
	ErrInvalidTaskID              = newAPIError(errInvalidInput, "The given task ID is invalid.")
	ErrInvalidServerURL           = newAPIError(errInvalidInput, "The given server URL is invalid.")
	ErrDatabaseNotModifiable      = newAPIError(errDatabaseNotModifiable, "The target database is not modifiable in its current state.")
	ErrOperationAlreadyInProgress = newAPIError(errOperationAlreadyInProgress, "An operation is already in progress for the given entity.")
	ErrDatabaseNotFound           = newAPIError(errNotFound, "No database found with the given ID.")
	ErrTaskNotFound               = newAPIError(errNotFound, "No task found with the given ID.")
	ErrInvalidJoinToken           = newAPIError(errInvalidJoinToken, "The given join token is invalid.")
	ErrDatabaseAlreadyExists      = newAPIError(errDatabaseAlreadyExists, "A database already exists with the given ID.")
)

func apiErr(err error) error {
	var goaErr *goa.ServiceError
	var apiErr *api.APIError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &goaErr), errors.As(err, &apiErr):
		return err
	case errors.Is(err, database.ErrDatabaseNotFound):
		return newAPIError(errNotFound, err.Error())
	case errors.Is(err, task.ErrTaskNotFound):
		return ErrTaskNotFound
	case errors.Is(err, database.ErrDatabaseNotModifiable):
		return ErrDatabaseNotModifiable
	case errors.Is(err, database.ErrNodeNotInDBSpec):
		return makeInvalidInputErr(err)
	case errors.Is(err, etcd.ErrInvalidJoinToken):
		return ErrInvalidJoinToken
	case errors.Is(err, workflows.ErrDuplicateWorkflow):
		return ErrOperationAlreadyInProgress
	case errors.Is(err, database.ErrDatabaseAlreadyExists):
		return ErrDatabaseAlreadyExists
	default:
		return newAPIError(errServerError, err.Error())
	}
}

func makeInvalidInputErr(err error) error {
	return newAPIError(errInvalidInput, err.Error())
}

func newAPIError(name, message string) *api.APIError {
	return &api.APIError{
		Name:    name,
		Message: message,
	}
}
