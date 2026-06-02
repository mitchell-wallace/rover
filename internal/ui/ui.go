// Package ui wraps huh prompts with a non-interactive fallback so the same
// service code works whether driven from the interactive menu or from flags.
package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// Interactive reports whether stdin is a terminal we can prompt on.
func Interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// Confirm asks a yes/no question. In non-interactive contexts it returns
// defaultVal without prompting.
func Confirm(title, description string, defaultVal bool) (bool, error) {
	if !Interactive() {
		return defaultVal, nil
	}
	v := defaultVal
	err := huh.NewConfirm().
		Title(title).
		Description(description).
		Affirmative("Yes").
		Negative("No").
		Value(&v).
		Run()
	if err != nil {
		return false, err
	}
	return v, nil
}

// Select presents a single-choice menu and returns the chosen value.
func Select[T comparable](title string, options []huh.Option[T]) (T, error) {
	var choice T
	err := huh.NewSelect[T]().
		Title(title).
		Options(options...).
		Value(&choice).
		Run()
	return choice, err
}

// Warn prints a highlighted warning to stderr.
func Warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\033[1;33m[warn]\033[0m "+format+"\n", args...)
}

// Info prints an informational line to stderr.
func Info(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "\033[1;34m==>\033[0m "+format+"\n", args...)
}
