package main

import "testing"

// A display name is quoted back into a channel, so it must not be able to
// format the line or mention anyone.
func TestDiscordWaveLineEscapesTheName(t *testing.T) {
	got := discordWaveLine("@everyone **x**_`y`", roomWaves["hi"])
	want := "**@​everyone \\*\\*x\\*\\*\\_\\`y\\`**: 👋 Hi!"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// Every greeting is offered in Discord, in a fixed order, under its own
// wording -- the menu and the vocabulary can't drift apart.
func TestDiscordWaveChoicesCoverTheVocabulary(t *testing.T) {
	choices := discordWaveChoices()
	if len(choices) != len(roomWaves) {
		t.Fatalf("%d choices for %d waves", len(choices), len(roomWaves))
	}
	for i, c := range choices {
		kind, _ := c.Value.(string)
		if kind != discordWaveOrder[i] || c.Name != roomWaves[kind] || c.Name == "" {
			t.Errorf("choice %d = %q/%q", i, kind, c.Name)
		}
	}
}
