package apiv1

import (
	"errors"

	goa "goa.design/goa/v3/pkg"

	api "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
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
	ErrNotImplemented             = newAPIError(errServerError, "this endpoint is not yet implemented")
	ErrAlreadyInitialized         = newAPIError(errClusterAlreadyInitialized, "this operation is invalid on an initialized cluster")
	ErrUninitialized              = newAPIError(errClusterNotInitialized, "this operation is invalid on an uninitialized cluster")
	ErrInvalidTaskID              = newAPIError(errInvalidInput, "the given task ID is invalid")
	ErrInvalidServerURL           = newAPIError(errInvalidInput, "the given server URL is invalid")
	ErrDatabaseNotModifiable      = newAPIError(errDatabaseNotModifiable, "the target database is not modifiable in its current state")
	ErrOperationAlreadyInProgress = newAPIError(errOperationAlreadyInProgress, "an operation is already in progress for the given entity")
	ErrDatabaseNotFound           = newAPIError(errNotFound, "no database found with the given ID")
	ErrTaskNotFound               = newAPIError(errNotFound, "no task found with the given ID")
	ErrInvalidJoinToken           = newAPIError(errInvalidJoinToken, "the given join token is invalid")
	ErrDatabaseAlreadyExists      = newAPIError(errDatabaseAlreadyExists, "a database already exists with the given ID")
	ErrHostNotFound               = newAPIError(errNotFound, "no host found with the given ID")
)

func apiErr(err error) error {
	var goaErr *goa.ServiceError
	var apiErr *api.APIError
	var vErr *validationError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &goaErr), errors.As(err, &apiErr):
		return err
	case errors.As(err, &vErr):
		return makeInvalidInputErr(err)
	case errors.Is(err, database.ErrDatabaseNotFound):
		return newAPIError(errNotFound, err.Error())
	case errors.Is(err, database.ErrInstanceNotFound):
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
	case errors.Is(err, database.ErrInvalidDatabaseUpdate):
		return makeInvalidInputErr(err)
	case errors.Is(err, etcd.ErrCannotRemoveSelf):
		return makeInvalidInputErr(err)
	case errors.Is(err, etcd.ErrMemberNotFound):
		return makeNotFoundErr(err)
	case errors.Is(err, etcd.ErrMinimumClusterSize):
		return makeInvalidInputErr(err)
	default:
		return newAPIError(errServerError, err.Error())
	}
}

func makeInvalidInputErr(err error) error {
	return newAPIError(errInvalidInput, err.Error())
}

func makeNotFoundErr(err error) error {
	return newAPIError(errNotFound, err.Error())
}

func newAPIError(name, message string) *api.APIError {
	return &api.APIError{
		Name:    name,
		Message: message,
	}
}
