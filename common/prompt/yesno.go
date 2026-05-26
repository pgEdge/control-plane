package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	FlagNameAssumeYes = "assumeyes"
	FlagNameAssumeNo  = "assumeno"
)

func YesNo(question string, defaultChoice bool) (bool, error) {
	choices := "Y/n, defaults to 'y' if blank"
	if !defaultChoice {
		choices = "y/N, defaults to 'n' if blank"
	}
	rdr := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprintf(os.Stderr, "%s (%s) ", question, choices)
		answer, err := rdr.ReadString('\n')
		if err != nil {
			return false, fmt.Errorf("failed to read answer: %w", err)
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		switch answer {
		case "":
			return defaultChoice, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			return false, fmt.Errorf("unrecognized answer '%s'", answer)
		}
	}
}

func AddYesNoFlags(cmd *cobra.Command) {
	cmd.Flags().BoolP(FlagNameAssumeYes, "y", false, "Automatically answer yes for all questions")
	cmd.Flags().Bool(FlagNameAssumeNo, false, "Automatically answer no for all questions")
}

func DefaultFromYesNoFlags(flags *pflag.FlagSet) (*bool, error) {
	flagValue := func(flagName string) (*bool, error) {
		def, err := flags.GetBool(flagName)
		if err != nil {
			return nil, fmt.Errorf("failed to get '%s' flag value: %w", flagName, err)
		}
		return &def, nil
	}
	switch {
	case flags.Changed(FlagNameAssumeYes):
		return flagValue(FlagNameAssumeYes)
	case flags.Changed(FlagNameAssumeNo):
		return flagValue(FlagNameAssumeNo)
	default:
		return nil, nil
	}
}
