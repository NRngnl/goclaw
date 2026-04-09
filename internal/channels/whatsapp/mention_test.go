package whatsapp

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestIsMentioned(t *testing.T) {
	// Helper to build an events.Message with mentioned JIDs in extended text.
	makeEvt := func(mentionedJIDs []string) *events.Message {
		return &events.Message{
			Message: &waE2E.Message{
				ExtendedTextMessage: &waE2E.ExtendedTextMessage{
					Text: proto.String("hello @bot"),
					ContextInfo: &waE2E.ContextInfo{
						MentionedJID: mentionedJIDs,
					},
				},
			},
		}
	}

	tests := []struct {
		name    string
		myJID   string // bot's phone JID
		myLID   string // bot's LID
		mentions []string
		want    bool
	}{
		{
			name:     "mentioned by phone JID",
			myJID:    "1234567890@s.whatsapp.net",
			mentions: []string{"1234567890@s.whatsapp.net"},
			want:     true,
		},
		{
			name:     "mentioned by LID",
			myLID:    "9876543210@lid",
			mentions: []string{"9876543210@lid"},
			want:     true,
		},
		{
			name:     "mentioned by JID with device suffix",
			myJID:    "1234567890@s.whatsapp.net",
			mentions: []string{"1234567890:42@s.whatsapp.net"},
			want:     true,
		},
		{
			name:     "mentioned by LID with device suffix",
			myLID:    "9876543210@lid",
			mentions: []string{"9876543210:5@lid"},
			want:     true,
		},
		{
			name:     "dual identity — mentioned via LID when JID also set",
			myJID:    "1234567890@s.whatsapp.net",
			myLID:    "9876543210@lid",
			mentions: []string{"9876543210@lid"},
			want:     true,
		},
		{
			name:     "dual identity — mentioned via JID when LID also set",
			myJID:    "1234567890@s.whatsapp.net",
			myLID:    "9876543210@lid",
			mentions: []string{"1234567890@s.whatsapp.net"},
			want:     true,
		},
		{
			name:     "not mentioned — different user",
			myJID:    "1234567890@s.whatsapp.net",
			mentions: []string{"9999999999@s.whatsapp.net"},
			want:     false,
		},
		{
			name:     "not mentioned — empty mentions",
			myJID:    "1234567890@s.whatsapp.net",
			mentions: []string{},
			want:     false,
		},
		{
			name:     "unknown identity — fail closed",
			myJID:    "",
			myLID:    "",
			mentions: []string{"1234567890@s.whatsapp.net"},
			want:     false,
		},
		{
			name:     "no extended text message",
			myJID:    "1234567890@s.whatsapp.net",
			mentions: nil, // will use plain conversation message
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := &Channel{}

			// Set bot identity.
			if tt.myJID != "" {
				jid, _ := types.ParseJID(tt.myJID)
				ch.myJID = jid
			}
			if tt.myLID != "" {
				lid, _ := types.ParseJID(tt.myLID)
				ch.myLID = lid
			}

			var evt *events.Message
			if tt.mentions == nil {
				// Plain conversation message — no extended text.
				evt = &events.Message{
					Message: &waE2E.Message{
						Conversation: proto.String("hello"),
					},
				}
			} else {
				evt = makeEvt(tt.mentions)
			}

			got := ch.isMentioned(evt)
			if got != tt.want {
				t.Errorf("isMentioned() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsReplyToBot(t *testing.T) {
	tests := []struct {
		name       string
		myJID      string
		myLID      string
		evt        *events.Message
		seedSent   []string // message IDs to pre-populate sentMessages
		want       bool
	}{
		{
			name:     "empty participant + stanzaID in sentMessages → reply to bot",
			myJID:    "1234567890@s.whatsapp.net",
			seedSent: []string{"3EB0ABCDEF123456"},
			evt: &events.Message{Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("replying"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID: proto.String("3EB0ABCDEF123456"),
				},
			}}},
			want: true,
		},
		{
			name:  "empty participant + stanzaID NOT in sentMessages → not reply to bot",
			myJID: "1234567890@s.whatsapp.net",
			evt: &events.Message{Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("replying"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID: proto.String("UNKNOWN_MSG_ID"),
				},
			}}},
			want: false,
		},
		{
			name:  "reply to bot via phone JID participant",
			myJID: "1234567890@s.whatsapp.net",
			evt: &events.Message{Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("replying"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:    proto.String("3EB0ABCDEF123456"),
					Participant: proto.String("1234567890@s.whatsapp.net"),
				},
			}}},
			want: true,
		},
		{
			name:  "reply to bot via phone JID with device suffix",
			myJID: "1234567890@s.whatsapp.net",
			evt: &events.Message{Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("replying"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:    proto.String("3EB0ABCDEF123456"),
					Participant: proto.String("1234567890:42@s.whatsapp.net"),
				},
			}}},
			want: true,
		},
		{
			name:  "reply to bot via LID",
			myLID: "9876543210@lid",
			evt: &events.Message{Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("replying"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:    proto.String("3EB0ABCDEF123456"),
					Participant: proto.String("9876543210@lid"),
				},
			}}},
			want: true,
		},
		{
			name:  "reply to another user — not bot",
			myJID: "1234567890@s.whatsapp.net",
			evt: &events.Message{Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("replying"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:    proto.String("3EB0ABCDEF123456"),
					Participant: proto.String("9999999999@s.whatsapp.net"),
				},
			}}},
			want: false,
		},
		{
			name:  "no reply context — plain message",
			myJID: "1234567890@s.whatsapp.net",
			evt: &events.Message{Message: &waE2E.Message{
				Conversation: proto.String("hello"),
			}},
			want: false,
		},
		{
			name:  "unknown identity — fail closed",
			myJID: "",
			myLID: "",
			evt: &events.Message{Message: &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String("replying"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:    proto.String("3EB0ABCDEF123456"),
					Participant: proto.String("1234567890@s.whatsapp.net"),
				},
			}}},
			want: false,
		},
		{
			name:     "image reply — self-quote (empty participant, stanzaID in sentMessages)",
			myJID:    "1234567890@s.whatsapp.net",
			seedSent: []string{"3EB0ABCDEF123456"},
			evt: &events.Message{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
				Caption: proto.String("image reply"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID: proto.String("3EB0ABCDEF123456"),
				},
			}}},
			want: true,
		},
		{
			name:  "image reply to bot via LID participant",
			myLID: "9876543210@lid",
			evt: &events.Message{Message: &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
				Caption: proto.String("image reply"),
				ContextInfo: &waE2E.ContextInfo{
					StanzaID:    proto.String("3EB0ABCDEF123456"),
					Participant: proto.String("9876543210:5@lid"),
				},
			}}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := &Channel{}
			if tt.myJID != "" {
				jid, _ := types.ParseJID(tt.myJID)
				ch.myJID = jid
			}
			if tt.myLID != "" {
				lid, _ := types.ParseJID(tt.myLID)
				ch.myLID = lid
			}
			for _, id := range tt.seedSent {
				ch.sentMessages.Store(id, time.Now())
			}
			got := ch.isReplyToBot(tt.evt)
			if got != tt.want {
				t.Errorf("isReplyToBot() = %v, want %v", got, tt.want)
			}
		})
	}
}
