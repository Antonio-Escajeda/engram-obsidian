//go:build !linux

package main

import "errors"

// ErrSubcommandUnavailable is returned on non-Linux platforms.
var errSubcommandUnavailable = errors.New("subcommand unavailable on this platform (Linux only)")

func runSetupKeys() error { return errSubcommandUnavailable }
func runRecover() error   { return errSubcommandUnavailable }
