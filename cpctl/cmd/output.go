package cmd

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	controlplane "github.com/pgEdge/control-plane/api/apiv1/gen/control_plane"
	"github.com/pgEdge/control-plane/common/output"
	"github.com/pgEdge/control-plane/cpctl/config"
)

func writeOutput(out io.Writer, outputType config.OutputType, data any) error {
	switch outputType {
	case config.OutputTypeJSON:
		formatter := output.NewJSONFormatter(data)
		return formatter.Write(out)
	case config.OutputTypeTable:
		formatter, err := tableFormatter(data)
		if err != nil {
			return err
		}
		return formatter.Write(out)
	default:
		return fmt.Errorf("unrecognized output type '%s'", outputType)
	}
}

func tableFormatter(data any) (*output.TableFormatter, error) {
	switch d := data.(type) {
	case *controlplane.ListDatabasesResponse:
		return instancesTableFormatter(d), nil
	default:
		return nil, fmt.Errorf("no table formatter found for type %T", d)
	}
}

func instancesTableFormatter(resp *controlplane.ListDatabasesResponse) *output.TableFormatter {
	headers := []string{
		"id",
		"state",
		"node",
		"role",
		"addresses",
		"port",
		"postgres version",
		"spock version",
	}
	// rows := make([][]string, len(resp.Databases))
	var rows [][]string
	for _, db := range resp.Databases {
		for _, inst := range db.Instances {
			connectionInfo := fromPointer(inst.ConnectionInfo)
			postgres := fromPointer(inst.Postgres)
			spock := fromPointer(inst.Spock)
			var port string
			if connectionInfo.Port != nil {
				port = strconv.Itoa(*connectionInfo.Port)
			}
			rows = append(rows, []string{
				inst.ID,
				inst.State,
				inst.NodeName,
				fromPointer(postgres.Role),
				strings.Join(connectionInfo.Addresses, ", "),
				port,
				fromPointer(postgres.Version),
				fromPointer(spock.Version),
			})
		}
	}
	return output.NewTableFormatter(headers, rows...)
}

func fromPointer[T any](v *T) T {
	var zero T
	if v == nil {
		return zero
	}
	return *v
}
