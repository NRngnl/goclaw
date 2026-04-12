package tools

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// --- Context helpers for media video ---

const ctxMediaVideoRefs toolContextKey = "tool_media_video_refs"

// WithMediaVideoRefs stores video MediaRefs in context for read_video tool access.
func WithMediaVideoRefs(ctx context.Context, refs []providers.MediaRef) context.Context {
	return context.WithValue(ctx, ctxMediaVideoRefs, refs)
}

// MediaVideoRefsFromCtx retrieves stored video MediaRefs from context.
func MediaVideoRefsFromCtx(ctx context.Context) []providers.MediaRef {
	v, _ := ctx.Value(ctxMediaVideoRefs).([]providers.MediaRef)
	return v
}

// --- ReadVideoTool ---

// videoMaxBytes is the max file size for base64 video analysis (100MB).
const videoMaxBytes = 100 * 1024 * 1024

// videoProviderPriority is the order in which providers are tried for video analysis.
// OpenAI excluded — no native video upload in chat completions.
var videoProviderPriority = []string{"gemini", "openrouter"}

// videoModelDefaults maps provider names to preferred video-capable models.
var videoModelDefaults = map[string]string{
	"gemini":     "gemini-2.5-flash",
	"openrouter": "google/gemini-2.5-flash",
}

// ReadVideoTool uses a video-capable provider to analyze video files
// attached to the current conversation or from the agent's workspace.
type ReadVideoTool struct {
	registry       *providers.Registry
	mediaLoader    MediaPathLoader
	deniedPrefixes []string
}

// DenyPaths adds path prefixes that read_video must reject when using file_path.
func (t *ReadVideoTool) DenyPaths(prefixes ...string) {
	t.deniedPrefixes = append(t.deniedPrefixes, prefixes...)
}

func NewReadVideoTool(registry *providers.Registry, mediaLoader MediaPathLoader) *ReadVideoTool {
	return &ReadVideoTool{registry: registry, mediaLoader: mediaLoader}
}

func (t *ReadVideoTool) Name() string { return "read_video" }

func (t *ReadVideoTool) Description() string {
	return "Analyze video files. Works with user-sent attachments (<media:video> tags), " +
		"video files in your workspace (file_path), or video URLs (video_url). " +
		"Specify what you want to extract or analyze."
}

func (t *ReadVideoTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "What to analyze. E.g. 'Describe what happens in this video', 'Summarize the key scenes', 'What text appears on screen?'",
			},
			"media_id": map[string]any{
				"type":        "string",
				"description": "Optional: specific media_id from <media:video> tag. If omitted, uses most recent video.",
			},
			"file_path": map[string]any{
				"type":        "string",
				"description": "Optional: path to a video file in your workspace. Use this when you downloaded a video file (e.g. via a skill) rather than receiving it from the user.",
			},
			"video_url": map[string]any{
				"type":        "string",
				"description": "Optional: HTTPS URL to a video. The AI provider will download it directly. Use for large videos that exceed file size limits.",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *ReadVideoTool) Execute(ctx context.Context, args map[string]any) *Result {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		prompt = "Analyze this video and describe its contents."
	}
	mediaID, _ := args["media_id"].(string)
	filePath, _ := args["file_path"].(string)
	videoURL, _ := args["video_url"].(string)

	// URL mode: pass URL directly to provider, no file reading needed.
	if videoURL != "" {
		parsed, parseErr := url.Parse(videoURL)
		if parseErr != nil || parsed.Scheme != "https" {
			return ErrorResult("video_url must be a valid https:// URL")
		}
		// SSRF check: DNS pinning, private/loopback/link-local ranges, .internal hostnames
		if err := CheckSSRF(videoURL); err != nil {
			slog.Warn("security.read_video_ssrf_blocked", "url", videoURL, "err", err)
			return ErrorResult(fmt.Sprintf("video_url is not allowed: %v", err))
		}
		slog.Info("read_video: using URL mode", "host", parsed.Host)
		chain := ResolveMediaProviderChain(ctx, "read_video", "", "",
			videoProviderPriority, videoModelDefaults, t.registry)
		for i := range chain {
			if chain[i].Params == nil {
				chain[i].Params = make(map[string]any)
			}
			chain[i].Params["prompt"] = prompt
			chain[i].Params["video_url"] = videoURL
			chain[i].Params["mime"] = mimeFromVideoExt(filepath.Ext(parsed.Path))
		}
		chainResult, err := ExecuteWithChain(ctx, chain, t.registry, t.callProvider)
		if err != nil {
			return ErrorResult(fmt.Sprintf("Video analysis failed: %v", err))
		}
		result := NewResult(string(chainResult.Data))
		result.Usage = chainResult.Usage
		result.Provider = chainResult.Provider
		result.Model = chainResult.Model
		return result
	}

	var videoPath, videoMime string
	var err error

	if filePath != "" {
		// Resolve file_path relative to workspace and validate access.
		workspace := ToolWorkspaceFromCtx(ctx)
		if workspace == "" {
			return ErrorResult("file_path: no workspace configured for this agent")
		}
		allowed := allowedWithTeamWorkspace(ctx, nil)
		videoPath, err = resolvePathWithAllowed(filePath, workspace, effectiveRestrict(ctx, true), allowed)
		if err != nil {
			return ErrorResult(fmt.Sprintf("file_path: %v", err))
		}
		if err := checkDeniedPath(videoPath, workspace, t.deniedPrefixes); err != nil {
			return ErrorResult(err.Error())
		}
		videoMime = mimeFromVideoExt(filepath.Ext(videoPath))
	} else {
		videoPath, videoMime, err = t.resolveVideoFile(ctx, mediaID)
		if err != nil {
			return ErrorResult(err.Error())
		}
	}

	slog.Info("read_video: resolved file", "path", videoPath, "mime", videoMime, "media_id", mediaID)

	data, err := os.ReadFile(videoPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to read video file: %v", err))
	}
	slog.Info("read_video: file loaded", "size_bytes", len(data))
	if len(data) > videoMaxBytes {
		return ErrorResult(fmt.Sprintf("Video too large: %d bytes (max %d)", len(data), videoMaxBytes))
	}

	chain := ResolveMediaProviderChain(ctx, "read_video", "", "",
		videoProviderPriority, videoModelDefaults, t.registry)

	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = prompt
		chain[i].Params["data"] = data
		chain[i].Params["mime"] = videoMime
	}

	chainResult, err := ExecuteWithChain(ctx, chain, t.registry, t.callProvider)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Video analysis failed: %v", err))
	}

	result := NewResult(string(chainResult.Data))
	result.Usage = chainResult.Usage
	result.Provider = chainResult.Provider
	result.Model = chainResult.Model
	return result
}

// mimeFromVideoExt returns MIME type for video file extensions.
func mimeFromVideoExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".mkv":
		return "video/x-matroska"
	case ".wmv":
		return "video/x-ms-wmv"
	case ".flv":
		return "video/x-flv"
	case ".3gp":
		return "video/3gpp"
	case ".mpeg", ".mpg":
		return "video/mpeg"
	default:
		return "video/mp4"
	}
}
