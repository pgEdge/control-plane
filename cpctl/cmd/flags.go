package cmd

import (
	"fmt"

	"github.com/spf13/pflag"

	"github.com/pgEdge/control-plane/common/ds"
	"github.com/pgEdge/control-plane/cpctl/config"
)

func addOutputFlag(flags *pflag.FlagSet) {
	flags.StringP("output", "o", config.OutputTypeTable.String(), fmt.Sprintf("Set the output format. One of: %s", ds.SetToString(config.OutputTypes())))
}
