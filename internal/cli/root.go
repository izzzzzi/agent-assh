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
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return writeInvalidArgs(cmd, "unknown command "+args[0], "run assh-go --help")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeInvalidArgs(cmd, "command required", "run assh-go --help")
		},
	}
	cmd.PersistentFlags().Bool("json", true, "emit JSON output")
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return writeInvalidArgs(cmd, err.Error(), "run assh-go --help")
	})
	cmd.AddCommand(newExecCommand(), newReadCommand(), newCapabilitiesCommand(), newSessionCommand())
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

func writeJSON(cmd *cobra.Command, v any) error {
	body, err := response.Marshal(v)
	if err != nil {
		return err
	}
	_, _ = cmd.OutOrStdout().Write(body)
	return nil
}

func writeError(cmd *cobra.Command, code, message, hint string) error {
	body, marshalErr := response.MarshalError(code, message, hint)
	if marshalErr != nil {
		return marshalErr
	}
	_, _ = cmd.ErrOrStderr().Write(body)
	return errors.New(message)
}
