package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Send delivers an outbound message to WhatsApp via whatsmeow.
func (c *Channel) Send(_ context.Context, msg bus.OutboundMessage) error {
	if c.client == nil || !c.client.IsConnected() {
		return fmt.Errorf("whatsapp not connected")
	}

	chatJID, err := types.ParseJID(msg.ChatID)
	if err != nil {
		return fmt.Errorf("invalid whatsapp JID %q: %w", msg.ChatID, err)
	}

	// Send media attachments first.
	if len(msg.Media) > 0 {
		for i, m := range msg.Media {
			caption := m.Caption
			if caption == "" && i == 0 && msg.Content != "" {
				caption = markdownToWhatsApp(msg.Content)
			}

			data, readErr := os.ReadFile(m.URL)
			if readErr != nil {
				return fmt.Errorf("read media file: %w", readErr)
			}

			waMsg, buildErr := c.buildMediaMessage(data, m.ContentType, caption)
			if buildErr != nil {
				return fmt.Errorf("build media message: %w", buildErr)
			}

			if _, sendErr := c.client.SendMessage(c.ctx, chatJID, waMsg); sendErr != nil {
				return fmt.Errorf("send whatsapp media: %w", sendErr)
			}
		}
		// Skip text if caption was used on first media.
		if msg.Media[0].Caption == "" && msg.Content != "" {
			msg.Content = ""
		}
	}

	// Send text (chunked if exceeding limit).
	if msg.Content != "" {
		formatted := markdownToWhatsApp(msg.Content)

		// Resolve @mentions only if text contains @.
		var wireText string
		var mentionJIDs []string
		if strings.ContainsRune(formatted, '@') {
			contacts := c.loadContactsForMentions()
			wireText, mentionJIDs = resolveMentions(formatted, contacts)
		} else {
			wireText = formatted
		}

		chunks := chunkText(wireText, maxMessageLen)
		for _, chunk := range chunks {
			var waMsg *waE2E.Message
			// Only attach MentionedJID to chunks that actually contain @phone references.
			if len(mentionJIDs) > 0 && strings.ContainsRune(chunk, '@') {
				// Filter JIDs to only those whose phone number appears in this chunk.
				var chunkJIDs []string
				for _, jid := range mentionJIDs {
					phone := strings.SplitN(jid, "@", 2)[0]
					if strings.Contains(chunk, "@"+phone) {
						chunkJIDs = append(chunkJIDs, jid)
					}
				}
				if len(chunkJIDs) > 0 {
					waMsg = &waE2E.Message{
						ExtendedTextMessage: &waE2E.ExtendedTextMessage{
							Text: proto.String(chunk),
							ContextInfo: &waE2E.ContextInfo{
								MentionedJID: chunkJIDs,
							},
						},
					}
				}
			}
			if waMsg == nil {
				waMsg = &waE2E.Message{
					Conversation: proto.String(chunk),
				}
			}
			if _, err := c.client.SendMessage(c.ctx, chatJID, waMsg); err != nil {
				return fmt.Errorf("send whatsapp message: %w", err)
			}
		}
	}

	// Stop typing indicator.
	if cancel, ok := c.typingCancel.LoadAndDelete(msg.ChatID); ok {
		if fn, ok := cancel.(context.CancelFunc); ok {
			fn()
		}
	}
	go c.sendPresence(chatJID, types.ChatPresencePaused)

	return nil
}

// loadContactsForMentions fetches known contacts for this channel instance
// and builds a list for mention resolution.
func (c *Channel) loadContactsForMentions() []contactEntry {
	cc := c.ContactCollector()
	if cc == nil {
		return nil
	}

	contacts, err := cc.ListContacts(c.ctx, store.ContactListOpts{
		ChannelType: string(channels.TypeWhatsApp),
		ContactType: "user",
		Limit:       1000,
	})
	if err != nil {
		slog.Debug("whatsapp: failed to load contacts for mentions", "error", err)
		return nil
	}

	instanceName := c.Name()
	var entries []contactEntry
	for _, ct := range contacts {
		// Filter by this channel instance.
		if ct.ChannelInstance != nil && *ct.ChannelInstance != instanceName {
			continue
		}
		if ct.DisplayName == nil || *ct.DisplayName == "" {
			continue
		}
		// Extract phone number from sender_id (e.g., "1234567890@s.whatsapp.net" → "1234567890").
		phone := ct.SenderID
		if idx := strings.IndexByte(phone, '@'); idx > 0 {
			phone = phone[:idx]
		}
		if phone == "" || !isDigits(phone) {
			continue // skip non-phone JIDs (groups, LIDs without phone)
		}
		entries = append(entries, contactEntry{
			displayName: *ct.DisplayName,
			phone:       phone,
			jid:         ct.SenderID,
		})
	}
	return entries
}

// buildMediaMessage uploads media to WhatsApp and returns the message proto.
func (c *Channel) buildMediaMessage(data []byte, mime, caption string) (*waE2E.Message, error) {
	switch {
	case strings.HasPrefix(mime, "image/"):
		uploaded, err := c.client.Upload(c.ctx, data, whatsmeow.MediaImage)
		if err != nil {
			return nil, err
		}
		return &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				Caption:       proto.String(caption),
				Mimetype:      proto.String(mime),
				URL:           &uploaded.URL,
				DirectPath:    &uploaded.DirectPath,
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(data))),
			},
		}, nil

	case strings.HasPrefix(mime, "video/"):
		uploaded, err := c.client.Upload(c.ctx, data, whatsmeow.MediaVideo)
		if err != nil {
			return nil, err
		}
		return &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{
				Caption:       proto.String(caption),
				Mimetype:      proto.String(mime),
				URL:           &uploaded.URL,
				DirectPath:    &uploaded.DirectPath,
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(data))),
			},
		}, nil

	case strings.HasPrefix(mime, "audio/"):
		uploaded, err := c.client.Upload(c.ctx, data, whatsmeow.MediaAudio)
		if err != nil {
			return nil, err
		}
		return &waE2E.Message{
			AudioMessage: &waE2E.AudioMessage{
				Mimetype:      proto.String(mime),
				URL:           &uploaded.URL,
				DirectPath:    &uploaded.DirectPath,
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(data))),
			},
		}, nil

	default: // document
		uploaded, err := c.client.Upload(c.ctx, data, whatsmeow.MediaDocument)
		if err != nil {
			return nil, err
		}
		return &waE2E.Message{
			DocumentMessage: &waE2E.DocumentMessage{
				Caption:       proto.String(caption),
				Mimetype:      proto.String(mime),
				URL:           &uploaded.URL,
				DirectPath:    &uploaded.DirectPath,
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uint64(len(data))),
			},
		}, nil
	}
}

// keepTyping sends "composing" presence repeatedly until ctx is cancelled.
func (c *Channel) keepTyping(ctx context.Context, chatJID types.JID) {
	c.sendPresence(chatJID, types.ChatPresenceComposing)
	ticker := time.NewTicker(8 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.sendPresence(chatJID, types.ChatPresenceComposing)
		}
	}
}

// sendPresence sends a WhatsApp chat presence update.
func (c *Channel) sendPresence(to types.JID, state types.ChatPresence) {
	if c.client == nil || !c.client.IsConnected() {
		return
	}
	if err := c.client.SendChatPresence(c.ctx, to, state, ""); err != nil {
		slog.Debug("whatsapp: presence update failed", "state", state, "error", err)
	}
}
