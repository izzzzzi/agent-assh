package cli

import (
	"errors"

	"github.com/agent-ssh/assh/internal/response"
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "assh-go",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeInvalidArgs(cmd, "command required", "run assh-go --help")
		},
	}
	cmd.PersistentFlags().Bool("json", true, "emit JSON output")
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return writeInvalidArgs(cmd, err.Error(), "run assh-go --help")
	})
	return cmd
}

func Execute() error {
	return NewRootCommand().Execute()
}

func writeInvalidArgs(cmd *cobra.Command, message, hint string) error {
	body, marshalErr := response.MarshalError("invalid_args", message, hint)
	if marshalErr != nil {
		return marshalErr
	}
	_, _ = cmd.ErrOrStderr().Write(body)
	return errors.New(message)
}
