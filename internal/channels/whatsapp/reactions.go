package whatsapp

import (
	"context"
	"log/slog"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// Status-to-emoji mapping for WhatsApp reactions.
// WhatsApp supports any single emoji as a reaction.
var waStatusEmoji = map[string]string{
	"thinking": "🤔",
	"tool":     "⚡",
	"coding":   "👨‍💻",
	"web":      "🌐",
	"error":    "❌",
}

// OnReactionEvent handles agent status change events and reacts to the original message.
// Implements the channels.ReactionChannel interface.
func (c *Channel) OnReactionEvent(ctx context.Context, chatID string, messageID string, status string) error {
	if c.config.ReactionLevel == "" || c.config.ReactionLevel == "off" {
		return nil
	}

	// "minimal" mode: only react on thinking (acknowledge receipt) and terminal states.
	if c.config.ReactionLevel == "minimal" && status != "thinking" && status != "done" && status != "error" {
		return nil
	}

	// "done" clears the reaction — the response itself is the confirmation.
	if status == "done" {
		return c.sendReaction(ctx, chatID, messageID, "")
	}

	emoji, ok := waStatusEmoji[status]
	if !ok {
		return nil
	}

	return c.sendReaction(ctx, chatID, messageID, emoji)
}

// ClearReaction removes a reaction from a message (sends empty emoji).
// Implements the channels.ReactionChannel interface.
func (c *Channel) ClearReaction(ctx context.Context, chatID string, messageID string) error {
	return c.sendReaction(ctx, chatID, messageID, "")
}

// sendReaction sends an emoji reaction to a specific message.
func (c *Channel) sendReaction(ctx context.Context, chatID string, messageID string, emoji string) error {
	if c.client == nil || !c.client.IsConnected() {
		return nil
	}

	chatJID, err := types.ParseJID(chatID)
	if err != nil {
		return nil
	}

	// Build the reaction message key.
	// WhatsApp requires the original sender's JID as participant in group chats.
	key := &waCommon.MessageKey{
		FromMe:    proto.Bool(false),
		ID:        proto.String(messageID),
		RemoteJID: proto.String(chatJID.String()),
	}
	// Set participant from the original sender's JID stored in run metadata.
	if meta := channels.RunMetadataFromContext(ctx); meta != nil {
		if senderJID := meta["wa_sender_jid"]; senderJID != "" {
			key.Participant = proto.String(senderJID)
		}
	}

	reactionMsg := &waE2E.Message{
		ReactionMessage: &waE2E.ReactionMessage{
			Key:               key,
			Text:              proto.String(emoji),
			SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
		},
	}

	_, err = c.client.SendMessage(ctx, chatJID, reactionMsg)
	if err != nil {
		slog.Warn("whatsapp: reaction failed", "chat", chatID, "msg", messageID, "emoji", emoji, "error", err)
	}
	return nil
}
