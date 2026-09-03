package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/Luqueee/kivgraph/internal/config"
)

type profileOptions struct {
	ConfigPath string
	Yes        bool
}

func profileFlagSet(command string, options *profileOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("profile "+command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.ConfigPath, "config", "", "configuration file")
	if command == "remove" {
		flags.BoolVar(&options.Yes, "yes", false, "remove the profile and its indexed state without prompting")
	}
	return flags
}

func runProfile(command string, args []string, stdout, stderr io.Writer) int {
	var options profileOptions
	flags := profileFlagSet(command, &options)
	if parsed, code := parseCommandFlags("profile "+command, flags, args, stdout, stderr); !parsed {
		return code
	}
	if command == "list" {
		if flags.NArg() != 0 {
			writeCommandError(stderr, "profile list: unexpected arguments: %v", flags.Args())
			return 2
		}
		profiles, err := config.ListProfiles(options.ConfigPath)
		if err != nil {
			writeCommandError(stderr, "profile list: %v", err)
			return 1
		}
		for _, profile := range profiles {
			marker := " "
			if profile.Default {
				marker = "*"
			}
			fmt.Fprintf(stdout, "%s %s\n", marker, profile.Name)
		}
		return 0
	}
	if flags.NArg() != 1 {
		writeCommandError(stderr, "profile %s: expected one profile name", command)
		return 2
	}
	name := flags.Arg(0)
	var err error
	switch command {
	case "create":
		err = config.CreateProfile(options.ConfigPath, name)
	case "use":
		err = config.UseProfile(options.ConfigPath, name)
	case "remove":
		if !options.Yes {
			writeCommandError(stderr, "profile remove: refusing to remove %q without --yes", name)
			return 2
		}
		err = config.RemoveProfile(options.ConfigPath, name)
	default:
		writeCommandError(stderr, "profile: unknown operation %q", command)
		return 2
	}
	if err != nil {
		writeCommandError(stderr, "profile %s: %v", command, err)
		return 1
	}
	switch command {
	case "create":
		writeSuccess(stdout, "profile created: %s", name)
	case "use":
		writeSuccess(stdout, "default profile: %s", name)
	case "remove":
		writeSuccess(stdout, "profile removed permanently: %s", name)
	}
	return 0
}
