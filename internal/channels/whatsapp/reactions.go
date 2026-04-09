package whatsapp

import (
	"context"
	"log/slog"

	"go.mau.fi/whatsmeow/types"
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
		return nil // silently skip if not connected
	}

	chatJID, err := types.ParseJID(chatID)
	if err != nil {
		return nil
	}

	// Use bot's own JID as the sender of the reaction.
	c.lastQRMu.RLock()
	myJID := c.myJID
	c.lastQRMu.RUnlock()

	if myJID.IsEmpty() {
		return nil
	}

	reactionMsg := c.client.BuildReaction(chatJID, myJID, types.MessageID(messageID), emoji)
	_, err = c.client.SendMessage(ctx, chatJID, reactionMsg)
	if err != nil {
		slog.Debug("whatsapp: reaction failed", "chat", chatID, "msg", messageID, "emoji", emoji, "error", err)
	}
	return nil // don't propagate reaction errors
}
