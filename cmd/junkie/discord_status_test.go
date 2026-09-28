package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

type statusTransport func(*http.Request) (*http.Response, error)

func (f statusTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func statusTestSession(t *testing.T, fn statusTransport) *discordgo.Session {
	t.Helper()
	s, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatal(err)
	}
	s.Client = &http.Client{Transport: fn}
	return s
}
func statusHTTPResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func statusInteraction() *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{ID: "interaction", AppID: "app", Token: "token"}}
}

func TestStatusAcknowledgesBeforeDatabase(t *testing.T) {
	calls := 0
	s := statusTestSession(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/callback") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var response discordgo.InteractionResponse
		if err := json.NewDecoder(r.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response.Type != discordgo.InteractionResponseChannelMessageWithSource || response.Data.Flags != discordgo.MessageFlagsEphemeral {
			t.Fatalf("unexpected acknowledgement: %+v", response)
		}
		return statusHTTPResponse(404, `{"message":"Unknown interaction","code":10062}`), nil
	})
	// No app/database: any work before acknowledgement or after its failure panics.
	(&discordBot{}).handleStatus(s, statusInteraction())
	if calls != 1 {
		t.Fatalf("requests = %d, want 1", calls)
	}
}

func TestStatusPublicFollowupAndPrivateErrors(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "publish failure"
		}
		t.Run(name, func(t *testing.T) {
			calls := 0
			s := statusTestSession(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/webhooks/app/token") {
						t.Fatalf("unexpected followup: %s %s", r.Method, r.URL.Path)
					}
					var body struct {
						Content    string            `json:"content"`
						Flags      int               `json:"flags"`
						Components []json.RawMessage `json:"components"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					if body.Content != "status" || body.Flags != 0 || len(body.Components) != 1 {
						t.Fatalf("unexpected public status: %+v", body)
					}
					if fail {
						return statusHTTPResponse(403, `{"message":"Missing Permissions","code":50013}`), nil
					}
					return statusHTTPResponse(200, `{"id":"message"}`), nil
				}
				wantMethod := http.MethodDelete
				if fail {
					wantMethod = http.MethodPatch
				}
				if calls != 2 || r.Method != wantMethod || !strings.HasSuffix(r.URL.Path, "/messages/@original") {
					t.Fatalf("unexpected completion: %s %s", r.Method, r.URL.Path)
				}
				if fail {
					return statusHTTPResponse(200, `{"id":"original"}`), nil
				}
				return statusHTTPResponse(204, ""), nil
			})
			(&discordBot{}).publishStatusReply(s, statusInteraction(), "status", timerComponents(room{}, nil))
			if calls != 2 {
				t.Fatalf("requests = %d, want 2", calls)
			}
		})
	}
}

func TestHelpRepliesWithoutDatabase(t *testing.T) {
	calls := 0
	s := statusTestSession(t, func(r *http.Request) (*http.Response, error) {
		calls++
		var body discordgo.InteractionResponse
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body.Data.Content, "junkie commands") || body.Data.Flags != discordgo.MessageFlagsEphemeral {
			t.Fatalf("unexpected help response: %+v", body)
		}
		return statusHTTPResponse(204, ""), nil
	})
	i := statusInteraction()
	i.Type = discordgo.InteractionApplicationCommand
	i.Data = discordgo.ApplicationCommandInteractionData{Name: "junkie", Options: []*discordgo.ApplicationCommandInteractionDataOption{{Name: "help", Type: discordgo.ApplicationCommandOptionSubCommand}}}
	(&discordBot{}).onInteraction(s, i)
	if calls != 1 {
		t.Fatalf("requests = %d, want 1", calls)
	}
}

func TestStatusDatabaseFlow(t *testing.T) {
	for _, linked := range []bool{false, true} {
		name := "unlinked"
		if linked {
			name = "linked"
		}
		t.Run(name, func(t *testing.T) {
			a := newTestApp(t)
			u := makeUser(t, a, "Status tester")
			rm := makeRoom(t, a, u)
			calls := 0
			s := statusTestSession(t, func(r *http.Request) (*http.Response, error) {
				calls++
				switch calls {
				case 1:
					if !strings.HasSuffix(r.URL.Path, "/callback") {
						t.Fatalf("first request must acknowledge: %s", r.URL.Path)
					}
					// Create the association only when acknowledged. Looking up the user
					// before acknowledging would incorrectly take the unlinked branch.
					if linked {
						if _, err := a.db.Exec(context.Background(), `INSERT INTO discord_links (discord_user_id, user_id) VALUES ($1,$2)`, u.ID, u.ID); err != nil {
							t.Fatal(err)
						}
						if _, err := a.db.Exec(context.Background(), `INSERT INTO discord_guilds (guild_id, room_id, channel_id) VALUES ($1,$2,'channel')`, rm.ID, rm.ID); err != nil {
							t.Fatal(err)
						}
					}
					return statusHTTPResponse(204, ""), nil
				case 2:
					if linked {
						if r.Method != http.MethodPost {
							t.Fatalf("expected public followup, got %s", r.Method)
						}
						body, _ := io.ReadAll(r.Body)
						if !strings.Contains(string(body), "no run active") || !strings.Contains(string(body), "Join") {
							t.Fatalf("unexpected status: %s", body)
						}
					} else {
						if r.Method != http.MethodPatch {
							t.Fatalf("expected private edit, got %s", r.Method)
						}
						body, _ := io.ReadAll(r.Body)
						if !strings.Contains(string(body), "Link your Discord") {
							t.Fatalf("unexpected error: %s", body)
						}
					}
					return statusHTTPResponse(200, `{"id":"message"}`), nil
				case 3:
					if !linked || r.Method != http.MethodDelete {
						t.Fatalf("unexpected cleanup: %s", r.Method)
					}
					return statusHTTPResponse(204, ""), nil
				default:
					t.Fatalf("unexpected request %d", calls)
					return nil, nil
				}
			})
			i := statusInteraction()
			i.User = &discordgo.User{ID: u.ID}
			i.GuildID = rm.ID
			(&discordBot{app: a}).handleStatus(s, i)
			want := 2
			if linked {
				want = 3
			}
			if calls != want {
				t.Fatalf("requests = %d, want %d", calls, want)
			}
		})
	}
}
