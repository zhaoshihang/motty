//go:build !windows

package main

func explorerMenuCommand(args []string) (bool, error) { return false, nil }
