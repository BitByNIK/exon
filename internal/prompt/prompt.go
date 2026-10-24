// Package prompt asks the user questions.
package prompt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/bitbynik/exon/internal/usererr"
)

// Question is one thing to ask. ID must be unique across all steps in a run;
// prefix it with the step name ("apps.adopt.google-chrome").
//
// A question is multiple choice when Options is set, free text when Input is
// set, and yes/no otherwise.
type Question struct {
	ID      string
	Text    string
	Options []string // choices for a multiple-choice question
	Input   bool     // the answer is typed text
	// Default is one of Options, "yes"/"no" for a yes/no question, or the
	// suggested text for an Input question (Enter keeps it).
	Default string
	// Validate checks a typed answer. Its error is shown and the question
	// asked again. Optional; Input questions only.
	Validate func(string) error
}

// Answers maps Question.ID to the chosen option ("yes"/"no" for yes/no questions).
type Answers map[string]string

// Prompter asks questions.
type Prompter interface {
	// Confirm asks a yes/no question.
	Confirm(ctx context.Context, text string, def bool) (bool, error)
	// Ask asks every question and returns the answers.
	Ask(ctx context.Context, qs []Question) (Answers, error)
}

// Terminal asks on a terminal, reading one line per answer.
type Terminal struct {
	in  *bufio.Reader
	out io.Writer
}

// NewTerminal returns a Prompter reading from in and writing to out.
func NewTerminal(in io.Reader, out io.Writer) *Terminal {
	return &Terminal{in: bufio.NewReader(in), out: out}
}

// Confirm asks text and accepts y/yes/n/no; Enter picks def.
func (t *Terminal) Confirm(ctx context.Context, text string, def bool) (bool, error) {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	for {
		fmt.Fprintf(t.out, "%s %s ", text, hint)
		line, err := t.readLine(ctx)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(line) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(t.out, "  Please answer y or n.")
	}
}

// Ask asks each question in order.
func (t *Terminal) Ask(ctx context.Context, qs []Question) (Answers, error) {
	ans := Answers{}
	for _, q := range qs {
		switch {
		case q.Input:
			text, err := t.input(ctx, q)
			if err != nil {
				return nil, err
			}
			ans[q.ID] = text
			continue
		case len(q.Options) == 0:
			ok, err := t.Confirm(ctx, q.Text, q.Default == "yes")
			if err != nil {
				return nil, err
			}
			ans[q.ID] = yesNo(ok)
			continue
		}
		choice, err := t.choose(ctx, q)
		if err != nil {
			return nil, err
		}
		ans[q.ID] = choice
	}
	return ans, nil
}

// input asks for typed text. Enter keeps the default; an empty answer with no
// default, or one Validate rejects, is asked again.
func (t *Terminal) input(ctx context.Context, q Question) (string, error) {
	for {
		if q.Default != "" {
			fmt.Fprintf(t.out, "%s [%s]: ", q.Text, q.Default)
		} else {
			fmt.Fprintf(t.out, "%s: ", q.Text)
		}
		line, err := t.readLine(ctx)
		if err != nil {
			return "", err
		}
		if line == "" {
			line = q.Default
		}
		if line == "" {
			fmt.Fprintln(t.out, "  Please type an answer.")
			continue
		}
		if q.Validate != nil {
			if err := q.Validate(line); err != nil {
				fmt.Fprintf(t.out, "  %v\n", err)
				continue
			}
		}
		return line, nil
	}
}

func (t *Terminal) choose(ctx context.Context, q Question) (string, error) {
	fmt.Fprintln(t.out, q.Text)
	def := max(slices.Index(q.Options, q.Default), 0)
	for i, o := range q.Options {
		marker := " "
		if i == def {
			marker = "*"
		}
		fmt.Fprintf(t.out, "  %s %d) %s\n", marker, i+1, o)
	}
	for {
		fmt.Fprintf(t.out, "Choose 1-%d [%d]: ", len(q.Options), def+1)
		line, err := t.readLine(ctx)
		if err != nil {
			return "", err
		}
		if line == "" {
			return q.Options[def], nil
		}
		if n, err := strconv.Atoi(line); err == nil && n >= 1 && n <= len(q.Options) {
			return q.Options[n-1], nil
		}
		fmt.Fprintf(t.out, "  Please enter a number from 1 to %d.\n", len(q.Options))
	}
}

// readLine reads one trimmed line, returning ctx.Err() if ctx is cancelled first.
func (t *Terminal) readLine(ctx context.Context) (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := t.in.ReadString('\n')
		ch <- result{strings.TrimSpace(line), err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-ch:
		if r.err != nil && (!errors.Is(r.err, io.EOF) || r.line == "") {
			return "", usererr.New("input closed before all questions were answered", "", r.err)
		}
		return r.line, nil
	}
}

// Defaults answers every question with its default, without asking (--yes).
type Defaults struct{}

// Confirm returns true: --yes means "go ahead".
func (Defaults) Confirm(context.Context, string, bool) (bool, error) { return true, nil }

// Ask returns each question's default.
func (Defaults) Ask(_ context.Context, qs []Question) (Answers, error) {
	ans := Answers{}
	for _, q := range qs {
		ans[q.ID] = q.Default
	}
	return ans, nil
}

// Unavailable fails any question: used when there is no terminal and no --yes.
type Unavailable struct{}

var errNoTerminal = usererr.New(
	"exon needs to ask questions but is not running in a terminal",
	"Run it in a terminal, or pass --yes to accept the defaults.",
	nil,
)

// Confirm fails.
func (Unavailable) Confirm(context.Context, string, bool) (bool, error) { return false, errNoTerminal }

// Ask fails if there is anything to ask.
func (Unavailable) Ask(_ context.Context, qs []Question) (Answers, error) {
	if len(qs) == 0 {
		return Answers{}, nil
	}
	return nil, errNoTerminal
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
