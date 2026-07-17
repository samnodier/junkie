package main

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func strOpt(name, val string) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{Name: name, Type: discordgo.ApplicationCommandOptionString, Value: val}
}

func boolOpt(name string, val bool) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{Name: name, Type: discordgo.ApplicationCommandOptionBoolean, Value: val}
}

// The whole point of the partial-update design: an option the user didn't
// supply must leave that setting untouched. This is the footgun guard —
// flipping check-in must not silently reset the timer or auto-breaks.
func TestMergeRoomConfigLeavesOmittedSettingsAlone(t *testing.T) {
	rm := room{FocusMinutes: 50, BreakMinutes: 10, AutoSessions: 5, RequireCheckin: false, AutoRoll: true}

	cfg, _, ok := mergeRoomConfig(rm, map[string]*discordgo.ApplicationCommandInteractionDataOption{
		"checkin": boolOpt("checkin", true),
	})
	if !ok {
		t.Fatal("merge rejected a valid checkin-only change")
	}
	if cfg.Focus != 50 || cfg.Break != 10 || cfg.Sessions != 5 {
		t.Errorf("timer changed by a checkin-only edit: %+v", cfg)
	}
	if cfg.AutoRoll != true {
		t.Error("auto-breaks changed by a checkin-only edit")
	}
	if cfg.RequireCheckin != true {
		t.Error("checkin was not applied")
	}
}

func TestMergeRoomConfigAppliesTimerAndTogglesTogether(t *testing.T) {
	rm := room{FocusMinutes: 25, BreakMinutes: 5, AutoSessions: 4, RequireCheckin: false, AutoRoll: true}
	cfg, _, ok := mergeRoomConfig(rm, map[string]*discordgo.ApplicationCommandInteractionDataOption{
		"timer":       strOpt("timer", "50/10/6"),
		"checkin":     boolOpt("checkin", true),
		"auto-breaks": boolOpt("auto-breaks", false),
	})
	if !ok {
		t.Fatal("merge rejected a valid full change")
	}
	if cfg.Focus != 50 || cfg.Break != 10 || cfg.Sessions != 6 {
		t.Errorf("timer not applied: %+v", cfg)
	}
	if !cfg.RequireCheckin || cfg.AutoRoll {
		t.Errorf("toggles not applied: %+v", cfg)
	}
}

func TestMergeRoomConfigClampsAndRejectsBadTimer(t *testing.T) {
	rm := room{FocusMinutes: 25, BreakMinutes: 5, AutoSessions: 4}
	// Out-of-range values clamp to the allowed bounds rather than erroring.
	cfg, _, ok := mergeRoomConfig(rm, map[string]*discordgo.ApplicationCommandInteractionDataOption{
		"timer": strOpt("timer", "999/0/99"),
	})
	if !ok {
		t.Fatal("clampable timer was rejected")
	}
	if cfg.Focus != 180 || cfg.Break != 1 || cfg.Sessions != 12 {
		t.Errorf("timer not clamped to bounds: %+v", cfg)
	}
	// A shorthand that isn't three parts is a hard error, and nothing changes.
	if _, _, ok := mergeRoomConfig(rm, map[string]*discordgo.ApplicationCommandInteractionDataOption{
		"timer": strOpt("timer", "30/5"),
	}); ok {
		t.Error("malformed timer shorthand was accepted")
	}
}
