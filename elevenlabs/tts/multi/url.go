package multi

import (
	"net/url"
	"strconv"
	"strings"
)

// ConnectConfig maps to the ElevenLabs multi-context TTS websocket query parameters and auth options.
type ConnectConfig struct {
	// BaseURL is the websocket base URL, e.g. "wss://api.elevenlabs.io".
	BaseURL string
	// VoiceID is required.
	VoiceID string

	// APIKey is your ElevenLabs API key (sent as header: xi-api-key).
	APIKey string
	// Authorization bearer token (optional alternative to API key).
	Authorization string

	ModelID                string
	LanguageCode           string
	EnableLogging          *bool
	EnableSSMLParsing      *bool
	OutputFormat           string
	InactivityTimeout      *int
	SyncAlignment          *bool
	AutoMode               *bool
	ApplyTextNormalization string
	Seed                   *int
}

func DefaultBaseURL() string { return "wss://api.elevenlabs.io" }

func BuildURL(cfg ConnectConfig) (string, error) {
	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL()
	}
	base = strings.TrimRight(base, "/")

	u, err := url.Parse(base + "/v1/text-to-speech/" + url.PathEscape(cfg.VoiceID) + "/multi-stream-input")
	if err != nil {
		return "", err
	}

	q := u.Query()
	if cfg.ModelID != "" {
		q.Set("model_id", cfg.ModelID)
	}
	if cfg.LanguageCode != "" {
		q.Set("language_code", cfg.LanguageCode)
	}
	if cfg.OutputFormat != "" {
		q.Set("output_format", cfg.OutputFormat)
	}
	if cfg.EnableLogging != nil {
		if *cfg.EnableLogging {
			q.Set("enable_logging", "true")
		} else {
			q.Set("enable_logging", "false")
		}
	}
	if cfg.EnableSSMLParsing != nil {
		if *cfg.EnableSSMLParsing {
			q.Set("enable_ssml_parsing", "true")
		} else {
			q.Set("enable_ssml_parsing", "false")
		}
	}
	if cfg.InactivityTimeout != nil {
		q.Set("inactivity_timeout", strconv.Itoa(*cfg.InactivityTimeout))
	}
	if cfg.SyncAlignment != nil {
		if *cfg.SyncAlignment {
			q.Set("sync_alignment", "true")
		} else {
			q.Set("sync_alignment", "false")
		}
	}
	if cfg.AutoMode != nil {
		if *cfg.AutoMode {
			q.Set("auto_mode", "true")
		} else {
			q.Set("auto_mode", "false")
		}
	}
	if cfg.ApplyTextNormalization != "" {
		q.Set("apply_text_normalization", cfg.ApplyTextNormalization)
	}
	if cfg.Seed != nil {
		q.Set("seed", strconv.Itoa(*cfg.Seed))
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
