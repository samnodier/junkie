package main

import (
	"bytes"
	"strings"
	"testing"
)

// The overview and the per-command help are generated from one table, so a
// command that exists must be findable, documented, and reachable by every
// name it answers to.
func TestEveryCommandIsDocumentedAndReachable(t *testing.T) {
	var overview bytes.Buffer
	writeOverview(&overview)

	seen := map[string]string{}
	for _, c := range commands() {
		if c.summary == "" {
			t.Errorf("%s has no summary, so the overview has nothing to say about it", c.name)
		}
		if c.detail == "" {
			t.Errorf("%s has no detail, so `junkie help %s` explains nothing", c.name, c.name)
		}
		if c.run == nil {
			t.Errorf("%s has nothing to run", c.name)
		}
		if c.group == "" {
			t.Errorf("%s is in no group, so the overview will not list it", c.name)
		}
		if !strings.Contains(overview.String(), c.name) {
			t.Errorf("%s is missing from the overview", c.name)
		}
		for _, name := range append([]string{c.name}, c.aliases...) {
			if prior, dup := seen[name]; dup {
				t.Errorf("%q is claimed by both %s and %s", name, prior, c.name)
			}
			seen[name] = c.name
			if _, ok := find(name); !ok {
				t.Errorf("%q does not resolve", name)
			}
		}
	}
	// Every group named in the order is real, and every group used is named.
	for _, c := range commands() {
		found := false
		for _, g := range groupOrder {
			if g == c.group {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is in group %q, which the overview never prints", c.name, c.group)
		}
	}
}

func TestHelpForOneCommand(t *testing.T) {
	var out bytes.Buffer
	writeHelp(&out, []string{"room"})
	got := out.String()
	for _, want := range []string{"junkie room <command>", "create, join and run shared rooms",
		"junkie room join CODE", "junkie room start"} {
		if !strings.Contains(got, want) {
			t.Errorf("help missing %q:\n%s", want, got)
		}
	}
}

// --json is documented on the commands that take it, and only those.
func TestJSONFlagIsDocumentedWhereItWorks(t *testing.T) {
	var withFlag bytes.Buffer
	writeHelp(&withFlag, []string{"status"})
	if !strings.Contains(withFlag.String(), "--json") {
		t.Error("status takes --json but its help does not say so")
	}
	var without bytes.Buffer
	writeHelp(&without, []string{"cancel"})
	if strings.Contains(without.String(), "--json") {
		t.Error("cancel does not take --json but its help offers it")
	}
}

// An unknown topic says so and then shows what does exist, rather than
// silently printing the overview as though the request made sense.
func TestHelpForUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	writeHelp(&out, []string{"frobnicate"})
	got := out.String()
	if !strings.Contains(got, "no command called") {
		t.Errorf("expected an explanation:\n%s", got)
	}
	if !strings.Contains(got, "Account") {
		t.Errorf("expected the overview as well:\n%s", got)
	}
}

// `junkie room --help` should explain rooms, not attempt them.
func TestWantsHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"join", "--help"}} {
		if !wantsHelp(args) {
			t.Errorf("%v should ask for help", args)
		}
	}
	for _, args := range [][]string{nil, {"25"}, {"--watch"}, {"join", "ABC-123"}} {
		if wantsHelp(args) {
			t.Errorf("%v should not ask for help", args)
		}
	}
}

func TestVersionLine(t *testing.T) {
	got := versionLine()
	if !strings.HasPrefix(got, "junkie ") {
		t.Errorf("version line = %q", got)
	}
	// A build from source says so rather than claiming a version it has not
	// been given; a release has its tag substituted in at link time.
	if !strings.Contains(got, version) {
		t.Errorf("version line %q does not carry the version %q", got, version)
	}
	if !strings.Contains(got, "/") {
		t.Errorf("version line should name the platform: %q", got)
	}
}

func TestHelpAndVersionRunWithoutASession(t *testing.T) {
	// These are the two commands that must work before `junkie login` has
	// ever been run, so neither may touch the config or the network.
	t.Setenv("JUNKIE_CONFIG", "/nonexistent/junkie/config.json")
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}, {"version"}, {"--version"}, {"-v"}, {"help", "room"}} {
		if err := run(args); err != nil {
			t.Errorf("junkie %v: %v", args, err)
		}
	}
}
