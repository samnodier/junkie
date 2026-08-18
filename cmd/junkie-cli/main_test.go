package main

import (
	"reflect"
	"testing"
)

func TestHasFlag(t *testing.T) {
	rest, ok := hasFlag([]string{"25", "--watch"}, "watch")
	if !ok || !reflect.DeepEqual(rest, []string{"25"}) {
		t.Errorf("rest = %v, ok = %v", rest, ok)
	}
	rest, ok = hasFlag([]string{"25"}, "watch")
	if ok || !reflect.DeepEqual(rest, []string{"25"}) {
		t.Errorf("absent flag: rest = %v, ok = %v", rest, ok)
	}
}

func TestFlagValue(t *testing.T) {
	rest, v := flagValue([]string{"--url", "http://localhost:8080", "sam"}, "url")
	if v != "http://localhost:8080" || !reflect.DeepEqual(rest, []string{"sam"}) {
		t.Errorf("space form: rest = %v, value = %q", rest, v)
	}
	rest, v = flagValue([]string{"--url=http://localhost:8080", "sam"}, "url")
	if v != "http://localhost:8080" || !reflect.DeepEqual(rest, []string{"sam"}) {
		t.Errorf("equals form: rest = %v, value = %q", rest, v)
	}
	rest, v = flagValue([]string{"sam"}, "url")
	if v != "" || !reflect.DeepEqual(rest, []string{"sam"}) {
		t.Errorf("absent: rest = %v, value = %q", rest, v)
	}
}

func TestUnknownCommandIsAnError(t *testing.T) {
	if err := run([]string{"frobnicate"}); err == nil {
		t.Error("expected an error for an unknown command")
	}
}
