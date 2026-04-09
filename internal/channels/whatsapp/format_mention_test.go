package whatsapp

import (
	"testing"
)

func TestResolveMentions(t *testing.T) {
	contacts := []contactEntry{
		{displayName: "Alice Smith", phone: "1234567890", jid: "1234567890@s.whatsapp.net"},
		{displayName: "Bob Lee", phone: "9876543210", jid: "9876543210@s.whatsapp.net"},
		{displayName: "Bob Wang", phone: "81234567890", jid: "81234567890@s.whatsapp.net"},
	}

	tests := []struct {
		name      string
		input     string
		wantText  string
		wantJIDs  int
	}{
		{
			name:     "mention by full name",
			input:    "Hey @Alice Smith check this",
			wantText: "Hey @1234567890 check this",
			wantJIDs: 1,
		},
		{
			name:     "mention by first name — unique match",
			input:    "Hey @Alice check this",
			wantText: "Hey @1234567890 check this",
			wantJIDs: 1,
		},
		{
			name:     "mention by phone number",
			input:    "Hey @1234567890 check this",
			wantText: "Hey @1234567890 check this",
			wantJIDs: 1,
		},
		{
			name:     "ambiguous first name — no mention (Bob matches 2)",
			input:    "Hey @Bob what's up",
			wantText: "Hey @Bob what's up",
			wantJIDs: 0,
		},
		{
			name:     "disambiguated full name",
			input:    "Hey @Bob Lee what's up",
			wantText: "Hey @9876543210 what's up",
			wantJIDs: 1,
		},
		{
			name:     "email — no mention",
			input:    "Send to user@gmail.com please",
			wantText: "Send to user@gmail.com please",
			wantJIDs: 0,
		},
		{
			name:     "unknown name — pass through",
			input:    "Hey @Unknown check this",
			wantText: "Hey @Unknown check this",
			wantJIDs: 0,
		},
		{
			name:     "no mentions",
			input:    "Hello everyone",
			wantText: "Hello everyone",
			wantJIDs: 0,
		},
		{
			name:     "multiple mentions",
			input:    "@Alice Smith and @Bob Lee please check",
			wantText: "@1234567890 and @9876543210 please check",
			wantJIDs: 2,
		},
		{
			name:     "mention at start of text",
			input:    "@Alice hello",
			wantText: "@1234567890 hello",
			wantJIDs: 1,
		},
		{
			name:     "mention with trailing punctuation",
			input:    "@Alice, can you check?",
			wantText: "@1234567890, can you check?",
			wantJIDs: 1,
		},
		{
			name:     "mention with trailing period",
			input:    "Ask @Alice.",
			wantText: "Ask @1234567890.",
			wantJIDs: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotText, gotJIDs := resolveMentions(tt.input, contacts)
			if gotText != tt.wantText {
				t.Errorf("text = %q, want %q", gotText, tt.wantText)
			}
			if len(gotJIDs) != tt.wantJIDs {
				t.Errorf("jids = %d (%v), want %d", len(gotJIDs), gotJIDs, tt.wantJIDs)
			}
		})
	}
}
