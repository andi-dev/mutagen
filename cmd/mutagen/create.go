package main

import (
	"context"
	"fmt"

	"github.com/pkg/errors"

	"github.com/spf13/cobra"

	"github.com/havoc-io/mutagen/pkg/configuration"
	"github.com/havoc-io/mutagen/pkg/filesystem"
	promptpkg "github.com/havoc-io/mutagen/pkg/prompt"
	sessionsvcpkg "github.com/havoc-io/mutagen/pkg/session/service"
	"github.com/havoc-io/mutagen/pkg/url"
)

func createMain(command *cobra.Command, arguments []string) error {
	// Load the Mutagen configuration.
	configuration, err := configuration.Load()
	if err != nil {
		return errors.Wrap(err, "unable to load configuration file")
	}

	// Validate, extract, and parse URLs.
	if len(arguments) != 2 {
		return errors.New("invalid number of endpoint URLs provided")
	}
	alpha, err := url.Parse(arguments[0])
	if err != nil {
		return errors.Wrap(err, "unable to parse alpha URL")
	}
	beta, err := url.Parse(arguments[1])
	if err != nil {
		return errors.Wrap(err, "unable to parse beta URL")
	}

	// If either URL is a local path, make sure it's normalized.
	if alpha.Protocol == url.Protocol_Local {
		if alphaPath, err := filesystem.Normalize(alpha.Path); err != nil {
			return errors.Wrap(err, "unable to normalize alpha path")
		} else {
			alpha.Path = alphaPath
		}
	}
	if beta.Protocol == url.Protocol_Local {
		if betaPath, err := filesystem.Normalize(beta.Path); err != nil {
			return errors.Wrap(err, "unable to normalize beta path")
		} else {
			beta.Path = betaPath
		}
	}

	// Compute ignores by appending those specified on the command line to the
	// defaults specified in the user's configuration.
	var ignores []string
	ignores = append(ignores, configuration.Ignore.Default...)
	ignores = append(ignores, createConfiguration.ignores...)

	// Connect to the daemon and defer closure of the connection.
	daemonConnection, err := createDaemonClientConnection()
	if err != nil {
		return errors.Wrap(err, "unable to connect to daemon")
	}
	defer daemonConnection.Close()

	// Create a session service client.
	sessionService := sessionsvcpkg.NewSessionClient(daemonConnection)

	// Invoke the session create method. The stream will close when the
	// associated context is cancelled.
	createContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := sessionService.Create(createContext)
	if err != nil {
		return errors.Wrap(err, "unable to invoke create")
	}

	// Send the initial request.
	request := &sessionsvcpkg.CreateRequest{
		Alpha:   alpha,
		Beta:    beta,
		Ignores: ignores,
	}
	if err := stream.Send(request); err != nil {
		return errors.Wrap(err, "unable to send create request")
	}

	// Receive and process responses until we're done.
	for {
		// Receive the next response, watching for completion or another prompt.
		var prompt *promptpkg.Prompt
		if response, err := stream.Recv(); err != nil {
			return errors.Wrap(err, "unable to receive response")
		} else if response.Session != "" {
			if response.Prompt != nil {
				return errors.New("invalid create response received (session with prompt)")
			}
			fmt.Println("Created session", response.Session)
			return nil
		} else if response.Prompt == nil {
			return errors.New("invalid create response received (empty)")
		} else {
			prompt = response.Prompt
		}

		// Process the prompt.
		if response, err := promptpkg.PromptCommandLine(prompt.Message, prompt.Prompt); err != nil {
			return errors.Wrap(err, "unable to perform prompting")
		} else if err = stream.Send(&sessionsvcpkg.CreateRequest{Response: response}); err != nil {
			return errors.Wrap(err, "unable to send prompt response")
		}
	}
}

var createCommand = &cobra.Command{
	Use:   "create <alpha> <beta>",
	Short: "Creates and starts a new synchronization session",
	Run:   mainify(createMain),
}

var createConfiguration struct {
	help    bool
	ignores []string
}

func init() {
	// Bind flags to configuration. We manually add help to override the default
	// message, but Cobra still implements it automatically.
	flags := createCommand.Flags()
	flags.BoolVarP(&createConfiguration.help, "help", "h", false, "Show help information")
	flags.StringSliceVarP(&createConfiguration.ignores, "ignore", "i", nil, "Specify ignore paths")
}
