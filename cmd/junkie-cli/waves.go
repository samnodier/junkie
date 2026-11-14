package main

// waveLabels is the wording of a room wave, keyed the way the server relays
// it. The server only ever sends the key (see roomWaves in cmd/junkie), so
// an unknown one is dropped rather than printed: nothing a member typed can
// reach this terminal. Mirrors web/src/lib/waves.js.
var waveLabels = map[string]string{
	"hi":   "👋 Hi!",
	"luck": "🍀 Good luck",
	"go":   "💪 Let's go",
	"nice": "🎉 Nice work",
	"back": "☕ Back soon",
}
